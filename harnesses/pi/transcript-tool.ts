import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

import { renderTranscriptView } from "./render.ts";
import { AdapterError, type TranscriptView } from "./rpc.ts";

interface TranscriptClient {
  getTranscript(
    repositoryRoot: string,
    transcriptID: string,
    view: "compact" | "raw",
    offset?: number,
    signal?: AbortSignal,
  ): Promise<TranscriptView>;
}

export function registerTranscriptTool(
  pi: ExtensionAPI,
  client: TranscriptClient,
  isEnabled: () => boolean,
): void {
  pi.registerTool({
    name: "madeleine_transcript",
    label: "Madeleine Transcript",
    description: "Retrieve compact or paged raw evidence for one Madeleine Episode Transcript.",
    parameters: Type.Object(
      {
        transcript_id: Type.String({ description: "Transcript ID shown by madeleine_episode" }),
        view: Type.Optional(Type.Union([
          Type.Literal("compact"),
          Type.Literal("raw"),
        ], { default: "compact" })),
        offset: Type.Optional(Type.Integer({ minimum: 0 })),
      },
      { additionalProperties: false },
    ),
    async execute(_toolCallID, params, signal, _onUpdate, ctx) {
      if (!isEnabled()) throw new Error("Madeleine is unavailable for this session.");
      const view = params.view ?? "compact";
      if (view === "compact" && params.offset !== undefined) {
        throw new Error("A raw offset can only be used with the raw Transcript view.");
      }
      try {
        const transcript = await client.getTranscript(
          ctx.cwd,
          params.transcript_id,
          view,
          params.offset,
          signal,
        );
        return {
          content: [{ type: "text" as const, text: renderTranscriptView(transcript) }],
          details: {
            transcript_id: transcript.transcript_id,
            view: transcript.view,
            next_offset: transcript.next_offset,
          },
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
    return "Transcript is not available in the current repository.";
  }
  return "Madeleine could not retrieve that Transcript.";
}
