import { execFile } from "node:child_process";
import { mkdir, mkdtemp, rm, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { registerMadeleine } from "../../harnesses/pi/index.ts";
import { RPCClient, type Capture, type EpisodeDetail } from "../../harnesses/pi/rpc.ts";

const execFileAsync = promisify(execFile);
const workspaceRoot = fileURLToPath(new URL("../..", import.meta.url));
let compiledBinary = "";
let buildDirectory = "";

beforeAll(async () => {
  buildDirectory = await mkdtemp(join(tmpdir(), "madeleine-e2e-bin-"));
  compiledBinary = join(buildDirectory, "madeleine");
  await execFileAsync("go", ["build", "-o", compiledBinary, "./cmd/madeleine"], {
    cwd: workspaceRoot,
  });
}, 30_000);

afterAll(async () => {
  await rm(buildDirectory, { recursive: true, force: true });
});

type EventHandler = (event: any, ctx: any) => Promise<unknown> | unknown;
type CommandHandler = (argumentsText: string, ctx: any) => Promise<void>;
type Tool = {
  execute(
    toolCallID: string,
    params: any,
    signal: AbortSignal,
    onUpdate: (result: unknown) => void,
    ctx: any,
  ): Promise<any>;
};

class DeterministicModels {
  readonly model = {
    id: "madeleine-e2e-model",
    name: "Madeleine E2E",
    provider: "test",
    contextWindow: 128_000,
    maxTokens: 8_192,
  };
  readonly prompts: string[] = [];
  activeCalls = 0;
  maximumActiveCalls = 0;
  malformedResponses = 0;
  private blockNextCall = false;
  private blockedResolve: (() => void) | undefined;
  private releaseResolve: (() => void) | undefined;

  hasConfiguredAuth(): boolean {
    return true;
  }

  blockNext(): void {
    this.blockNextCall = true;
  }

  async waitUntilBlocked(): Promise<void> {
    if (!this.blockNextCall && this.releaseResolve) return;
    await new Promise<void>((resolvePromise) => {
      this.blockedResolve = resolvePromise;
    });
  }

  release(): void {
    this.releaseResolve?.();
    this.releaseResolve = undefined;
  }

  async complete(_model: unknown, request: any): Promise<any> {
    const prompt = request.messages[0].content[0].text as string;
    this.prompts.push(prompt);
    this.activeCalls++;
    this.maximumActiveCalls = Math.max(this.maximumActiveCalls, this.activeCalls);
    try {
      if (this.blockNextCall) {
        this.blockNextCall = false;
        await new Promise<void>((resolvePromise) => {
          this.releaseResolve = resolvePromise;
          this.blockedResolve?.();
          this.blockedResolve = undefined;
        });
      }
      let text: string;
      if (this.malformedResponses > 0) {
        this.malformedResponses--;
        text = "not JSON";
      } else {
        text = JSON.stringify({
          l1: "Madeleine recorded the completed file changes and their intent.",
          l2: "The agent intentionally changed the recorded paths. The persisted bounded evidence contains the conversation and structured mutation outcomes used for this summary.",
        });
      }
      return {
        role: "assistant",
        content: [{ type: "text", text }],
        stopReason: "stop",
        usage: { input: 1, output: 1, cacheRead: 0, cacheWrite: 0, totalTokens: 2 },
        timestamp: Date.now(),
      };
    } finally {
      this.activeCalls--;
    }
  }
}

class FakePiRuntime {
  readonly handlers = new Map<string, EventHandler[]>();
  readonly commands = new Map<string, CommandHandler>();
  readonly tools = new Map<string, Tool>();
  readonly notifications: Array<{ message: string; level: string }> = [];
  readonly entries: any[];
  readonly ctx: any;
  readonly api: any;
  leaf: string | null;
  private nextEntry = 1;

  constructor(
    readonly repository: string,
    readonly sessionID: string,
    readonly sessionFile: string,
    client: RPCClient,
    readonly models: DeterministicModels,
    restoredEntries: any[] = [],
  ) {
    this.entries = restoredEntries.map((entry) => structuredClone(entry));
    this.leaf = this.entries.at(-1)?.id ?? null;
    this.nextEntry = this.entries.length + 1;
    this.api = {
      on: (event: string, handler: EventHandler) => {
        const handlers = this.handlers.get(event) ?? [];
        handlers.push(handler);
        this.handlers.set(event, handlers);
      },
      appendEntry: (customType: string, data: unknown) => {
        this.append({ type: "custom", customType, data });
      },
      registerCommand: (name: string, command: { handler: CommandHandler }) => {
        this.commands.set(name, command.handler);
      },
      registerTool: (tool: Tool & { name: string }) => {
        this.tools.set(tool.name, tool);
      },
    };
    this.ctx = {
      cwd: repository,
      hasUI: true,
      signal: undefined,
      model: models.model,
      modelRegistry: models,
      sessionManager: {
        getSessionFile: () => sessionFile,
        getSessionId: () => sessionID,
        getLeafId: () => this.leaf,
        getBranch: () => this.branch(),
        getEntries: () => this.entries,
      },
      ui: {
        notify: (message: string, level: string) => {
          this.notifications.push({ message, level });
        },
        confirm: async () => true,
      },
      waitForIdle: async () => undefined,
    };
    registerMadeleine(this.api, client);
  }

  async start(reason: "startup" | "reload" | "new" | "resume" | "fork" = "startup"): Promise<void> {
    await this.emit("session_start", { type: "session_start", reason });
  }

  async shutdown(reason: "quit" | "reload" | "new" | "resume" | "fork" = "quit"): Promise<void> {
    await this.emit("session_shutdown", { type: "session_shutdown", reason });
  }

  async write(path: string, label = path): Promise<void> {
    const absolutePath = resolve(this.repository, path);
    await mkdir(dirname(absolutePath), { recursive: true });
    await writeFile(absolutePath, `${label}\n`, "utf8");
    const toolCallID = `write-${this.nextEntry}`;
    this.append({
      type: "message",
      message: { role: "user", content: `Change ${label}`, timestamp: Date.now() },
    });
    this.append({
      type: "message",
      message: {
        role: "assistant",
        content: [{ type: "toolCall", id: toolCallID, name: "write", arguments: { path, content: label } }],
      },
    });
    this.append({
      type: "message",
      message: {
        role: "toolResult",
        toolCallId: toolCallID,
        toolName: "write",
        content: [{ type: "text", text: "written" }],
        isError: false,
      },
    });
    await this.emit("tool_result", {
      type: "tool_result",
      toolCallId: toolCallID,
      toolName: "write",
      input: { path, content: label },
      content: [{ type: "text", text: "written" }],
      details: undefined,
      isError: false,
    });
  }

  addConversationMessages(count: number): void {
    for (let index = 0; index < count; index++) {
      this.append({
        type: "message",
        message: { role: "user", content: `Follow-up ${index}`, timestamp: Date.now() },
      });
    }
  }

  async read(path: string): Promise<any> {
    const results = await this.emit("tool_result", {
      type: "tool_result",
      toolCallId: `read-${this.nextEntry}`,
      toolName: "read",
      input: { path },
      content: [{ type: "text", text: "current file" }],
      details: undefined,
      isError: false,
    });
    return results.find((result) => result !== undefined);
  }

  async command(argumentsText: string): Promise<void> {
    await this.commands.get("madeleine")!(argumentsText, this.ctx);
  }

  async tool(name: string, params: unknown): Promise<any> {
    return this.tools.get(name)!.execute(
      `tool-${this.nextEntry}`,
      params,
      new AbortController().signal,
      () => undefined,
      this.ctx,
    );
  }

  private append(entry: Record<string, unknown>): void {
    const id = `entry-${this.nextEntry++}`;
    this.entries.push({
      id,
      parentId: this.leaf,
      timestamp: new Date().toISOString(),
      ...entry,
    });
    this.leaf = id;
  }

  private branch(): any[] {
    const byID = new Map(this.entries.map((entry) => [entry.id, entry]));
    const branch: any[] = [];
    const visited = new Set<string>();
    let cursor = this.leaf;
    while (cursor && !visited.has(cursor)) {
      visited.add(cursor);
      const entry = byID.get(cursor);
      if (!entry) break;
      branch.push(entry);
      cursor = entry.parentId;
    }
    return branch.reverse();
  }

  async emit(event: string, payload: unknown): Promise<unknown[]> {
    const results: unknown[] = [];
    for (const handler of this.handlers.get(event) ?? []) {
      results.push(await handler(payload, this.ctx));
    }
    return results;
  }
}

interface Fixture {
  root: string;
  repository: string;
  home: string;
  sessionFile: string;
  client: RPCClient;
  cleanup(): Promise<void>;
}

async function fixture(): Promise<Fixture> {
  const root = await mkdtemp(join(tmpdir(), "madeleine-e2e-"));
  const repositoryDirectory = join(root, "repository");
  const home = join(root, "home");
  const sessionFile = join(root, "pi-session.jsonl");
  await mkdir(repositoryDirectory, { recursive: true });
  await execFileAsync("git", ["init"], { cwd: repositoryDirectory });
  const repository = (await execFileAsync("git", ["rev-parse", "--show-toplevel"], {
    cwd: repositoryDirectory,
  })).stdout.trim();
  await writeFile(sessionFile, "test session\n", "utf8");
  const client = new RPCClient({
    env: {
      MADELEINE_BIN: compiledBinary,
      MADELEINE_HOME: home,
    },
  });
  return {
    root,
    repository,
    home,
    sessionFile,
    client,
    cleanup: () => rm(root, { recursive: true, force: true }),
  };
}

async function openCaptures(test: Fixture, externalID: string): Promise<Capture[]> {
  return (await test.client.listPendingCaptures(test.repository, externalID))
    .filter((capture) => capture.status === "open");
}

async function waitForNoPending(test: Fixture, externalID: string): Promise<void> {
  await vi.waitFor(async () => {
    const captures = await test.client.listPendingCaptures(test.repository, externalID);
    expect(captures.filter((capture) => capture.status === "pending_summary")).toHaveLength(0);
  }, { timeout: 5_000, interval: 25 });
}

async function episodeForPath(test: Fixture, path: string): Promise<EpisodeDetail | undefined> {
  const contexts = await test.client.contextForPath(test.repository, path);
  const episodeID = contexts[0]?.episodes[0]?.episode_id;
  if (!episodeID) return undefined;
  return test.client.getEpisode(test.repository, episodeID);
}

describe("Pi MVP with the real Madeleine binary", () => {
  it("publishes a clean interval and retrieves L1, L2, compact, and paged raw evidence after reload", async () => {
    const test = await fixture();
    try {
      const models = new DeterministicModels();
      const sessionA = "018f0000-0000-7000-8000-0000000000a1";
      const intervalA = new FakePiRuntime(
        test.repository,
        sessionA,
        test.sessionFile,
        test.client,
        models,
      );
      await intervalA.start();
      await intervalA.write("src/a.ts", "first file");
      await intervalA.write("src/b.ts", "second file");
      intervalA.addConversationMessages(55);

      expect(intervalA.notifications).toEqual([]);
      const capturesBeforeReload = await openCaptures(test, sessionA);
      expect(capturesBeforeReload).toHaveLength(1);
      const captureBeforeReload = capturesBeforeReload[0]!;
      await intervalA.shutdown("reload");
      await intervalA.start("reload");
      const capturesAfterReload = await openCaptures(test, sessionA);
      expect(capturesAfterReload, JSON.stringify(intervalA.notifications)).toHaveLength(1);
      const captureAfterReload = capturesAfterReload[0]!;
      expect(captureAfterReload.id).toBe(captureBeforeReload.id);
      expect(captureAfterReload.start_cursor).toBe(captureBeforeReload.start_cursor);

      await intervalA.shutdown();
      await unlink(test.sessionFile);

      const contextA = await test.client.contextForPath(test.repository, "src/a.ts");
      expect(contextA[0]?.episodes).toHaveLength(1);
      expect(contextA[0]?.episodes[0]?.l1).toContain("recorded the completed file changes");
      const episodeID = contextA[0]!.episodes[0]!.episode_id;

      const sessionB = "018f0000-0000-7000-8000-0000000000b1";
      const intervalB = new FakePiRuntime(
        test.repository,
        sessionB,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
      );
      await intervalB.start("new");
      const readPatch = await intervalB.read("src/a.ts");
      const secondPathPatch = await intervalB.read("src/b.ts");
      expect(readPatch.content[1].text).toContain(episodeID);
      expect(readPatch.content[1].text).toContain("recorded the completed file changes");
      expect(secondPathPatch.content[1].text).toContain(episodeID);

      const episodeResult = await intervalB.tool("madeleine_episode", { episode_id: episodeID });
      expect(episodeResult.content[0].text).toContain("persisted bounded evidence");
      const episodeDetail = await test.client.getEpisode(test.repository, episodeID);
      expect(episodeResult.content[0].text).toContain(episodeDetail.transcript_id);
      const transcriptID = episodeDetail.transcript_id;

      const compact = await intervalB.tool("madeleine_transcript", {
        transcript_id: transcriptID,
        view: "compact",
      });
      expect(compact.content[0].text).toContain("src/a.ts");
      const firstRaw = await intervalB.tool("madeleine_transcript", {
        transcript_id: transcriptID,
        view: "raw",
        offset: 0,
      });
      expect(firstRaw.details.next_offset).toBe(50);
      const secondRaw = await intervalB.tool("madeleine_transcript", {
        transcript_id: transcriptID,
        view: "raw",
        offset: firstRaw.details.next_offset,
      });
      expect(secondRaw.content[0].text).toContain("Follow-up 54");

      const captureB = (await openCaptures(test, sessionB))[0]!;
      await intervalB.shutdown("reload");
      await intervalB.start("reload");
      expect((await openCaptures(test, sessionB))[0]?.id).toBe(captureB.id);
      expect(await intervalB.read("src/a.ts")).toBeUndefined();
      expect(await intervalB.read("src/b.ts")).toBeUndefined();
      await intervalB.shutdown();
    } finally {
      await test.cleanup();
    }
  }, 20_000);

  it("reattaches one open Capture after repeated process crashes without inferring downtime files", async () => {
    const test = await fixture();
    try {
      const sessionID = "018f0000-0000-7000-8000-0000000000c1";
      const first = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
      );
      await first.start();
      await first.write("src/crash.ts", "before crash");
      const original = (await openCaptures(test, sessionID))[0]!;
      const forkSessionID = "018f0000-0000-7000-8000-0000000000c2";
      const forked = new FakePiRuntime(
        test.repository,
        forkSessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
        first.entries,
      );
      await forked.start("fork");
      expect((await openCaptures(test, forkSessionID))[0]?.id).not.toBe(original.id);
      await forked.shutdown();

      await writeFile(join(test.repository, "src", "downtime.ts"), "human change\n", "utf8");
      const second = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
        first.entries,
      );
      await second.start();
      const afterFirstCrash = await openCaptures(test, sessionID);
      expect(afterFirstCrash).toHaveLength(1);
      expect(afterFirstCrash[0]?.id).toBe(original.id);
      expect(afterFirstCrash[0]?.start_cursor).toBe(original.start_cursor);

      const third = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
        second.entries,
      );
      await third.start();
      expect((await openCaptures(test, sessionID)).map((capture) => capture.id)).toEqual([
        original.id,
      ]);

      await third.shutdown();
      expect(await episodeForPath(test, "src/crash.ts")).toBeDefined();
      expect(await episodeForPath(test, "src/downtime.ts")).toBeUndefined();
    } finally {
      await test.cleanup();
    }
  }, 20_000);

  it("recovers multiple capture failures oldest-first while the current Capture records independently", async () => {
    const test = await fixture();
    try {
      const sessionID = "018f0000-0000-7000-8000-0000000000d1";
      const failingModels = new DeterministicModels();
      failingModels.malformedResponses = 2;
      const first = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        failingModels,
      );
      await first.start();
      await first.write("src/old.ts", "oldest interval");
      await first.command("capture");
      await first.write("src/middle.ts", "middle interval");
      await first.command("capture");
      await first.shutdown();
      expect((await test.client.listPendingCaptures(test.repository, sessionID))
        .filter((capture) => capture.status === "pending_summary")).toHaveLength(2);

      const recoveryModels = new DeterministicModels();
      recoveryModels.blockNext();
      const resumed = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        recoveryModels,
        first.entries,
      );
      await resumed.start();
      await recoveryModels.waitUntilBlocked();
      await resumed.write("src/current.ts", "current interval");
      const entriesAfterCurrentWrite = resumed.entries.length;
      expect((await openCaptures(test, sessionID))).toHaveLength(1);
      recoveryModels.release();
      await waitForNoPending(test, sessionID);

      expect(resumed.entries).toHaveLength(entriesAfterCurrentWrite);
      expect(recoveryModels.maximumActiveCalls).toBe(1);
      expect(recoveryModels.prompts).toHaveLength(2);
      expect(recoveryModels.prompts[0]).toContain("src/old.ts");
      expect(recoveryModels.prompts[1]).toContain("src/middle.ts");

      await resumed.shutdown();
      const oldEpisode = await episodeForPath(test, "src/old.ts");
      const middleEpisode = await episodeForPath(test, "src/middle.ts");
      const currentEpisode = await episodeForPath(test, "src/current.ts");
      expect(oldEpisode?.paths).toEqual(["src/old.ts"]);
      expect(middleEpisode?.paths).toEqual(["src/middle.ts"]);
      expect(currentEpisode?.paths).toEqual(["src/current.ts"]);
    } finally {
      await test.cleanup();
    }
  }, 20_000);

  it("fails open on a missing summary model and recovers after restart", async () => {
    const test = await fixture();
    try {
      const sessionID = "018f0000-0000-7000-8000-0000000000f1";
      const withoutModel = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
      );
      withoutModel.ctx.model = undefined;
      await withoutModel.start();
      await withoutModel.write("src/model-recovery.ts", "survives missing model");
      await withoutModel.shutdown();

      const pending = (await test.client.listPendingCaptures(test.repository, sessionID))
        .filter((capture) => capture.status === "pending_summary");
      expect(pending).toHaveLength(1);
      expect(withoutModel.notifications.some(({ message }) => message.includes("remains pending")))
        .toBe(true);

      const recovered = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
        withoutModel.entries,
      );
      await recovered.start();
      await waitForNoPending(test, sessionID);
      expect(await episodeForPath(test, "src/model-recovery.ts")).toBeDefined();
      await recovered.shutdown();
    } finally {
      await test.cleanup();
    }
  }, 20_000);

  it("abandons empty work and ignores unstructured or failed mutation attribution", async () => {
    const test = await fixture();
    try {
      const sessionID = "018f0000-0000-7000-8000-0000000000e1";
      const first = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
      );
      await first.start();
      const emptyCapture = (await openCaptures(test, sessionID))[0]!;
      const runtime = new FakePiRuntime(
        test.repository,
        sessionID,
        test.sessionFile,
        test.client,
        new DeterministicModels(),
        first.entries,
      );
      await runtime.start();
      expect((await openCaptures(test, sessionID))[0]?.id).toBe(emptyCapture.id);
      await writeFile(join(test.repository, "human.ts"), "human\n", "utf8");
      await writeFile(join(test.repository, "shell.ts"), "shell\n", "utf8");
      await writeFile(join(test.repository, "generated.ts"), "generated\n", "utf8");
      await writeFile(join(test.repository, "formatted.ts"), "formatted\n", "utf8");
      await writeFile(join(test.repository, "other-session.ts"), "other session\n", "utf8");
      await runtime.emit("tool_result", {
        type: "tool_result",
        toolCallId: "failed-write",
        toolName: "write",
        input: { path: "failed.ts" },
        content: [{ type: "text", text: "failed" }],
        details: undefined,
        isError: true,
      });
      await runtime.emit("tool_result", {
        type: "tool_result",
        toolCallId: "shell-change",
        toolName: "bash",
        input: { command: "generate files" },
        content: [{ type: "text", text: "done" }],
        details: undefined,
        isError: false,
      });
      await runtime.shutdown();

      expect(await test.client.listPendingCaptures(test.repository, sessionID)).toEqual([]);
      for (const path of [
        "human.ts",
        "shell.ts",
        "generated.ts",
        "formatted.ts",
        "other-session.ts",
        "failed.ts",
      ]) {
        expect(await episodeForPath(test, path)).toBeUndefined();
      }
    } finally {
      await test.cleanup();
    }
  }, 20_000);
});
