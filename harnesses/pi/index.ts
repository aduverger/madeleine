import {
  isReadToolResult,
  type ExtensionAPI,
  type ExtensionContext,
  type ToolResultEvent,
} from "@earendil-works/pi-coding-agent";
import { resolve } from "node:path";

import { registerEpisodeTool } from "./episode-tool.ts";
import { type DoctorCheck, type EpisodeDetail, type FileContext, RPCClient } from "./rpc.ts";
import { renderFileContext } from "./render.ts";

interface MadeleineClient {
  doctor(repositoryRoot: string): Promise<DoctorCheck[]>;
  contextForPath(repositoryRoot: string, path: string, signal?: AbortSignal): Promise<FileContext[]>;
  getEpisode(repositoryRoot: string, episodeID: string, signal?: AbortSignal): Promise<EpisodeDetail>;
}

const requiredDoctorChecks = [
  "binary_version",
  "data_directory",
  "application",
  "schema_version",
  "git_executable",
  "repository",
];

export function registerMadeleine(pi: ExtensionAPI, client: MadeleineClient = new RPCClient()): void {
  let enabled = false;
  let detection: Promise<void> | undefined;
  const claimedPaths = new Set<string>();

  const detect = async (ctx: ExtensionContext) => {
    try {
      enabled = doctorPassed(await client.doctor(ctx.cwd));
    } catch {
      enabled = false;
    }
    if (!enabled && ctx.hasUI) {
      ctx.ui.notify("Madeleine is disabled for this session.", "warning");
    }
  };

  pi.on("session_start", async (_event, ctx) => {
    detection ??= detect(ctx);
    await detection;
  });

  pi.on("tool_result", async (event, ctx) => {
    if (!enabled || event.isError || !isReadToolResult(event)) return;
    return enrichRead(event, ctx, client, claimedPaths);
  });

  registerEpisodeTool(pi, client, () => enabled);
}

async function enrichRead(
  event: ToolResultEvent,
  ctx: ExtensionContext,
  client: MadeleineClient,
  claimedPaths: Set<string>,
): Promise<{ content: ToolResultEvent["content"] } | undefined> {
  const path = event.input.path;
  if (typeof path !== "string" || path.length === 0) return;

  const pathKey = resolve(ctx.cwd, path);
  if (claimedPaths.has(pathKey)) return;
  claimedPaths.add(pathKey);

  try {
    const contexts = await client.contextForPath(ctx.cwd, path, ctx.signal);
    const block = contexts.length === 1 ? renderFileContext(contexts[0]) : undefined;
    if (!block) {
      claimedPaths.delete(pathKey);
      return;
    }
    return {
      content: [...event.content, { type: "text", text: block }],
    };
  } catch {
    claimedPaths.delete(pathKey);
    return;
  }
}

function doctorPassed(checks: DoctorCheck[]): boolean {
  const checksByName = new Map(checks.map((check) => [check.name, check.ok]));
  return requiredDoctorChecks.every((name) => checksByName.get(name) === true);
}

export default function madeleineExtension(pi: ExtensionAPI): void {
  registerMadeleine(pi);
}
