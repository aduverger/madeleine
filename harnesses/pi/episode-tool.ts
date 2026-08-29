import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

import { AdapterError, type EpisodeDetail } from "./rpc.ts";
import { renderEpisode } from "./render.ts";

interface EpisodeClient {
  getEpisode(repositoryRoot: string, episodeID: string, signal?: AbortSignal): Promise<EpisodeDetail>;
}

export function registerEpisodeTool(
  pi: ExtensionAPI,
  client: EpisodeClient,
  isEnabled: () => boolean,
): void {
  pi.registerTool({
    name: "madeleine_episode",
    label: "Madeleine Episode",
    description: "Retrieve the longer historical brief for one Madeleine Episode in the current repository.",
    parameters: Type.Object(
      {
        episode_id: Type.String({ description: "Episode ID shown in Madeleine historical context" }),
      },
      { additionalProperties: false },
    ),
    async execute(_toolCallID, params, signal, _onUpdate, ctx) {
      if (!isEnabled()) {
        throw new Error("Madeleine is unavailable for this session.");
      }
      try {
        const episode = await client.getEpisode(ctx.cwd, params.episode_id, signal);
        return {
          content: [{ type: "text" as const, text: renderEpisode(episode) }],
          details: { episode_id: episode.episode_id },
        };
      } catch (error) {
        throw new Error(toolErrorMessage(error));
      }
    },
  });
}

function toolErrorMessage(error: unknown): string {
  if (
    error instanceof AdapterError &&
    ["not_found", "not_git_repository", "outside_repository"].includes(error.code ?? "")
  ) {
    return "Episode is not available in the current repository.";
  }
  return "Madeleine could not retrieve that Episode.";
}
