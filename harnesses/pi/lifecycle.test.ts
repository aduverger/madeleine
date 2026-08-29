import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";

import { CaptureLifecycle, type CaptureClient } from "./lifecycle.ts";
import type { Capture, FinalizationDraft } from "./rpc.ts";
import { PiState, stateEntryType } from "./state.ts";

type Handler = (event: any, ctx: ExtensionContext) => Promise<unknown> | unknown;

class FakePi {
  readonly handlers = new Map<string, Handler[]>();
  readonly entries: any[] = [];
  leaf: string | null = "leaf-1";

  readonly api = {
    on: (event: string, handler: Handler) => {
      const handlers = this.handlers.get(event) ?? [];
      handlers.push(handler);
      this.handlers.set(event, handlers);
    },
    appendEntry: (customType: string, data: unknown) => {
      const id = `state-${this.entries.length + 1}`;
      this.entries.push({ type: "custom", id, parentId: this.leaf, customType, data });
      this.leaf = id;
    },
  } as unknown as ExtensionAPI;

  async emit(event: string, payload: unknown, ctx: ExtensionContext): Promise<void> {
    for (const handler of this.handlers.get(event) ?? []) await handler(payload, ctx);
  }
}

class FakeClient implements CaptureClient {
  readonly calls: Array<{ method: string; captureID?: string; path?: string }> = [];
  readonly captures: Capture[] = [];
  fail = new Set<string>();
  emptySeal = false;
  blockWrites = false;
  nextID = 1;

  async startCapture(
    repositoryRoot: string,
    externalID: string,
    transcriptRef: string,
    startCursor: string,
  ): Promise<Capture> {
    this.maybeFail("start");
    const capture = captureRecord(`capture-${this.nextID++}`, externalID, {
      worktree_root: repositoryRoot,
      transcript_ref: transcriptRef || undefined,
      start_cursor: startCursor,
    });
    this.captures.push(capture);
    this.calls.push({ method: "start", captureID: capture.id });
    return capture;
  }

  async getCapture(captureID: string): Promise<Capture> {
    this.maybeFail("get");
    this.calls.push({ method: "get", captureID });
    const capture = this.captures.find((candidate) => candidate.id === captureID);
    if (!capture) throw new Error("not found");
    return capture;
  }

  async listPendingCaptures(_repositoryRoot: string, externalID?: string): Promise<Capture[]> {
    this.maybeFail("list");
    this.calls.push({ method: "list" });
    return this.captures.filter(
      (capture) =>
        ["open", "pending_summary"].includes(capture.status) &&
        (!externalID || capture.conversation_key.external_id === externalID),
    );
  }

  async recordWrite(captureID: string, path: string, signal?: AbortSignal): Promise<void> {
    this.maybeFail("record");
    this.calls.push({ method: "record", captureID, path });
    if (!this.blockWrites) return;
    await new Promise<void>((_resolve, rejectPromise) => {
      if (signal?.aborted) return rejectPromise(new Error("cancelled"));
      signal?.addEventListener("abort", () => rejectPromise(new Error("cancelled")), { once: true });
    });
  }

  async sealCapture(captureID: string, _endCursor: string): Promise<FinalizationDraft> {
    this.maybeFail("seal");
    this.calls.push({ method: "seal", captureID });
    const capture = this.captures.find((candidate) => candidate.id === captureID)!;
    capture.status = this.emptySeal ? "abandoned" : "pending_summary";
    return {
      capture_id: captureID,
      status: capture.status,
      empty: this.emptySeal,
      paths: this.emptySeal ? [] : ["src/a.ts"],
    };
  }

  private maybeFail(method: string): void {
    if (this.fail.has(method)) throw new Error(`${method} failed`);
  }
}

function captureRecord(id: string, externalID: string, overrides: Partial<Capture> = {}): Capture {
  return {
    id,
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: externalID },
    worktree_root: "/repo",
    status: "open",
    start_cursor: "leaf-1",
    started_at: "2026-01-01T00:00:00Z",
    last_seen_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function testContext(pi: FakePi, sessionFile = "/sessions/current.jsonl") {
  const notify = vi.fn();
  const ctx = {
    cwd: "/repo",
    hasUI: true,
    signal: undefined,
    ui: { notify },
    sessionManager: {
      getSessionFile: () => sessionFile,
      getLeafId: () => pi.leaf,
      getBranch: () => pi.entries,
    },
  } as unknown as ExtensionContext;
  return { ctx, notify, externalID: resolve(sessionFile) };
}

function setup(now: () => number = () => 0) {
  const pi = new FakePi();
  const client = new FakeClient();
  const state = new PiState(pi.api);
  const lifecycle = new CaptureLifecycle(client, state, async () => true, now);
  lifecycle.register(pi.api);
  const context = testContext(pi);
  return { pi, client, lifecycle, ...context };
}

function mutation(toolName: string, path: unknown, isError = false) {
  return {
    type: "tool_result",
    toolCallId: "call-1",
    toolName,
    input: { path },
    content: [{ type: "text", text: "result" }],
    details: undefined,
    isError,
  };
}

describe("Capture lifecycle", () => {
  it.each(["startup", "new", "resume", "fork"] as const)(
    "starts a distinct Capture for session_start: %s",
    async (reason) => {
      const { pi, client, lifecycle, ctx } = setup();
      await pi.emit("session_start", { type: "session_start", reason }, ctx);

      expect(client.calls.map((call) => call.method)).toEqual(["list", "start"]);
      expect(lifecycle.currentCaptureID()).toBe("capture-1");
    },
  );

  it("reattaches the persisted open Capture on reload", async () => {
    const { pi, client, lifecycle, ctx, externalID } = setup();
    client.captures.push(captureRecord("capture-existing", externalID));
    pi.entries.push({
      type: "custom",
      id: "state-existing",
      parentId: "leaf-1",
      customType: stateEntryType,
      data: {
        version: 1,
        conversation_id: externalID,
        capture_id: "capture-existing",
        injected_paths: ["src/a.ts"],
      },
    });
    pi.leaf = "state-existing";

    await pi.emit("session_start", { type: "session_start", reason: "reload" }, ctx);

    expect(client.calls.map((call) => call.method)).toEqual(["get"]);
    expect(lifecycle.currentCaptureID()).toBe("capture-existing");
  });

  it("falls back to the Conversation's single open Capture when reload state is missing", async () => {
    const { pi, client, lifecycle, ctx, externalID } = setup();
    client.captures.push(captureRecord("capture-existing", externalID));

    await pi.emit("session_start", { type: "session_start", reason: "reload" }, ctx);

    expect(client.calls.map((call) => call.method)).toEqual(["list"]);
    expect(lifecycle.currentCaptureID()).toBe("capture-existing");
    expect(pi.entries.at(-1)?.data.capture_id).toBe("capture-existing");
  });

  it("does not reattach an older open Capture on non-reload startup", async () => {
    const { pi, client, lifecycle, ctx, notify, externalID } = setup();
    client.captures.push(captureRecord("capture-old", externalID));

    await pi.emit("session_start", { type: "session_start", reason: "resume" }, ctx);

    expect(lifecycle.currentCaptureID()).toBeUndefined();
    expect(client.calls.map((call) => call.method)).toEqual(["list"]);
    expect(notify).toHaveBeenCalledOnce();
  });

  it.each(["quit", "new", "resume", "fork"] as const)(
    "seals on session_shutdown: %s",
    async (reason) => {
      const { pi, client, lifecycle, ctx } = setup();
      await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);
      await pi.emit("session_shutdown", { type: "session_shutdown", reason }, ctx);

      expect(client.calls.filter((call) => call.method === "seal")).toHaveLength(1);
      expect(lifecycle.currentCaptureID()).toBeUndefined();
    },
  );

  it("preserves the open Capture on reload shutdown", async () => {
    const { pi, client, lifecycle, ctx } = setup();
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);
    await pi.emit("session_shutdown", { type: "session_shutdown", reason: "reload" }, ctx);

    expect(client.calls.some((call) => call.method === "seal")).toBe(false);
    expect(lifecycle.currentCaptureID()).toBe("capture-1");
  });

  it("records successful edit/write paths and throttles repeats for 30 seconds", async () => {
    let now = 1_000;
    const { pi, client, ctx } = setup(() => now);
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    await pi.emit("tool_result", mutation("edit", "src/a.ts"), ctx);
    now += 29_999;
    await pi.emit("tool_result", mutation("write", "src/a.ts"), ctx);
    now += 2;
    await pi.emit("tool_result", mutation("write", "src/a.ts"), ctx);

    expect(client.calls.filter((call) => call.method === "record")).toEqual([
      expect.objectContaining({ path: resolve("/repo", "src/a.ts") }),
      expect.objectContaining({ path: resolve("/repo", "src/a.ts") }),
    ]);
  });

  it("ignores failed mutations, reads, Bash, and malformed paths", async () => {
    const { pi, client, ctx } = setup();
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    await pi.emit("tool_result", mutation("edit", "src/a.ts", true), ctx);
    await pi.emit("tool_result", mutation("read", "src/a.ts"), ctx);
    await pi.emit("tool_result", mutation("bash", "src/a.ts"), ctx);
    await pi.emit("tool_result", mutation("write", 42), ctx);

    expect(client.calls.some((call) => call.method === "record")).toBe(false);
  });

  it("cancels an in-flight write before non-reload sealing", async () => {
    const { pi, client, ctx } = setup();
    client.blockWrites = true;
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    const write = pi.emit("tool_result", mutation("write", "src/a.ts"), ctx);
    await vi.waitFor(() => {
      expect(client.calls.some((call) => call.method === "record")).toBe(true);
    });
    await pi.emit("session_shutdown", { type: "session_shutdown", reason: "quit" }, ctx);
    await expect(write).resolves.toBeUndefined();

    expect(client.calls.map((call) => call.method)).toEqual(["list", "start", "record", "seal"]);
  });

  it("treats an empty seal as successful abandonment", async () => {
    const { pi, client, lifecycle, ctx } = setup();
    client.emptySeal = true;
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);
    await pi.emit("session_shutdown", { type: "session_shutdown", reason: "quit" }, ctx);

    expect(client.captures[0]?.status).toBe("abandoned");
    expect(lifecycle.currentCaptureID()).toBeUndefined();
  });

  it("fails open on list, start, reload lookup, write, and seal RPC errors", async () => {
    const listFailure = setup();
    listFailure.client.fail.add("list");
    await expect(
      listFailure.pi.emit(
        "session_start",
        { type: "session_start", reason: "startup" },
        listFailure.ctx,
      ),
    ).resolves.toBeUndefined();

    const startFailure = setup();
    startFailure.client.fail.add("start");
    await expect(
      startFailure.pi.emit(
        "session_start",
        { type: "session_start", reason: "startup" },
        startFailure.ctx,
      ),
    ).resolves.toBeUndefined();
    expect(startFailure.lifecycle.currentCaptureID()).toBeUndefined();

    const reloadFailure = setup();
    reloadFailure.pi.entries.push({
      type: "custom",
      id: "state-existing",
      parentId: "leaf-1",
      customType: stateEntryType,
      data: {
        version: 1,
        conversation_id: reloadFailure.externalID,
        capture_id: "capture-missing",
        injected_paths: [],
      },
    });
    reloadFailure.pi.leaf = "state-existing";
    reloadFailure.client.fail.add("get");
    reloadFailure.client.fail.add("list");
    await expect(
      reloadFailure.pi.emit(
        "session_start",
        { type: "session_start", reason: "reload" },
        reloadFailure.ctx,
      ),
    ).resolves.toBeUndefined();

    const active = setup();
    await active.pi.emit("session_start", { type: "session_start", reason: "startup" }, active.ctx);
    active.client.fail.add("record");
    await expect(active.pi.emit("tool_result", mutation("write", "a.ts"), active.ctx)).resolves.toBeUndefined();
    active.client.fail.add("seal");
    await expect(
      active.pi.emit("session_shutdown", { type: "session_shutdown", reason: "quit" }, active.ctx),
    ).resolves.toBeUndefined();
    expect(active.lifecycle.currentCaptureID()).toBe("capture-1");
  });
});
