import type { ExtensionAPI, ExtensionCommandContext } from "@earendil-works/pi-coding-agent";
import { describe, expect, it, vi } from "vitest";

import { registerCommands } from "./commands.ts";
import type { Capture, DoctorCheck } from "./rpc.ts";

function capture(id: string, status: Capture["status"] = "open"): Capture {
  return {
    id,
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: "session-1" },
    worktree_root: "/repo",
    status,
    start_cursor: "entry-1",
    started_at: "2026-01-01T00:00:00Z",
    last_seen_at: "2026-01-01T00:00:00Z",
  };
}

function setup(options: { captures?: Capture[]; checks?: DoctorCheck[]; confirmed?: boolean } = {}) {
  let handler: ((args: string, ctx: ExtensionCommandContext) => Promise<void>) | undefined;
  const pi = {
    registerCommand: (_name: string, command: { handler: typeof handler }) => {
      handler = command.handler;
    },
  } as unknown as ExtensionAPI;
  const client = {
    doctor: vi.fn(async () => options.checks ?? []),
    listPendingCaptures: vi.fn(async () => options.captures ?? []),
    abandonCapture: vi.fn(async () => undefined),
  };
  const current = {
    currentCaptureID: vi.fn(() => "capture-current" as string | undefined),
    clearCurrentCapture: vi.fn(),
    rollover: vi.fn(async () => ({
      finalization: {
        captureID: "capture-current",
        status: "published" as const,
        episodeID: "episode-current",
      },
      startedCaptureID: "capture-next",
    })),
  };
  const notify = vi.fn();
  const confirm = vi.fn(async () => options.confirmed ?? false);
  const waitForIdle = vi.fn(async () => undefined);
  const ctx = {
    cwd: "/repo",
    hasUI: true,
    ui: { notify, confirm },
    waitForIdle,
  } as unknown as ExtensionCommandContext;
  registerCommands(pi, client, current);
  return {
    run: (args: string) => handler!(args, ctx),
    client,
    current,
    notify,
    confirm,
    waitForIdle,
  };
}

describe("/madeleine", () => {
  it("prints usage for missing, unknown, and removed subcommands without RPC", async () => {
    const test = setup();
    await test.run("");
    await test.run("unknown");
    await test.run("rollover");
    await test.run("retry");

    expect(test.notify).toHaveBeenCalledTimes(4);
    expect(test.client.listPendingCaptures).not.toHaveBeenCalled();
    expect(test.client.doctor).not.toHaveBeenCalled();
  });

  it("lists repository Captures and marks the current one", async () => {
    const test = setup({
      captures: [capture("capture-current"), capture("capture-pending", "pending_summary")],
    });
    await test.run("status");

    expect(test.client.listPendingCaptures).toHaveBeenCalledWith("/repo");
    expect(test.notify).toHaveBeenCalledWith(
      expect.stringContaining("* capture-current  open"),
      "info",
    );
    expect(test.notify.mock.calls[0]?.[0]).toContain("capture-pending  pending_summary");
  });

  it("waits for idle before capturing the current work", async () => {
    const test = setup();
    await test.run("capture");

    expect(test.waitForIdle).toHaveBeenCalledOnce();
    expect(test.current.rollover).toHaveBeenCalledOnce();
    expect(test.notify).toHaveBeenCalledWith(
      "Published Episode episode-current from Capture capture-current.\nStarted Capture capture-next.",
      "info",
    );
  });

  it("confirms and abandons only a Capture listed for the repository", async () => {
    const test = setup({ captures: [capture("capture-current")], confirmed: true });
    await test.run("abandon capture-current");

    expect(test.confirm).toHaveBeenCalledOnce();
    expect(test.client.abandonCapture).toHaveBeenCalledWith("capture-current");
    expect(test.current.clearCurrentCapture).toHaveBeenCalledWith("capture-current");
  });

  it("refuses to abandon a finalized Capture", async () => {
    const test = setup({ captures: [capture("capture-finalized", "finalized")], confirmed: true });
    await test.run("abandon capture-finalized");
    expect(test.confirm).not.toHaveBeenCalled();
    expect(test.client.abandonCapture).not.toHaveBeenCalled();
  });

  it("does nothing when abandonment is cancelled", async () => {
    const test = setup({ captures: [capture("capture-current")], confirmed: false });
    await test.run("abandon capture-current");
    expect(test.client.abandonCapture).not.toHaveBeenCalled();
  });

  it("rejects a Capture ID that was not returned for the repository", async () => {
    const test = setup({ captures: [capture("capture-other")] });
    await test.run("abandon capture-wrong-repository");

    expect(test.confirm).not.toHaveBeenCalled();
    expect(test.client.abandonCapture).not.toHaveBeenCalled();
    expect(test.notify).toHaveBeenCalledWith(
      "That Capture is not open or pending in this repository.",
      "error",
    );
  });

  it("shows structured doctor checks", async () => {
    const test = setup({
      checks: [
        { name: "application", ok: true, detail: "initialized" },
        { name: "repository", ok: false, detail: "not a Git repository" },
      ],
    });
    await test.run("doctor");

    expect(test.client.doctor).toHaveBeenCalledWith("/repo");
    expect(test.notify).toHaveBeenCalledWith(
      "ok: application — initialized\nfailed: repository — not a Git repository",
      "warning",
    );
  });
});
