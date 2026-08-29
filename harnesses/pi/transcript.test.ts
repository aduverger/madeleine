import type { SessionEntry } from "@earendil-works/pi-coding-agent";
import { describe, expect, it } from "vitest";

import {
  maxProjectionCharacters,
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
    toolCallId: `call-${id}`,
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

  it("omits reads and binary content while retaining bounded mutation details", () => {
    const entries = [
      custom("start", null),
      user("goal", "start", "change a file"),
      assistant("calls", "goal", [
        { type: "toolCall", id: "read", name: "read", arguments: { path: "secret.txt" } },
        { type: "toolCall", id: "write", name: "write", arguments: { path: "src/a.ts", content: "new" } },
      ]),
      toolResult("read-result", "calls", "read", "large secret output"),
      toolResult("write-result", "read-result", "write", "wrote file"),
      entry("image", "write-result", {
        role: "user",
        content: [{ type: "image", data: "base64-payload", mimeType: "image/png" }],
        timestamp: 0,
      }),
    ];

    const projection = projectCaptureTranscript(entries, "start", "image", ["src/a.ts"]);
    expect(projection).toContain("[Mutation write]");
    expect(projection).toContain("[Mutation result write: success]");
    expect(projection).not.toContain("secret.txt");
    expect(projection).not.toContain("large secret output");
    expect(projection).not.toContain("base64-payload");
  });

  it("recursively removes complete Madeleine context blocks", () => {
    const injected = "before <madeleine-context>x <madeleine-context>nested</madeleine-context> y</madeleine-context> after";
    expect(stripMadeleineContext(injected)).toBe("before  after");
  });

  it("bounds total input while preserving the first goal and newest entries", () => {
    const entries: SessionEntry[] = [custom("start", null), user("goal", "start", "first goal")];
    let parent = "goal";
    for (let index = 0; index < 12; index++) {
      const id = `summary-${index}`;
      entries.push({
        type: "branch_summary",
        id,
        parentId: parent,
        timestamp: "2026-01-01T00:00:00Z",
        fromId: parent,
        summary: `${index}:${"x".repeat(5000)}`,
      });
      parent = id;
    }
    entries.push(toolResult("latest", parent, "write", `latest:${"y".repeat(5000)}`));

    const projection = projectCaptureTranscript(entries, "start", "latest", []);
    expect(projection.length).toBeLessThanOrEqual(maxProjectionCharacters);
    expect(projection).toContain("first goal");
    expect(projection).toContain("[Branch summary]\n11:");
    expect(projection).toContain("[Mutation result write: success]\nlatest:");
    expect(projection).not.toContain("[Branch summary]\n0:");
  });

  it("rejects missing or unrelated boundaries instead of summarizing the full session", () => {
    const entries = [custom("start", null), custom("other", null)];
    expect(() => projectCaptureTranscript(entries, "missing", "other", [])).toThrow("start cursor");
    expect(() => projectCaptureTranscript(entries, "start", "other", [])).toThrow("not on one Pi branch");
  });
});
