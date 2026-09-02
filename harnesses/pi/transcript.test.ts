import type { SessionEntry } from "@earendil-works/pi-coding-agent";
import { describe, expect, it } from "vitest";

import {
  maxMutationErrorCharacters,
  extractCaptureTranscript,
  projectCaptureTranscript,
  stripMadeleineContext,
} from "./transcript.ts";

function entry(id: string, parentId: string | null, message: unknown): SessionEntry {
  return {
    type: "message",
    id,
    parentId,
    timestamp: "2026-01-01T00:00:00Z",
    message,
  } as SessionEntry;
}

function custom(id: string, parentId: string | null): SessionEntry {
  return {
    type: "custom",
    id,
    parentId,
    timestamp: "2026-01-01T00:00:00Z",
    customType: "boundary",
    data: {},
  };
}

function user(id: string, parentId: string, text: string): SessionEntry {
  return entry(id, parentId, { role: "user", content: text, timestamp: 0 });
}

function assistant(id: string, parentId: string, content: unknown[]): SessionEntry {
  return entry(id, parentId, {
    role: "assistant",
    content,
    api: "test",
    provider: "test",
    model: "test",
    usage: {},
    stopReason: "stop",
    timestamp: 0,
  });
}

function toolResult(
  id: string,
  parentId: string,
  toolName: string,
  text: string,
  isError = false,
): SessionEntry {
  return entry(id, parentId, {
    role: "toolResult",
    toolCallId: toolName,
    toolName,
    content: [{ type: "text", text }],
    isError,
    timestamp: 0,
  });
}

describe("projectCaptureTranscript", () => {
  it("projects only entries after the start cursor through the end cursor", () => {
    const entries = [
      user("before", "root", "old conversation"),
      custom("start", "before"),
      user("goal", "start", "implement summaries"),
      assistant("answer", "goal", [{ type: "text", text: "I will implement them." }]),
      custom("end", "answer"),
    ];

    const projection = projectCaptureTranscript(entries, "start", "end", ["src/a.ts"]);
    expect(projection).toContain("implement summaries");
    expect(projection).toContain("I will implement them");
    expect(projection).not.toContain("old conversation");
    expect(projection).toContain("- src/a.ts");
  });

  it("reconstructs the selected fork from parent IDs", () => {
    const entries = [
      custom("start", null),
      user("goal", "start", "choose a branch"),
      assistant("abandoned", "goal", [{ type: "text", text: "abandoned answer" }]),
      assistant("active", "goal", [{ type: "text", text: "active answer" }]),
      custom("end", "active"),
    ];

    const projection = projectCaptureTranscript(entries, "start", "end", []);
    expect(projection).toContain("active answer");
    expect(projection).not.toContain("abandoned answer");
  });

  it("includes a branch summary without traversing the abandoned branch", () => {
    const entries: SessionEntry[] = [
      custom("start", null),
      user("goal", "start", "try two approaches"),
      assistant("abandoned", "goal", [{ type: "text", text: "raw abandoned work" }]),
      {
        type: "branch_summary",
        id: "summary",
        parentId: "goal",
        timestamp: "2026-01-01T00:00:00Z",
        fromId: "abandoned",
        summary: "The abandoned branch established the parser invariant.",
      },
      assistant("tail", "summary", [{ type: "text", text: "continued on the active branch" }]),
    ];

    const projection = projectCaptureTranscript(entries, "start", "tail", []);
    expect(projection).toContain("[Branch summary]\nThe abandoned branch established the parser invariant.");
    expect(projection).toContain("continued on the active branch");
    expect(projection).not.toContain("raw abandoned work");
  });

  it("includes a branch summary used as the root Capture boundary", () => {
    const entries: SessionEntry[] = [{
      type: "branch_summary",
      id: "summary",
      parentId: null,
      timestamp: "2026-01-01T00:00:00Z",
      fromId: "old-leaf",
      summary: "Context carried from the previous branch.",
    }];

    expect(extractCaptureTranscript(entries, "summary", "summary").entries).toEqual([{
      kind: "branch_summary",
      text: "Context carried from the previous branch.",
    }]);
  });

  it("uses raw bounded messages and omits a compaction summary", () => {
    const entries: SessionEntry[] = [
      custom("start", null),
      user("goal", "start", "large refactor"),
      assistant("before-compact", "goal", [{ type: "text", text: "Changed the parser." }]),
      {
        type: "compaction",
        id: "compact",
        parentId: "before-compact",
        timestamp: "2026-01-01T00:00:00Z",
        summary: "Compacted summary that may include pre-Capture context.",
        firstKeptEntryId: "goal",
        tokensBefore: 1000,
      },
      assistant("tail", "compact", [{ type: "text", text: "Tests now pass." }]),
    ];

    const projection = projectCaptureTranscript(entries, "start", "tail", []);
    expect(projection).toContain("Changed the parser.");
    expect(projection).toContain("Tests now pass.");
    expect(projection).not.toContain("Compacted summary that may include pre-Capture context.");
  });

  it("keeps mutation metadata without read, file-content, result, or binary bulk", () => {
    const entries = [
      custom("start", null),
      user("goal", "start", "change a file"),
      assistant("calls", "goal", [
        { type: "toolCall", id: "read", name: "read", arguments: { path: "secret.txt" } },
        {
          type: "toolCall",
          id: "write",
          name: "write",
          arguments: { path: "src/a.ts", content: "distinctive-write-payload" },
        },
        {
          type: "toolCall",
          id: "edit",
          name: "edit",
          arguments: {
            path: "src/b.ts",
            oldText: "distinctive-old-text",
            newText: "distinctive-new-text",
          },
        },
      ]),
      toolResult("read-result", "calls", "read", "large secret output"),
      toolResult("write-result", "read-result", "write", "successful write result prose"),
      toolResult("edit-result", "write-result", "edit", "successful edit result prose"),
      entry("image", "edit-result", {
        role: "user",
        content: [{ type: "image", data: "base64-payload", mimeType: "image/png" }],
        timestamp: 0,
      }),
    ];

    const projection = projectCaptureTranscript(entries, "start", "image", ["src/a.ts"]);
    expect(projection).toContain("[Mutation write: success]\nPath: src/a.ts");
    expect(projection).toContain("[Mutation edit: success]\nPath: src/b.ts");
    expect(projection).not.toContain("secret.txt");
    expect(projection).not.toContain("distinctive-write-payload");
    expect(projection).not.toContain("distinctive-old-text");
    expect(projection).not.toContain("distinctive-new-text");
    expect(projection).not.toContain("large secret output");
    expect(projection).not.toContain("successful write result prose");
    expect(projection).not.toContain("base64-payload");
  });

  it("retains a bounded error for a failed mutation", () => {
    const error = `permission denied ${"x".repeat(maxMutationErrorCharacters)}`;
    const entries = [
      custom("start", null),
      assistant("call", "start", [
        { type: "toolCall", id: "edit", name: "edit", arguments: { path: "src/a.ts" } },
      ]),
      toolResult("failure", "call", "edit", error, true),
    ];

    const projection = projectCaptureTranscript(entries, "start", "failure", []);
    expect(projection).toContain("[Mutation edit: failure]\nPath: src/a.ts\npermission denied");
    expect(projection).toContain("… [truncated]");
    expect(projection).not.toContain(error);
  });

  it("returns versioned structured entries for sealing and retry", () => {
    const entries = [
      custom("start", null),
      user("goal", "start", "change a file"),
      assistant("call", "goal", [
        { type: "toolCall", id: "write", name: "write", arguments: { path: "src/a.ts", content: "secret" } },
      ]),
      toolResult("result", "call", "write", "written"),
    ];

    expect(extractCaptureTranscript(entries, "start", "result")).toEqual({
      format_version: 1,
      entries: [
        { kind: "user", text: "change a file" },
        { kind: "mutation", operation: "write", path: "src/a.ts", status: "success" },
      ],
    });
  });

  it("recursively removes complete Madeleine context blocks", () => {
    const injected = "before <madeleine-context>x <madeleine-context>nested</madeleine-context> y</madeleine-context> after";
    expect(stripMadeleineContext(injected)).toBe("before  after");
  });

  it("does not discard long in-bound conversational evidence", () => {
    const early = `early:${"x".repeat(50_000)}`;
    const late = `late:${"y".repeat(50_000)}`;
    const entries = [
      custom("start", null),
      user("goal", "start", early),
      assistant("answer", "goal", [{ type: "text", text: late }]),
    ];

    const projection = projectCaptureTranscript(entries, "start", "answer", []);
    expect(projection).toContain(early);
    expect(projection).toContain(late);
  });

  it("rejects missing or unrelated boundaries instead of summarizing the full session", () => {
    const entries = [custom("start", null), custom("other", null)];
    expect(() => projectCaptureTranscript(entries, "missing", "other", [])).toThrow("start cursor");
    expect(() => projectCaptureTranscript(entries, "start", "other", [])).toThrow("not on one Pi branch");
  });
});
