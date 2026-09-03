import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";

import { registerMadeleine } from "./index.ts";
import { binaryInstallMessage, RPCClient } from "./rpc.ts";
import {
  createFakeMadeleine,
  type FakeAction,
  type FakeMadeleine,
  healthyDoctorResult,
} from "./test-helper.ts";

type Handler = (event: any, context: ExtensionContext) => Promise<unknown> | unknown;

class FakeExtension {
  readonly handlers = new Map<string, Handler[]>();

  readonly api = {
    on: (event: string, handler: Handler) => {
      const handlers = this.handlers.get(event) ?? [];
      handlers.push(handler);
      this.handlers.set(event, handlers);
    },
    registerTool: () => undefined,
    registerCommand: () => undefined,
    appendEntry: () => undefined,
  } as unknown as ExtensionAPI;

  async emit(event: string, payload: unknown, context: ExtensionContext): Promise<unknown[]> {
    return Promise.all((this.handlers.get(event) ?? []).map((handler) => handler(payload, context)));
  }
}

const fakes: FakeMadeleine[] = [];

afterEach(async () => {
  await Promise.all(fakes.splice(0).map((fake) => fake.cleanup()));
});

function context(cwd = "/repo", hasUI = true) {
  const notify = vi.fn();
  const value = {
    cwd,
    hasUI,
    signal: undefined,
    sessionManager: {
      getSessionFile: () => "/sessions/current.jsonl",
      getSessionId: () => "018f0000-0000-7000-8000-000000000123",
      getLeafId: () => "leaf-1",
      getBranch: () => [],
      getEntries: () => [],
    },
    ui: { notify },
  } as unknown as ExtensionContext;
  return { value, notify };
}

async function extensionWith(action: FakeAction, clientOptions = {}, cwd = "/repo with spaces") {
  const fake = await createFakeMadeleine({
    doctor: { result: healthyDoctorResult() },
    methods: {
      "capture.list_pending": { result: [] },
      "capture.start": { result: captureResult() },
      "context.for_paths": action,
    },
  });
  fakes.push(fake);
  const extension = new FakeExtension();
  registerMadeleine(
    extension.api,
    new RPCClient({ env: { MADELEINE_BIN: fake.binary }, ...clientOptions }),
  );
  const ctx = context(cwd);
  await extension.emit("session_start", { reason: "startup" }, ctx.value);
  return { extension, fake, ctx };
}

function readEvent(path: unknown, options: { isError?: boolean } = {}) {
  return {
    type: "tool_result",
    toolCallId: "call-1",
    toolName: "read",
    input: { path },
    content: [{ type: "text", text: "original file content" }],
    details: { truncation: { truncated: false } },
    isError: options.isError ?? false,
  };
}

function captureResult() {
  return {
    id: "capture-1",
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: "018f0000-0000-7000-8000-000000000123" },
    worktree_root: "/repo",
    status: "open",
    start_cursor: "leaf-1",
    started_at: "2026-01-01T00:00:00Z",
    last_seen_at: "2026-01-01T00:00:00Z",
  };
}

function oneSummary() {
  return {
    episode_id: "episode-1",
    ended_at: "2026-01-01T00:01:00Z",
    harness: "pi",
    l1: "Historical summary",
  };
}

describe("read enrichment", () => {
  it("preserves a successful read with zero summaries", async () => {
    const { extension, ctx } = await extensionWith({ contextEpisodes: [] });
    const event = readEvent("src/a.ts");

    const [patch] = await extension.emit("tool_result", event, ctx.value);
    expect(patch).toBeUndefined();
    expect(event.content).toEqual([{ type: "text", text: "original file content" }]);
  });

  it("appends one context block while leaving content and details intact", async () => {
    const { extension, ctx } = await extensionWith({ contextEpisodes: [oneSummary()] });
    const event = readEvent("src/été file.ts");

    const [patch] = await extension.emit("tool_result", event, ctx.value);
    expect(patch).toMatchObject({
      content: [
        { type: "text", text: "original file content" },
        { type: "text", text: expect.stringContaining("<madeleine-context") },
      ],
    });
    expect(event.details).toEqual({ truncation: { truncated: false } });
    expect(event.content).toHaveLength(1);
  });

  it("normalizes Pi read paths before lookup and deduplication", async () => {
    const { extension, fake, ctx } = await extensionWith({ contextEpisodes: [oneSummary()] });

    const [first] = await extension.emit("tool_result", readEvent("@src/a.ts"), ctx.value);
    const [repeat] = await extension.emit("tool_result", readEvent("src/a.ts"), ctx.value);
    const [distinct] = await extension.emit("tool_result", readEvent("src/b.ts"), ctx.value);

    expect(first).toBeDefined();
    expect(repeat).toBeUndefined();
    expect(distinct).toBeDefined();
    const requests = await fake.requests();
    expect(requests.find((request) => request.args[1] === "context.for_paths")).toMatchObject({
      request: {
        params: { paths: [resolve(ctx.value.cwd, "src/a.ts")] },
      },
    });
  });

  it("normalizes file URLs and deduplicates their absolute path", async () => {
    const repository = await mkdtemp(join(tmpdir(), "madeleine-repository-"));
    const path = join(repository, "src", "a.ts");
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, "content\n");

    try {
      const { extension, fake, ctx } = await extensionWith(
        { contextEpisodes: [oneSummary()] },
        {},
        repository,
      );
      const [fileURL] = await extension.emit(
        "tool_result",
        readEvent(pathToFileURL(path).href),
        ctx.value,
      );
      const [absolute] = await extension.emit("tool_result", readEvent(path), ctx.value);

      expect(fileURL).toBeDefined();
      expect(absolute).toBeUndefined();
      const requests = await fake.requests();
      expect(requests.find((request) => request.args[1] === "context.for_paths")).toMatchObject({
        request: { params: { paths: [path] } },
      });
    } finally {
      await rm(repository, { recursive: true, force: true });
    }
  });

  it("keeps five summaries in the order returned by the core", async () => {
    const episodes = [5, 4, 3, 2, 1].map((index) => ({
      ...oneSummary(),
      episode_id: `episode-${index}`,
      l1: `summary-${index}`,
    }));
    const { extension, ctx } = await extensionWith({ contextEpisodes: episodes });

    const [patch] = await extension.emit("tool_result", readEvent("src/a.ts"), ctx.value);
    const block = (patch as any).content[1].text as string;
    expect(block.match(/episode-\d/g)).toEqual([
      "episode-5",
      "episode-4",
      "episode-3",
      "episode-2",
      "episode-1",
    ]);
  });

  it("ignores failed reads and malformed read input", async () => {
    const { extension, fake, ctx } = await extensionWith({ contextEpisodes: [oneSummary()] });

    const [failed] = await extension.emit("tool_result", readEvent("src/a.ts", { isError: true }), ctx.value);
    const [malformed] = await extension.emit("tool_result", readEvent(42), ctx.value);
    const [invalidURL] = await extension.emit("tool_result", readEvent("file://%"), ctx.value);

    expect(failed).toBeUndefined();
    expect(malformed).toBeUndefined();
    expect(invalidURL).toBeUndefined();
    expect((await fake.requests()).filter((request) => request.args[1] === "context.for_paths")).toHaveLength(0);
  });

  it.each([
    ["lookup timeout", { result: [], delayMs: 200 }, { timeoutMs: 20 }],
    ["bad JSON", { rawStdout: "bad json" }, {}],
    ["wrong protocol", { result: [], protocolVersion: 2 }, {}],
    ["SQLite busy", { error: { code: "database_busy", message: "database is busy" } }, {}],
    ["nonzero child", { result: [], exitCode: 1 }, {}],
  ] as const)("fails open on %s", async (_name, action, options) => {
    const { extension, ctx } = await extensionWith(action, options);
    const event = readEvent("src/a.ts");

    const [patch] = await extension.emit("tool_result", event, ctx.value);
    expect(patch).toBeUndefined();
    expect(event.content).toEqual([{ type: "text", text: "original file content" }]);
  });

  it("fails open when the binary is missing", async () => {
    const extension = new FakeExtension();
    registerMadeleine(
      extension.api,
      new RPCClient({ env: { MADELEINE_BIN: "/missing/madeleine" }, timeoutMs: 20 }),
    );
    const ctx = context();
    await extension.emit("session_start", { reason: "startup" }, ctx.value);

    const [patch] = await extension.emit("tool_result", readEvent("src/a.ts"), ctx.value);
    expect(patch).toBeUndefined();
    expect(ctx.notify).toHaveBeenCalledWith(binaryInstallMessage, "warning");
  });
});

describe("startup detection", () => {
  it("notifies at most once when a required doctor check fails", async () => {
    const unhealthy = healthyDoctorResult();
    unhealthy.checks.find((check) => check.name === "repository")!.ok = false;
    const fake = await createFakeMadeleine({ doctor: { result: unhealthy, exitCode: 1 } });
    fakes.push(fake);
    const extension = new FakeExtension();
    registerMadeleine(extension.api, new RPCClient({ env: { MADELEINE_BIN: fake.binary } }));
    const ctx = context();

    await extension.emit("session_start", { reason: "startup" }, ctx.value);
    await extension.emit("session_start", { reason: "reload" }, ctx.value);

    expect(ctx.notify).toHaveBeenCalledTimes(1);
    expect(await fake.requests()).toHaveLength(1);
  });

  it("does not notify or prompt in noninteractive mode", async () => {
    const fake = await createFakeMadeleine({
      doctor: { rawStdout: "bad response", exitCode: 1 },
    });
    fakes.push(fake);
    const extension = new FakeExtension();
    registerMadeleine(extension.api, new RPCClient({ env: { MADELEINE_BIN: fake.binary } }));
    const ctx = context("/repo", false);

    await extension.emit("session_start", { reason: "startup" }, ctx.value);

    expect(ctx.notify).not.toHaveBeenCalled();
  });
});
