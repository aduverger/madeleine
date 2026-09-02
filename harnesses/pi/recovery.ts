import type { ExtensionContext } from "@earendil-works/pi-coding-agent";

import type { Capture, FinalizationDraft } from "./rpc.ts";
import type { EpisodeFinalization } from "./summary.ts";

export interface RecoveryFinalizer {
  finalize(
    draft: FinalizationDraft,
    ctx: ExtensionContext,
    signal?: AbortSignal,
  ): Promise<EpisodeFinalization>;
}

export interface RecoveryClient {
  listPendingCaptures(
    repositoryRoot: string,
    externalID?: string,
    signal?: AbortSignal,
  ): Promise<Capture[]>;
  sealCapture(
    captureID: string,
    endCursor: string,
    transcript?: undefined,
    signal?: AbortSignal,
  ): Promise<FinalizationDraft>;
}

export class PendingCaptureRecovery {
  private readonly automaticallyAttempted = new Set<string>();
  private backgroundController: AbortController | undefined;
  private background: Promise<void> = Promise.resolve();

  constructor(
    private readonly client: RecoveryClient,
    private readonly finalizer: RecoveryFinalizer,
  ) {}

  start(
    repositoryRoot: string,
    externalID: string,
    currentCaptureID: string,
    ctx: ExtensionContext,
    notifyFailure: (message: string) => void,
  ): void {
    this.backgroundController?.abort();
    const controller = new AbortController();
    this.backgroundController = controller;
    const recover = async () => {
      let captures: Capture[];
      try {
        captures = (await this.pendingCaptures(repositoryRoot, externalID, controller.signal))
          .filter((capture) => capture.id !== currentCaptureID);
      } catch {
        if (!controller.signal.aborted) {
          notifyFailure("Madeleine could not inspect pending Captures for recovery.");
        }
        return;
      }

      for (const capture of captures) {
        if (controller.signal.aborted) return;
        if (this.automaticallyAttempted.has(capture.id)) continue;
        this.automaticallyAttempted.add(capture.id);
        try {
          await this.finalizeCapture(capture, ctx, controller.signal);
        } catch {
          if (!controller.signal.aborted) {
            notifyFailure(`Madeleine could not recover Capture ${capture.id}; it remains pending.`);
          }
        }
      }
    };
    this.background = this.background.then(recover, recover);
  }

  async stop(): Promise<void> {
    this.backgroundController?.abort();
    this.backgroundController = undefined;
    await this.background;
  }

  private async pendingCaptures(
    repositoryRoot: string,
    externalID: string,
    signal?: AbortSignal,
  ): Promise<Capture[]> {
    const captures = await this.client.listPendingCaptures(repositoryRoot, externalID, signal);
    return captures
      .filter((capture) => capture.status === "pending_summary")
      .sort((left, right) =>
        left.started_at.localeCompare(right.started_at) || left.id.localeCompare(right.id));
  }

  private async finalizeCapture(
    capture: Capture,
    ctx: ExtensionContext,
    signal?: AbortSignal,
  ): Promise<Extract<EpisodeFinalization, { status: "published" }>> {
    if (!capture.end_cursor) throw new Error("Pending Capture has no end cursor");
    const draft = await this.client.sealCapture(capture.id, capture.end_cursor, undefined, signal);
    const finalization = await this.finalizer.finalize(draft, ctx, signal);
    if (finalization.status !== "published") {
      throw new Error("Pending Capture was unexpectedly abandoned");
    }
    return finalization;
  }
}
