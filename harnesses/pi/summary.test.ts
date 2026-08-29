import {
  estimateTokens,
  type ExtensionContext,
  type SessionEntry,
} from "@earendil-works/pi-coding-agent";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Capture, Episode, FinalizationDraft } from "./rpc.ts";
import {
  EpisodeFinalizer,
  generateSummary,
  maxL1Characters,
  summaryInputTokenLimit,
  summaryMaxTokens,
  summaryTimeoutMs,
  validateSummary,
} from "./summary.ts";

function boundary(id: string, parentId: string | null): SessionEntry {
  return {
    type: "custom",
    id,
    parentId,
    timestamp: "2026-01-01T00:00:00Z",
    customType: "boundary",
    data: {},
  };
}

const transcriptEntries: SessionEntry[] = [
  boundary("start", null),
  {
    type: "message",
    id: "goal",
    parentId: "start",
    timestamp: "2026-01-01T00:00:01Z",
    message: { role: "user", content: "implement summaries", timestamp: 0 },
  } as SessionEntry,
  boundary("end", "goal"),
];

function modelResponse(text: string) {
  return {
    role: "assistant",
    content: text ? [{ type: "text", text }] : [],
    stopReason: "stop",
    usage: {},
  };
}

function context(options: {
  response?: string;
  authenticated?: boolean;
  complete?: (model: unknown, context: unknown, options: any) => Promise<any>;
  model?: unknown;
} = {}) {
  const complete = vi.fn(options.complete ?? (async () => modelResponse(
    options.response ?? JSON.stringify({ l1: "Short summary", l2: "Detailed brief" }),
  )));
  const ctx = {
    model: options.model === null
      ? undefined
      : (options.model ?? { id: "active-model", contextWindow: 128_000 }),
    modelRegistry: {
      hasConfiguredAuth: () => options.authenticated ?? true,
      complete,
    },
    sessionManager: { getEntries: () => transcriptEntries },
  } as unknown as ExtensionContext;
  return { ctx, complete };
}

function capture(overrides: Partial<Capture> = {}): Capture {
  return {
    id: "capture-1",
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: "session-1" },
    worktree_root: "/repo",
    status: "pending_summary",
    start_cursor: "start",
    end_cursor: "end",
    started_at: "2026-01-01T00:00:00Z",
    ended_at: "2026-01-01T00:01:00Z",
    last_seen_at: "2026-01-01T00:01:00Z",
    ...overrides,
  };
}

function episode(): Episode {
  return {
    id: "episode-1",
    capture_id: "capture-1",
    repository_id: "repository-1",
    conversation_id: "conversation-1",
    conversation_key: { harness: "pi", external_id: "session-1" },
    harness: "pi",
    paths: ["src/a.ts"],
    l1: "Short summary",
    l2: "Detailed brief",
    start_cursor: "start",
    end_cursor: "end",
    started_at: "2026-01-01T00:00:00Z",
    ended_at: "2026-01-01T00:01:00Z",
    created_at: "2026-01-01T00:01:01Z",
  };
}

const draft: FinalizationDraft = {
  capture_id: "capture-1",
  status: "pending_summary",
  empty: false,
  paths: ["src/a.ts"],
};

afterEach(() => {
  vi.useRealTimers();
});

describe("validateSummary", () => {
  it("accepts and trims the exact summary object", () => {
    expect(validateSummary('{"l1":" Short ","l2":" Detailed "}')).toEqual({
      l1: "Short",
      l2: "Detailed",
    });
  });

  it.each([
    ["surrounding prose", 'Here: {"l1":"a","l2":"b"}'],
    ["code fence", '```json\n{"l1":"a","l2":"b"}\n```'],
    ["extra key", '{"l1":"a","l2":"b","extra":true}'],
    ["missing key", '{"l1":"a"}'],
    ["empty value", '{"l1":" ","l2":"b"}'],
    ["wrong type", '{"l1":1,"l2":"b"}'],
    ["empty response", ""],
  ])("rejects %s", (_name, response) => {
    expect(() => validateSummary(response)).toThrow();
  });

  it("counts the L1 limit in Unicode characters", () => {
    const valid = JSON.stringify({ l1: "😀".repeat(maxL1Characters), l2: "brief" });
    const invalid = JSON.stringify({ l1: "😀".repeat(maxL1Characters + 1), l2: "brief" });
    expect(validateSummary(valid).l1).toHaveLength(maxL1Characters * 2);
    expect(() => validateSummary(invalid)).toThrow("Unicode characters");
  });
});

describe("generateSummary", () => {
  it("uses the active authenticated model without mutating the Pi session", async () => {
    const entriesBefore = [...transcriptEntries];
    const { ctx, complete } = context();

    await expect(generateSummary("untrusted transcript", ctx)).resolves.toEqual({
      l1: "Short summary",
      l2: "Detailed brief",
    });
    expect(complete).toHaveBeenCalledWith(
      ctx.model,
      { messages: [expect.objectContaining({ role: "user" })] },
      expect.objectContaining({
        cacheRetention: "none",
        maxTokens: summaryMaxTokens,
        sessionId: expect.stringMatching(/^[0-9a-f-]+$/),
        signal: expect.any(AbortSignal),
      }),
    );
    const modelContext = complete.mock.calls[0]![1] as any;
    const prompt = modelContext.messages[0].content[0].text;
    expect(prompt).toContain("untrusted Capture data");
    expect(prompt).toContain("untrusted transcript");
    expect(transcriptEntries).toEqual(entriesBefore);
  });

  it("summarizes oversized semantic evidence in model-sized chunks before final synthesis", async () => {
    let segment = 0;
    const complete = vi.fn(async (_model: unknown, modelContext: any) => {
      const prompt = modelContext.messages[0].content[0].text as string;
      if (prompt.startsWith("Create a Madeleine Episode summary")) {
        return modelResponse('{"l1":"Chunked summary","l2":"Combined segment evidence"}');
      }
      segment++;
      return modelResponse(`segment summary ${segment}`);
    });
    const model = { id: "small-model", contextWindow: 4_000 };
    const { ctx } = context({ complete, model });

    await expect(generateSummary(`start\n\n${"evidence ".repeat(4_000)}\n\nend`, ctx)).resolves.toEqual({
      l1: "Chunked summary",
      l2: "Combined segment evidence",
    });

    expect(segment).toBeGreaterThan(1);
    expect(complete).toHaveBeenCalledTimes(segment + 1);
    const inputLimit = summaryInputTokenLimit(model as any);
    for (const call of complete.mock.calls) {
      const prompt = call[1].messages[0].content[0].text;
      expect(estimateTokens({ role: "user", content: prompt, timestamp: 0 })).toBeLessThanOrEqual(inputLimit);
    }
    const finalPrompt = complete.mock.calls.at(-1)![1].messages[0].content[0].text;
    expect(finalPrompt).toContain("segment summary 1");
    expect(finalPrompt).not.toContain("evidence evidence evidence");
  });

  it("compacts segment summaries again when one hierarchy level does not fit", async () => {
    let secondLevelCalls = 0;
    const complete = vi.fn(async (_model: unknown, modelContext: any) => {
      const prompt = modelContext.messages[0].content[0].text as string;
      if (prompt.startsWith("Create a Madeleine Episode summary")) {
        return modelResponse('{"l1":"Recursive summary","l2":"All levels combined"}');
      }
      if (prompt.includes("first-level segment")) {
        secondLevelCalls++;
        return modelResponse("second-level segment");
      }
      return modelResponse(`first-level segment ${"x".repeat(1_500)}`);
    });
    const { ctx } = context({ complete, model: { id: "small-model", contextWindow: 4_000 } });

    await expect(generateSummary("original evidence ".repeat(4_000), ctx)).resolves.toEqual({
      l1: "Recursive summary",
      l2: "All levels combined",
    });

    expect(secondLevelCalls).toBeGreaterThan(0);
    const finalPrompt = complete.mock.calls.at(-1)![1].messages[0].content[0].text;
    expect(finalPrompt).toContain("second-level segment");
  });

  it.each([
    ["missing model", { model: null }],
    ["missing authentication", { authenticated: false }],
  ])("rejects %s before a model call", async (_name, options) => {
    const { ctx, complete } = context(options);
    await expect(generateSummary("projection", ctx)).rejects.toThrow("unavailable or unauthenticated");
    expect(complete).not.toHaveBeenCalled();
  });

  it("preserves model rejection", async () => {
    const { ctx } = context({ complete: async () => { throw new Error("provider rejected"); } });
    await expect(generateSummary("projection", ctx)).rejects.toThrow("provider rejected");
  });

  it("rejects a truncated model response", async () => {
    const { ctx } = context({
      complete: async () => ({ ...modelResponse("partial"), stopReason: "length" }),
    });
    await expect(generateSummary("projection", ctx)).rejects.toThrow("stopped with length");
  });

  it("aborts the model after the summary timeout", async () => {
    vi.useFakeTimers();
    const { ctx } = context({
      complete: async (_model, _context, options) => new Promise((_resolve, reject) => {
        options.signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
      }),
    });
    const result = generateSummary("projection", ctx);
    const rejection = expect(result).rejects.toThrow("aborted");
    await vi.advanceTimersByTimeAsync(summaryTimeoutMs);
    await rejection;
  });

  it("combines lifecycle cancellation with the timeout", async () => {
    const controller = new AbortController();
    const { ctx } = context({
      complete: async (_model, _context, options) => new Promise((_resolve, reject) => {
        options.signal.addEventListener("abort", () => reject(new Error("cancelled")), { once: true });
      }),
    });
    const result = generateSummary("projection", ctx, controller.signal);
    controller.abort();
    await expect(result).rejects.toThrow("cancelled");
  });
});

describe("EpisodeFinalizer", () => {
  it("projects, summarizes, and publishes authoritative paths", async () => {
    const getCapture = vi.fn(async () => capture());
    const publishEpisode = vi.fn(async () => episode());
    const finalizer = new EpisodeFinalizer({ getCapture, publishEpisode });
    const { ctx, complete } = context();

    await expect(finalizer.finalize(draft, ctx)).resolves.toEqual({
      captureID: "capture-1",
      status: "published",
      episodeID: "episode-1",
    });
    expect(getCapture).toHaveBeenCalledWith("capture-1", undefined);
    expect(publishEpisode).toHaveBeenCalledWith(
      "capture-1",
      "Short summary",
      "Detailed brief",
      undefined,
    );
    const modelContext = complete.mock.calls[0]![1] as any;
    expect(modelContext.messages[0].content[0].text).toContain("- src/a.ts");
  });

  it("returns immediately for an empty Capture without model or publish calls", async () => {
    const getCapture = vi.fn(async () => capture());
    const publishEpisode = vi.fn(async () => episode());
    const finalizer = new EpisodeFinalizer({ getCapture, publishEpisode });
    const { ctx, complete } = context();

    await expect(finalizer.finalize({
      capture_id: "capture-empty",
      status: "abandoned",
      empty: true,
      paths: [],
    }, ctx)).resolves.toEqual({ captureID: "capture-empty", status: "abandoned" });
    expect(getCapture).not.toHaveBeenCalled();
    expect(complete).not.toHaveBeenCalled();
    expect(publishEpisode).not.toHaveBeenCalled();
  });

  it.each(["model", "publish"])("leaves finalization failed when %s fails", async (failure) => {
    const getCapture = vi.fn(async () => capture());
    const publishEpisode = vi.fn(async () => {
      if (failure === "publish") throw new Error("publish failed");
      return episode();
    });
    const finalizer = new EpisodeFinalizer({ getCapture, publishEpisode });
    const { ctx } = context({
      complete: async () => {
        if (failure === "model") throw new Error("model failed");
        return modelResponse('{"l1":"Short summary","l2":"Detailed brief"}');
      },
    });

    await expect(finalizer.finalize(draft, ctx)).rejects.toThrow(`${failure} failed`);
    expect(draft.status).toBe("pending_summary");
  });
});
