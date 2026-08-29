import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";

import {
  CaptureLifecycle,
  type CaptureClient,
  type CaptureFinalizer,
} from "./lifecycle.ts";
import type { Capture, Episode, FinalizationDraft } from "./rpc.ts";
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

  async sealCapture(captureID: string, endCursor: string): Promise<FinalizationDraft> {
    this.maybeFail("seal");
    this.calls.push({ method: "seal", captureID });
    const capture = this.captures.find((candidate) => candidate.id === captureID)!;
    capture.status = this.emptySeal ? "abandoned" : "pending_summary";
    capture.end_cursor = endCursor;
    return {
      capture_id: captureID,
      status: capture.status,
      empty: this.emptySeal,
      paths: this.emptySeal ? [] : ["src/a.ts"],
    };
  }

  async publishEpisode(captureID: string, l1: string, l2: string): Promise<Episode> {
    this.maybeFail("publish");
    this.calls.push({ method: "publish", captureID });
    const capture = this.captures.find((candidate) => candidate.id === captureID)!;
    capture.status = "finalized";
    capture.episode_id = `episode-${captureID}`;
    return episodeRecord(capture, l1, l2);
  }

  private maybeFail(method: string): void {
    if (this.fail.has(method)) throw new Error(`${method} failed`);
  }
}

function episodeRecord(capture: Capture, l1: string, l2: string): Episode {
  return {
    id: capture.episode_id!,
    capture_id: capture.id,
    repository_id: capture.repository_id,
    conversation_id: capture.conversation_id,
    conversation_key: capture.conversation_key,
    harness: "pi",
    paths: ["src/a.ts"],
    l1,
    l2,
    start_cursor: capture.start_cursor,
    end_cursor: capture.end_cursor!,
    started_at: capture.started_at,
    ended_at: "2026-01-01T00:01:00Z",
    created_at: "2026-01-01T00:01:01Z",
  };
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
      getEntries: () => pi.entries,
    },
  } as unknown as ExtensionContext;
  return { ctx, notify, externalID: resolve(sessionFile) };
}

function setup(
  now: () => number = () => 0,
  finalizer?: CaptureFinalizer,
) {
  const pi = new FakePi();
  const client = new FakeClient();
  const state = new PiState(pi.api);
  const lifecycle = new CaptureLifecycle(client, state, async () => true, now, finalizer);
  lifecycle.register(pi.api);
  const context = testContext(pi);
  return { pi, client, state, lifecycle, ...context };
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

  it.each(["startup", "reload", "resume"] as const)(
    "reattaches the persisted open Capture on session_start: %s",
    async (reason) => {
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

      await pi.emit("session_start", { type: "session_start", reason }, ctx);

      expect(client.calls.map((call) => call.method)).toEqual(["get"]);
      expect(lifecycle.currentCaptureID()).toBe("capture-existing");
    },
  );

  it("preserves injected paths when fallback confirms the persisted Capture", async () => {
    const { pi, client, state, lifecycle, ctx, externalID } = setup();
    client.captures.push(captureRecord("capture-existing", externalID));
    client.fail.add("get");
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

    expect(client.calls.map((call) => call.method)).toEqual(["list"]);
    expect(lifecycle.currentCaptureID()).toBe("capture-existing");
    expect(state.claimPath("src/a.ts")).toBe(false);
    expect(pi.entries).toHaveLength(1);
  });

  it.each(["reload", "resume"] as const)(
    "reattaches the Conversation's open Capture without persisted state on %s",
    async (reason) => {
      const { pi, client, lifecycle, ctx, externalID } = setup();
      client.captures.push(captureRecord("capture-existing", externalID));

      await pi.emit("session_start", { type: "session_start", reason }, ctx);

      expect(client.calls.map((call) => call.method)).toEqual(["list"]);
      expect(lifecycle.currentCaptureID()).toBe("capture-existing");
      expect(pi.entries.at(-1)?.data.capture_id).toBe("capture-existing");
    },
  );

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

  it("publishes on every clean shutdown reason", async () => {
    for (const reason of ["quit", "new", "resume", "fork"] as const) {
      const finalize = vi.fn(async (sealed: FinalizationDraft) => ({
        captureID: sealed.capture_id,
        status: "published" as const,
        episodeID: `episode-${sealed.capture_id}`,
      }));
      const { pi, ctx } = setup(() => 0, { finalize });
      await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);
      await pi.emit("session_shutdown", { type: "session_shutdown", reason }, ctx);
      expect(finalize).toHaveBeenCalledOnce();
    }
  });

  it("rolls over the current Capture without changing Conversation", async () => {
    const { pi, client, lifecycle, ctx, externalID } = setup();
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    const result = await lifecycle.rollover(ctx);

    expect(result).toEqual({
      finalization: { captureID: "capture-1", status: "pending" },
      startedCaptureID: "capture-2",
    });
    expect(client.calls.map((call) => call.method)).toEqual(["list", "start", "seal", "get", "start"]);
    expect(client.captures[1]?.conversation_key.external_id).toBe(externalID);
    expect(lifecycle.currentCaptureID()).toBe("capture-2");
  });

  it("publishes before starting the rollover replacement", async () => {
    const finalize = vi.fn(async (sealed: FinalizationDraft) => ({
      captureID: sealed.capture_id,
      status: "published" as const,
      episodeID: "episode-1",
    }));
    const { pi, client, lifecycle, ctx } = setup(() => 0, { finalize });
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    await expect(lifecycle.rollover(ctx)).resolves.toEqual({
      finalization: {
        captureID: "capture-1",
        status: "published",
        episodeID: "episode-1",
      },
      startedCaptureID: "capture-2",
    });
    expect(client.calls.map((call) => call.method)).toEqual(["list", "start", "seal", "start"]);
  });

  it("keeps the current Capture usable when rollover sealing fails", async () => {
    const { pi, client, lifecycle, ctx } = setup();
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);
    client.fail.add("seal");

    await expect(lifecycle.rollover(ctx)).rejects.toThrow("seal failed");
    expect(lifecycle.currentCaptureID()).toBe("capture-1");

    client.fail.delete("seal");
    await pi.emit("tool_result", mutation("write", "src/a.ts"), ctx);
    expect(client.calls.at(-1)).toMatchObject({ method: "record", captureID: "capture-1" });
  });

  it("starts one rollover replacement when summary publication fails", async () => {
    const finalize = vi.fn(async () => {
      throw new Error("summary failed");
    });
    const { pi, client, lifecycle, ctx } = setup(() => 0, { finalize });
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    await expect(lifecycle.rollover(ctx)).resolves.toEqual({
      finalization: { captureID: "capture-1", status: "pending" },
      startedCaptureID: "capture-2",
    });
    expect(client.calls.filter((call) => call.method === "start")).toHaveLength(2);
  });

  it("retries pending Captures oldest-first and continues after failure", async () => {
    const attempted: string[] = [];
    const finalizer: CaptureFinalizer = {
      async finalize(sealed) {
        attempted.push(sealed.capture_id);
        if (sealed.capture_id === "capture-old") throw new Error("summary failed");
        return {
          captureID: sealed.capture_id,
          status: "published",
          episodeID: `episode-${sealed.capture_id}`,
        };
      },
    };
    const { pi, client, lifecycle, ctx, externalID } = setup(() => 0, finalizer);
    client.captures.push(
      captureRecord("capture-old", externalID, { status: "pending_summary", end_cursor: "end-old" }),
      captureRecord("capture-new", externalID, { status: "pending_summary", end_cursor: "end-new" }),
    );
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);

    await expect(lifecycle.retry(undefined, ctx)).resolves.toEqual([
      { captureID: "capture-old", status: "failed" },
      { captureID: "capture-new", status: "published", episodeID: "episode-capture-new" },
    ]);
    expect(attempted).toEqual(["capture-old", "capture-new"]);
  });

  it("requires an explicit retry ID to be pending in the current Conversation", async () => {
    const { pi, lifecycle, ctx } = setup();
    await pi.emit("session_start", { type: "session_start", reason: "startup" }, ctx);
    await expect(lifecycle.retry("capture-other", ctx)).rejects.toThrow("not pending");
  });

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

    expect(client.calls.map((call) => call.method)).toEqual(["list", "start", "record", "seal", "get"]);
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
