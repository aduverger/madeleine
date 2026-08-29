import { afterEach, describe, expect, it } from "vitest";

import { AdapterError, RPCClient } from "./rpc.ts";
import { createFakeMadeleine, type FakeMadeleine, healthyDoctorResult } from "./test-helper.ts";

const fakes: FakeMadeleine[] = [];

afterEach(async () => {
  await Promise.all(fakes.splice(0).map((fake) => fake.cleanup()));
});

async function fakeClient(spec: Parameters<typeof createFakeMadeleine>[0], options = {}) {
  const fake = await createFakeMadeleine(spec);
  fakes.push(fake);
  return {
    fake,
    client: new RPCClient({ env: { MADELEINE_BIN: fake.binary }, ...options }),
  };
}

function captureResult() {
  return {
    id: "capture-1",
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: "session-1" },
    worktree_root: "/repo",
    status: "open",
    transcript_ref: "/sessions/one.jsonl",
    start_cursor: "entry-1",
    started_at: "2026-01-01T00:00:00Z",
    last_seen_at: "2026-01-01T00:00:00Z",
  };
}

describe("RPCClient", () => {
  it("uses a non-empty binary override and otherwise falls back to PATH", () => {
    expect(new RPCClient({ env: { MADELEINE_BIN: " /tmp/bin with spaces " } }).binary).toBe(
      "/tmp/bin with spaces",
    );
    expect(new RPCClient({ env: { MADELEINE_BIN: "   " } }).binary).toBe("madeleine");
  });

  it("runs doctor and accepts its check-failure exit status", async () => {
    const { client } = await fakeClient({
      doctor: { result: healthyDoctorResult(), exitCode: 1 },
    });

    const checks = await client.doctor("/repo");
    expect(checks).toHaveLength(6);
  });

  it("passes paths with spaces and Unicode through stdin JSON", async () => {
    const { client, fake } = await fakeClient({
      methods: { "context.for_paths": { contextEpisodes: [] } },
    });
    const path = "src/été file.ts";

    await expect(client.contextForPath("/repo with spaces", path)).resolves.toEqual([
      { path, episodes: [] },
    ]);
    const requests = await fake.requests();
    expect(requests[0]).toMatchObject({
      args: ["rpc", "context.for_paths"],
      request: {
        protocol_version: 1,
        params: { repository_root: "/repo with spaces", paths: [path] },
      },
    });
  });

  it("validates context and Episode result shapes", async () => {
    const detail = {
      episode_id: "episode-1",
      harness: "pi",
      paths: ["src/a.ts"],
      l1: "short",
      l2: "long",
      transcript_ref: "session.jsonl",
      started_at: "2026-01-01T00:00:00Z",
      ended_at: "2026-01-01T00:01:00Z",
    };
    const { client } = await fakeClient({
      methods: {
        "context.for_paths": {
          result: [
            {
              path: "src/a.ts",
              episodes: [
                {
                  episode_id: "episode-1",
                  ended_at: "2026-01-01T00:01:00Z",
                  harness: "pi",
                  l1: "short",
                },
              ],
            },
          ],
        },
        "episode.get": { result: detail },
      },
    });

    await expect(client.contextForPath("/repo", "src/a.ts")).resolves.toHaveLength(1);
    await expect(client.getEpisode("/repo", "episode-1")).resolves.toEqual(detail);
  });

  it("validates Capture lifecycle results and request shapes", async () => {
    const capture = captureResult();
    const { client, fake } = await fakeClient({
      methods: {
        "capture.start": { result: capture },
        "capture.get": { result: capture },
        "capture.list_pending": { result: [capture] },
        "capture.record_write": { result: {} },
        "capture.seal": {
          result: {
            capture_id: "capture-1",
            status: "pending_summary",
            empty: false,
            paths: ["src/a.ts"],
          },
        },
        "capture.abandon": { result: {} },
      },
    });

    await expect(
      client.startCapture("/repo", "session-1", "/sessions/one.jsonl", "entry-1"),
    ).resolves.toEqual(capture);
    await expect(client.getCapture("capture-1")).resolves.toEqual(capture);
    await expect(client.listPendingCaptures("/repo", "session-1")).resolves.toEqual([capture]);
    await expect(client.recordWrite("capture-1", "/repo/src/a.ts")).resolves.toBeUndefined();
    await expect(client.sealCapture("capture-1", "entry-2")).resolves.toMatchObject({
      status: "pending_summary",
      paths: ["src/a.ts"],
    });
    await expect(client.abandonCapture("capture-1")).resolves.toBeUndefined();

    const requests = await fake.requests();
    expect(requests[0]).toMatchObject({
      args: ["rpc", "capture.start"],
      request: {
        params: {
          conversation_key: { harness: "pi", external_id: "session-1" },
          transcript_ref: "/sessions/one.jsonl",
          start_cursor: "entry-1",
        },
      },
    });
  });

  it("applies the standard timeout to lifecycle calls", async () => {
    const { client } = await fakeClient(
      { methods: { "capture.start": { result: captureResult(), delayMs: 200 } } },
      { timeoutMs: 20 },
    );

    await expect(
      client.startCapture("/repo", "session-1", "/sessions/one.jsonl", "entry-1"),
    ).rejects.toMatchObject({ kind: "timeout" });
  });

  it.each([
    ["bad JSON", { rawStdout: "not json" }, "invalid_response"],
    ["wrong protocol", { protocolVersion: 2, result: [] }, "invalid_response"],
    ["nonzero child", { result: [], exitCode: 1 }, "process_failure"],
  ] as const)("rejects %s", async (_name, action, kind) => {
    const { client } = await fakeClient({ methods: { "context.for_paths": action } });
    await expect(client.contextForPath("/repo", "a.ts")).rejects.toMatchObject({ kind });
  });

  it("preserves structured RPC error codes", async () => {
    const { client } = await fakeClient({
      methods: {
        "episode.get": {
          error: { code: "not_found", message: "requested object was not found" },
        },
      },
    });

    await expect(client.getEpisode("/repo", "missing")).rejects.toMatchObject({
      kind: "remote",
      code: "not_found",
    });
  });

  it("bounds stdout and stderr", async () => {
    const stdout = await fakeClient(
      { methods: { "context.for_paths": { stdoutBytes: 128 } } },
      { maxOutputBytes: 64 },
    );
    await expect(stdout.client.contextForPath("/repo", "a.ts")).rejects.toMatchObject({
      kind: "oversized_output",
    });

    const stderr = await fakeClient(
      { methods: { "context.for_paths": { result: [], stderrBytes: 128 } } },
      { maxOutputBytes: 64 },
    );
    await expect(stderr.client.contextForPath("/repo", "a.ts")).rejects.toMatchObject({
      kind: "oversized_output",
    });
  });

  it("kills timed-out and cancelled children", async () => {
    const timedOut = await fakeClient(
      { methods: { "context.for_paths": { result: [], delayMs: 200 } } },
      { timeoutMs: 20 },
    );
    await expect(timedOut.client.contextForPath("/repo", "a.ts")).rejects.toMatchObject({
      kind: "timeout",
    });

    const cancelled = await fakeClient({
      methods: { "context.for_paths": { result: [], delayMs: 200 } },
    });
    const controller = new AbortController();
    const request = cancelled.client.contextForPath("/repo", "a.ts", controller.signal);
    controller.abort();
    await expect(request).rejects.toMatchObject({ kind: "cancelled" });
  });

  it("reports a missing binary as unavailable", async () => {
    const client = new RPCClient({ env: { MADELEINE_BIN: "/missing/madeleine" } });
    await expect(client.contextForPath("/repo", "a.ts")).rejects.toEqual(
      expect.objectContaining<Partial<AdapterError>>({ kind: "unavailable" }),
    );
  });
});
