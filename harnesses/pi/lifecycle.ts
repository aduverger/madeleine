import {
  isEditToolResult,
  isWriteToolResult,
  type ExtensionAPI,
  type ExtensionCommandContext,
  type ExtensionContext,
  type SessionShutdownEvent,
  type SessionStartEvent,
  type ToolResultEvent,
} from "@earendil-works/pi-coding-agent";
import { access } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import type { Capture, FinalizationDraft } from "./rpc.ts";
import { PiState, type ConversationIdentity } from "./state.ts";

const writeRefreshIntervalMs = 30_000;
const unicodeSpaces = /[\u00A0\u2000-\u200A\u202F\u205F\u3000]/g;

export interface RolloverResult {
  sealed: FinalizationDraft;
  startedCaptureID: string;
}

export interface CaptureClient {
  startCapture(
    repositoryRoot: string,
    externalID: string,
    transcriptRef: string,
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
  sealCapture(captureID: string, endCursor: string, signal?: AbortSignal): Promise<FinalizationDraft>;
}

export class CaptureLifecycle {
  private repositoryRoot = "";
  private conversation: ConversationIdentity | undefined;
  private captureID: string | undefined;
  private workController = new AbortController();
  private readonly lastPersistedWriteAtByPath = new Map<string, number>();
  private readonly writePathsInFlight = new Set<string>();
  private readonly sentNotifications = new Set<string>();

  constructor(
    private readonly client: CaptureClient,
    private readonly state: PiState,
    private readonly ensureReady: (ctx: ExtensionContext) => Promise<boolean>,
    private readonly monotonicNow: () => number = () => performance.now(),
  ) {}

  register(pi: ExtensionAPI): void {
    pi.on("session_start", (event, ctx) => this.start(event, ctx));
    pi.on("tool_result", (event, ctx) => this.recordMutation(event, ctx));
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

  async rollover(ctx: ExtensionCommandContext): Promise<RolloverResult> {
    if (!this.captureID) throw new Error("Madeleine has no active Capture");

    this.workController.abort();
    try {
      const sealed = await this.sealCurrentCapture(ctx);
      if (!sealed) throw new Error("Madeleine has no active Capture");

      this.resetCaptureWork();
      await this.createCapture(ctx);
      if (!this.captureID) throw new Error("Madeleine could not start a replacement Capture");
      return { sealed, startedCaptureID: this.captureID };
    } catch (error) {
      if (this.captureID) this.workController = new AbortController();
      throw error;
    }
  }

  private async start(event: SessionStartEvent, ctx: ExtensionContext): Promise<void> {
    if (!(await this.ensureReady(ctx))) return;

    this.repositoryRoot = ctx.cwd;
    this.conversation = this.state.initialize(ctx, event.reason);
    this.captureID = undefined;
    this.resetCaptureWork();

    try {
      await this.resumeOrCreate(ctx);
    } catch {
      this.notifyOnce(ctx, "start", "Madeleine could not start write capture for this run.");
    }
  }

  private async resumeOrCreate(ctx: ExtensionContext): Promise<void> {
    const persistedCaptureID = this.state.currentCaptureID();
    if (persistedCaptureID) {
      try {
        const capture = await this.client.getCapture(persistedCaptureID, this.workController.signal);
        if (this.isOpenCurrentConversation(capture)) {
          this.captureID = capture.id;
          return;
        }
      } catch {
        // Fall back to the canonical pending-Capture query below.
      }
    }

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

  private async createCapture(ctx: ExtensionContext): Promise<void> {
    if (!this.conversation) return;
    const capture = await this.client.startCapture(
      this.repositoryRoot,
      this.conversation.externalID,
      this.conversation.transcriptRef,
      this.state.ensureCursor(ctx),
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

  private isOpenCurrentConversation(capture: Capture): boolean {
    return (
      capture.status === "open" &&
      capture.conversation_key.harness === "pi" &&
      capture.conversation_key.external_id === this.conversation?.externalID
    );
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

  private async shutdown(event: SessionShutdownEvent, ctx: ExtensionContext): Promise<void> {
    this.workController.abort();
    if (event.reason === "reload" || !this.captureID) return;

    try {
      await this.sealCurrentCapture(ctx);
    } catch {
      this.notifyOnce(ctx, "seal", "Madeleine could not seal the current Capture; it remains recoverable.");
    }
  }

  private async sealCurrentCapture(ctx: ExtensionContext): Promise<FinalizationDraft | undefined> {
    if (!this.captureID) return undefined;
    const captureID = this.captureID;
    const draft = await this.client.sealCapture(captureID, this.state.ensureCursor(ctx));
    this.clearCurrentCapture(captureID);
    return draft;
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
