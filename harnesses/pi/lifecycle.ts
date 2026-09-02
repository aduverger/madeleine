import {
  isEditToolResult,
  isWriteToolResult,
  type ExtensionAPI,
  type ExtensionContext,
  type SessionBeforeTreeEvent,
  type SessionShutdownEvent,
  type SessionStartEvent,
  type SessionTreeEvent,
  type ToolResultEvent,
} from "@earendil-works/pi-coding-agent";
import { access } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  PendingCaptureRecovery,
  type RecoveryFinalizer,
  type RetryResult,
} from "./recovery.ts";
import type { Capture, FinalizationDraft } from "./rpc.ts";
import { extractCaptureTranscript, type TranscriptInput } from "./transcript.ts";
import { PiState, type ConversationIdentity } from "./state.ts";
import {
  EpisodeFinalizer,
  type EpisodeFinalization,
  type SummaryClient,
} from "./summary.ts";

const writeRefreshIntervalMs = 30_000;
const unicodeSpaces = /[\u00A0\u2000-\u200A\u202F\u205F\u3000]/g;

export type FinalizationOutcome = EpisodeFinalization | {
  captureID: string;
  status: "pending";
};

export interface RolloverResult {
  finalization: FinalizationOutcome;
  startedCaptureID: string;
}

export type { RetryResult } from "./recovery.ts";

export type CaptureFinalizer = RecoveryFinalizer;

export interface CaptureClient extends SummaryClient {
  startCapture(
    repositoryRoot: string,
    externalID: string,
    startCursor: string,
    signal?: AbortSignal,
  ): Promise<Capture>;
  getCapture(captureID: string, signal?: AbortSignal): Promise<Capture>;
  listPendingCaptures(
    repositoryRoot: string,
    externalID?: string,
    signal?: AbortSignal,
  ): Promise<Capture[]>;
  recordWrite(captureID: string, path: string, signal?: AbortSignal): Promise<void>;
  abandonCapture(captureID: string, signal?: AbortSignal): Promise<void>;
  sealCapture(
    captureID: string,
    endCursor: string,
    transcript?: TranscriptInput,
    signal?: AbortSignal,
  ): Promise<FinalizationDraft>;
}

export class CaptureLifecycle {
  private repositoryRoot = "";
  private conversation: ConversationIdentity | undefined;
  private captureID: string | undefined;
  private workController = new AbortController();
  private readonly lastPersistedWriteAtByPath = new Map<string, number>();
  private readonly writePathsInFlight = new Set<string>();
  private readonly sentNotifications = new Set<string>();
  private replaceCaptureAfterTree = false;

  private readonly finalizer: CaptureFinalizer;
  private readonly recovery: PendingCaptureRecovery;

  constructor(
    private readonly client: CaptureClient,
    private readonly state: PiState,
    private readonly ensureReady: (ctx: ExtensionContext) => Promise<boolean>,
    private readonly monotonicNow: () => number = () => performance.now(),
    finalizer?: CaptureFinalizer,
  ) {
    this.finalizer = finalizer ?? new EpisodeFinalizer(client);
    this.recovery = new PendingCaptureRecovery(client, this.finalizer);
  }

  register(pi: ExtensionAPI): void {
    pi.on("session_start", (event, ctx) => this.start(event, ctx));
    pi.on("tool_result", (event, ctx) => this.recordMutation(event, ctx));
    pi.on("session_before_tree", (event, ctx) => this.beforeTree(event, ctx));
    pi.on("session_tree", (event, ctx) => this.afterTree(event, ctx));
    pi.on("session_shutdown", (event, ctx) => this.shutdown(event, ctx));
  }

  currentCaptureID(): string | undefined {
    return this.captureID;
  }

  clearCurrentCapture(captureID: string): void {
    if (this.captureID !== captureID) return;
    this.captureID = undefined;
    this.state.clearCapture();
  }

  async rollover(ctx: ExtensionContext): Promise<RolloverResult> {
    const captureID = this.captureID;
    if (!captureID) throw new Error("Madeleine has no active Capture");

    this.workController.abort();
    try {
      const finalization = await this.sealAndFinalize(captureID, ctx);
      this.resetCaptureWork();
      await this.createCapture(ctx);
      if (!this.captureID) throw new Error("Madeleine could not start a replacement Capture");
      return { finalization, startedCaptureID: this.captureID };
    } catch (error) {
      if (this.captureID) this.workController = new AbortController();
      throw error;
    }
  }

  async retry(captureID: string | undefined, ctx: ExtensionContext): Promise<RetryResult[]> {
    if (!this.conversation) throw new Error("Madeleine has no active Conversation");
    return this.recovery.retry(
      this.repositoryRoot,
      this.conversation.externalID,
      captureID,
      ctx,
    );
  }

  private async start(event: SessionStartEvent, ctx: ExtensionContext): Promise<void> {
    if (!(await this.ensureReady(ctx))) return;

    this.repositoryRoot = ctx.cwd;
    this.conversation = this.state.initialize(ctx, event.reason);
    this.captureID = undefined;
    this.replaceCaptureAfterTree = false;
    this.resetCaptureWork();

    try {
      await this.resumeOrCreate(ctx);
      if (this.captureID && this.conversation) {
        this.recovery.start(
          this.repositoryRoot,
          this.conversation.externalID,
          this.captureID,
          ctx,
          (message) => {
            if (ctx.hasUI) ctx.ui.notify(message, "warning");
          },
        );
      }
    } catch {
      this.notifyOnce(ctx, "start", "Madeleine could not start write capture for this run.");
    }
  }

  private async resumeOrCreate(ctx: ExtensionContext): Promise<void> {
    const persistedCaptureID = this.state.currentCaptureID();
    const openCaptures = await this.openCapturesForConversation();
    if (openCaptures.length === 1) {
      const captureID = openCaptures[0]!.id;
      if (captureID !== persistedCaptureID) this.state.attachCapture(captureID);
      this.captureID = captureID;
      return;
    }
    if (openCaptures.length > 1) {
      this.notifyOnce(ctx, "open-conflict", "Madeleine found multiple open Captures; write capture is disabled.");
      return;
    }
    await this.createCapture(ctx);
  }

  private async createCapture(ctx: ExtensionContext, startCursor?: string): Promise<void> {
    if (!this.conversation) return;
    const capture = await this.client.startCapture(
      this.repositoryRoot,
      this.conversation.externalID,
      startCursor ?? this.state.ensureCursor(ctx),
      this.workController.signal,
    );
    this.state.attachCapture(capture.id);
    this.captureID = capture.id;
  }

  private async openCapturesForConversation(): Promise<Capture[]> {
    if (!this.conversation) return [];
    const captures = await this.client.listPendingCaptures(
      this.repositoryRoot,
      this.conversation.externalID,
      this.workController.signal,
    );
    return captures.filter((capture) => capture.status === "open");
  }

  private async recordMutation(event: ToolResultEvent, ctx: ExtensionContext): Promise<void> {
    if (
      !this.captureID ||
      event.isError ||
      (!isEditToolResult(event) && !isWriteToolResult(event))
    ) {
      return;
    }

    const inputPath = event.input.path;
    if (typeof inputPath !== "string" || inputPath.length === 0) return;

    let path: string;
    try {
      path = await resolvePiToolPath(inputPath, ctx.cwd);
    } catch {
      return;
    }

    const now = this.monotonicNow();
    const lastPersistedAt = this.lastPersistedWriteAtByPath.get(path);
    if (
      this.writePathsInFlight.has(path) ||
      (lastPersistedAt !== undefined && now - lastPersistedAt < writeRefreshIntervalMs)
    ) {
      return;
    }

    this.writePathsInFlight.add(path);
    try {
      await this.client.recordWrite(this.captureID, path, this.workController.signal);
      this.lastPersistedWriteAtByPath.set(path, this.monotonicNow());
    } catch {
      this.notifyOnce(ctx, "record-write", "Madeleine could not record a file write.");
    } finally {
      this.writePathsInFlight.delete(path);
    }
  }

  private async beforeTree(
    event: SessionBeforeTreeEvent,
    ctx: ExtensionContext,
  ): Promise<{ cancel: true } | undefined> {
    this.replaceCaptureAfterTree = false;
    const captureID = this.captureID;
    if (!captureID) return;

    try {
      if (!event.preparation.oldLeafId) throw new Error("Pi tree navigation has no source leaf");

      this.workController.abort();
      const result = await this.sealAndFinalize(
        captureID,
        ctx,
        event.preparation.oldLeafId,
        event.signal,
      );
      if (result.status === "pending") {
        this.notifyOnce(ctx, "tree-summary", "Madeleine preserved the Capture, but its Episode remains pending.");
      }
      this.resetCaptureWork();
      await this.createCapture(ctx, event.preparation.oldLeafId);
      this.replaceCaptureAfterTree = true;
    } catch {
      if (this.captureID) this.workController = new AbortController();
      this.notifyOnce(ctx, "tree-seal", "Madeleine could not preserve the current Capture; tree navigation was cancelled.");
      return { cancel: true };
    }
  }

  private async afterTree(event: SessionTreeEvent, ctx: ExtensionContext): Promise<void> {
    if (!this.replaceCaptureAfterTree) return;
    this.replaceCaptureAfterTree = false;
    const sourceCaptureID = this.captureID;
    try {
      if (sourceCaptureID) {
        await this.client.abandonCapture(sourceCaptureID, ctx.signal);
        this.clearCurrentCapture(sourceCaptureID);
      }
      this.resetCaptureWork();
      const startCursor = event.summaryEntry
        ? event.summaryEntry.parentId ?? event.summaryEntry.id
        : event.newLeafId ?? undefined;
      await this.createCapture(ctx, startCursor);
    } catch {
      this.notifyOnce(ctx, "tree-start", "Madeleine could not start write capture on the selected branch.");
    }
  }

  private async shutdown(event: SessionShutdownEvent, ctx: ExtensionContext): Promise<void> {
    this.workController.abort();
    await this.recovery.stop();
    if (event.reason === "reload" || !this.captureID) return;

    try {
      const result = await this.sealAndFinalize(this.captureID, ctx);
      if (result.status === "pending") {
        this.notifyOnce(ctx, "summary", "Madeleine could not publish the sealed Capture; it remains pending.");
      }
    } catch {
      this.notifyOnce(ctx, "seal", "Madeleine could not seal the current Capture; it remains recoverable.");
    }
  }

  private async sealAndFinalize(
    captureID: string,
    ctx: ExtensionContext,
    endCursor = this.state.ensureCursor(ctx),
    signal = ctx.signal,
  ): Promise<FinalizationOutcome> {
    const capture = await this.client.getCapture(captureID, signal);
    const transcript = extractCaptureTranscript(
      ctx.sessionManager.getEntries(),
      capture.start_cursor,
      endCursor,
    );
    const draft = await this.client.sealCapture(captureID, endCursor, transcript, signal);
    this.clearCurrentCapture(captureID);
    try {
      return await this.finalizer.finalize(draft, ctx, signal);
    } catch {
      return { captureID, status: "pending" };
    }
  }

  private resetCaptureWork(): void {
    this.workController.abort();
    this.workController = new AbortController();
    this.lastPersistedWriteAtByPath.clear();
    this.writePathsInFlight.clear();
  }

  private notifyOnce(ctx: ExtensionContext, key: string, message: string): void {
    if (!ctx.hasUI || this.sentNotifications.has(key)) return;
    this.sentNotifications.add(key);
    ctx.ui.notify(message, "warning");
  }
}

export async function resolvePiToolPath(inputPath: string, cwd: string): Promise<string> {
  let path = inputPath.replace(unicodeSpaces, " ");
  if (path.startsWith("@")) path = path.slice(1);
  if (path.startsWith("file://")) path = fileURLToPath(path);
  if (path === "~") path = homedir();
  if (path.startsWith("~/")) path = join(homedir(), path.slice(2));

  const resolved = resolve(cwd, path);
  const decomposed = resolved.normalize("NFD");
  const candidates = [
    resolved,
    resolved.replace(/ (AM|PM)\./gi, "\u202F$1."),
    decomposed,
    resolved.replaceAll("'", "’"),
    decomposed.replaceAll("'", "’"),
  ];
  for (const candidate of candidates) {
    try {
      await access(candidate);
      return candidate;
    } catch {
      // Try the next filename form accepted by Pi's built-in tools.
    }
  }
  return resolved;
}
