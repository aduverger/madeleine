import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { describe, expect, it, vi } from "vitest";

import { AdapterError, type TranscriptView } from "./rpc.ts";
import { registerTranscriptTool } from "./transcript-tool.ts";

type TranscriptGetter = (
  repositoryRoot: string,
  transcriptID: string,
  view: "compact" | "raw",
  offset?: number,
  signal?: AbortSignal,
) => Promise<TranscriptView>;

function register(client: { getTranscript: TranscriptGetter }, enabled = true) {
  let tool: any;
  const pi = {
    registerTool(definition: unknown) {
      tool = definition;
    },
  } as unknown as ExtensionAPI;
  registerTranscriptTool(pi, client, () => enabled);
  return tool;
}

const context = { cwd: "/current/repository" } as unknown as ExtensionContext;

describe("madeleine_transcript", () => {
  it("defaults to compact and renders untrusted evidence", async () => {
    const getTranscript = vi.fn(async (): Promise<TranscriptView> => ({
      transcript_id: "transcript-1",
      view: "compact",
      compact: "Goal and implementation evidence",
    }));
    const tool = register({ getTranscript });

    expect(tool.parameters.required).toEqual(["transcript_id"]);
    expect(tool.parameters.additionalProperties).toBe(false);
    const result = await tool.execute(
      "call-1",
      { transcript_id: "transcript-1" },
      undefined,
      undefined,
      context,
    );

    expect(getTranscript).toHaveBeenCalledWith(
      "/current/repository",
      "transcript-1",
      "compact",
      undefined,
      undefined,
    );
    expect(result.content[0].text).toContain("Goal and implementation evidence");
    expect(result.content[0].text).toContain('trust="untrusted-data"');
  });

  it("passes a raw page offset and returns the next offset", async () => {
    const getTranscript = vi.fn(async (): Promise<TranscriptView> => ({
      transcript_id: "transcript-1",
      view: "raw",
      entries: [{ kind: "user", text: "historical request" }],
      next_offset: 100,
    }));
    const tool = register({ getTranscript });

    const result = await tool.execute(
      "call-1",
      { transcript_id: "transcript-1", view: "raw", offset: 50 },
      undefined,
      undefined,
      context,
    );

    expect(getTranscript).toHaveBeenCalledWith(
      "/current/repository",
      "transcript-1",
      "raw",
      50,
      undefined,
    );
    expect(result.details.next_offset).toBe(100);
  });

  it.each(["not_found", "outside_repository"])(
    "returns a repository-safe error for %s",
    async (code) => {
      const getTranscript = vi.fn(async (): Promise<TranscriptView> => {
        throw new AdapterError("remote", "unsafe detail", code);
      });
      const tool = register({ getTranscript });

      await expect(tool.execute(
        "call-1",
        { transcript_id: "transcript-1" },
        undefined,
        undefined,
        context,
      )).rejects.toThrow("Transcript is not available in the current repository.");
    },
  );

  it("rejects compact offsets before calling Madeleine", async () => {
    const getTranscript = vi.fn(async (): Promise<TranscriptView> => ({
      transcript_id: "transcript-1",
      view: "compact",
      compact: "evidence",
    }));
    const tool = register({ getTranscript });

    await expect(tool.execute(
      "call-1",
      { transcript_id: "transcript-1", view: "compact", offset: 1 },
      undefined,
      undefined,
      context,
    )).rejects.toThrow("raw offset");
    expect(getTranscript).not.toHaveBeenCalled();
  });
});
