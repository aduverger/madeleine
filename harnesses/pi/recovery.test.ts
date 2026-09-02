import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { describe, expect, it, vi } from "vitest";

import {
  PendingCaptureRecovery,
  type RecoveryClient,
  type RecoveryFinalizer,
} from "./recovery.ts";
import type { Capture, FinalizationDraft } from "./rpc.ts";

function capture(id: string, startedAt: string, status: Capture["status"] = "pending_summary"): Capture {
  return {
    id,
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: "conversation-1" },
    worktree_root: "/repo",
    status,
    transcript_id: status === "pending_summary" ? `transcript-${id}` : undefined,
    start_cursor: `start-${id}`,
    end_cursor: status === "pending_summary" ? `end-${id}` : undefined,
    started_at: startedAt,
    ended_at: status === "pending_summary" ? "2026-01-01T00:01:00Z" : undefined,
    last_seen_at: startedAt,
  };
}

function draft(value: Capture): FinalizationDraft {
  return {
    capture_id: value.id,
    transcript_id: value.transcript_id,
    status: "pending_summary",
    empty: false,
    paths: [`src/${value.id}.ts`],
  };
}

function setup(
  captures: Capture[],
  finalize: RecoveryFinalizer["finalize"],
) {
  const client: RecoveryClient = {
    listPendingCaptures: vi.fn(async () => captures),
    sealCapture: vi.fn(async (captureID) => draft(captures.find((item) => item.id === captureID)!)),
  };
  const finalizer = { finalize: vi.fn(finalize) };
  const worker = new PendingCaptureRecovery(client, finalizer);
  const ctx = { signal: undefined } as unknown as ExtensionContext;
  return { client, finalizer, worker, ctx };
}

async function waitForCalls(mock: ReturnType<typeof vi.fn>, count: number): Promise<void> {
  await vi.waitFor(() => expect(mock).toHaveBeenCalledTimes(count));
}

describe("PendingCaptureRecovery", () => {
  it("runs in the background oldest-first and continues after one failure", async () => {
    const attempted: string[] = [];
    const pending = [
      capture("new", "2026-01-02T00:00:00Z"),
      capture("current", "2025-12-31T00:00:00Z"),
      capture("old", "2026-01-01T00:00:00Z"),
    ];
    const { worker, finalizer, ctx } = setup(pending, async (sealed) => {
      attempted.push(sealed.capture_id);
      if (sealed.capture_id === "old") throw new Error("invalid summary");
      return {
        captureID: sealed.capture_id,
        status: "published",
        episodeID: `episode-${sealed.capture_id}`,
      };
    });
    const notify = vi.fn();

    worker.start("/repo", "conversation-1", "current", ctx, notify);
    expect(attempted).toEqual([]);
    await waitForCalls(finalizer.finalize, 2);
    await worker.stop();

    expect(attempted).toEqual(["old", "new"]);
    expect(notify).toHaveBeenCalledOnce();
    expect(notify).toHaveBeenCalledWith(
      "Madeleine could not recover Capture old; it remains pending.",
    );
  });

  it("attempts each queued Capture at most once per runtime", async () => {
    const pending = [capture("old", "2026-01-01T00:00:00Z")];
    const { worker, finalizer, ctx } = setup(pending, async () => {
      throw new Error("summary failed");
    });

    worker.start("/repo", "conversation-1", "current", ctx, () => undefined);
    await waitForCalls(finalizer.finalize, 1);
    await worker.stop();
    worker.start("/repo", "conversation-1", "current", ctx, () => undefined);
    await worker.stop();

    expect(finalizer.finalize).toHaveBeenCalledOnce();
  });

  it("aborts and awaits active recovery cleanup", async () => {
    const pending = [capture("old", "2026-01-01T00:00:00Z")];
    let cleanedUp = false;
    const { worker, finalizer, ctx } = setup(pending, async (_sealed, _ctx, signal) => {
      await new Promise<void>((resolve) => {
        if (signal?.aborted) return resolve();
        signal?.addEventListener("abort", () => resolve(), { once: true });
      });
      cleanedUp = true;
      throw new Error("cancelled");
    });

    worker.start("/repo", "conversation-1", "current", ctx, () => undefined);
    await waitForCalls(finalizer.finalize, 1);
    await worker.stop();

    expect(cleanedUp).toBe(true);
  });
});
