import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { describe, expect, it, vi } from "vitest";

import { registerEpisodeTool } from "./episode-tool.ts";
import { AdapterError, type EpisodeDetail } from "./rpc.ts";

type EpisodeGetter = (
  repositoryRoot: string,
  episodeID: string,
  signal?: AbortSignal,
) => Promise<EpisodeDetail>;

function register(client: { getEpisode: EpisodeGetter }, enabled = true) {
  let tool: any;
  const pi = {
    registerTool(definition: unknown) {
      tool = definition;
    },
  } as unknown as ExtensionAPI;
  registerEpisodeTool(pi, client, () => enabled);
  return tool;
}

function context() {
  return { cwd: "/current/repository" } as unknown as ExtensionContext;
}

const detail = {
  episode_id: "episode-1",
  harness: "pi",
  paths: ["src/a.ts", "src/b.ts"],
  l1: "Short summary",
  l2: "Longer historical brief",
  transcript_id: "transcript-1",
  started_at: "2026-01-01T00:00:00Z",
  ended_at: "2026-01-01T00:10:00Z",
};

describe("madeleine_episode", () => {
  it("has a strict one-field schema and renders L2 with its Transcript ID", async () => {
    const getEpisode = vi.fn(async () => detail);
    const tool = register({ getEpisode });

    expect(tool.parameters.required).toEqual(["episode_id"]);
    expect(tool.parameters.additionalProperties).toBe(false);
    expect(Object.keys(tool.parameters.properties)).toEqual(["episode_id"]);

    const result = await tool.execute("call-1", { episode_id: "episode-1" }, undefined, undefined, context());
    expect(getEpisode).toHaveBeenCalledWith("/current/repository", "episode-1", undefined);
    expect(result.content[0].text).toContain("L2:\nLonger historical brief");
    expect(result.content[0].text).toContain("Transcript ID: transcript-1");
    expect(result.content[0].text).not.toContain("transcript body");
  });

  it.each(["not_found", "outside_repository"])(
    "returns a concise repository-safe error for %s",
    async (code) => {
      const getEpisode = vi.fn(async (): Promise<EpisodeDetail> => {
        throw new AdapterError("remote", "unsafe detail", code);
      });
      const tool = register({ getEpisode });

      await expect(
        tool.execute("call-1", { episode_id: "episode-1" }, undefined, undefined, context()),
      ).rejects.toThrow("Episode is not available in the current repository.");
    },
  );

  it("reports a disabled runtime without calling the binary", async () => {
    const getEpisode = vi.fn(async () => detail);
    const tool = register({ getEpisode }, false);

    await expect(
      tool.execute("call-1", { episode_id: "episode-1" }, undefined, undefined, context()),
    ).rejects.toThrow("Madeleine is unavailable for this session.");
    expect(getEpisode).not.toHaveBeenCalled();
  });
});
