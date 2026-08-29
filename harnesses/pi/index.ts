import {
  isReadToolResult,
  type ExtensionAPI,
  type ExtensionContext,
  type ToolResultEvent,
} from "@earendil-works/pi-coding-agent";
import { registerCommands } from "./commands.ts";
import { registerEpisodeTool } from "./episode-tool.ts";
import { CaptureLifecycle, type CaptureClient, resolvePiToolPath } from "./lifecycle.ts";
import { type DoctorCheck, type EpisodeDetail, type FileContext, RPCClient } from "./rpc.ts";
import { renderFileContext } from "./render.ts";
import { PiState } from "./state.ts";

interface MadeleineClient extends CaptureClient {
  doctor(repositoryRoot: string): Promise<DoctorCheck[]>;
  abandonCapture(captureID: string, signal?: AbortSignal): Promise<void>;
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
  const state = new PiState(pi);

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

  const ready = async (ctx: ExtensionContext) => {
    detection ??= detect(ctx);
    await detection;
    return enabled;
  };

  const lifecycle = new CaptureLifecycle(client, state, ready);

  pi.on("tool_result", async (event, ctx) => {
    if (!enabled || event.isError || !isReadToolResult(event)) return;
    return enrichRead(event, ctx, client, state);
  });
  lifecycle.register(pi);

  registerEpisodeTool(pi, client, () => enabled);
  registerCommands(pi, client, lifecycle);
}

async function enrichRead(
  event: ToolResultEvent,
  ctx: ExtensionContext,
  client: MadeleineClient,
  state: PiState,
): Promise<{ content: ToolResultEvent["content"] } | undefined> {
  const inputPath = event.input.path;
  if (typeof inputPath !== "string" || inputPath.length === 0) return;

  let path: string;
  try {
    path = await resolvePiToolPath(inputPath, ctx.cwd);
  } catch {
    return;
  }
  if (!state.claimPath(path)) return;

  try {
    const contexts = await client.contextForPath(ctx.cwd, path, ctx.signal);
    const block = contexts.length === 1 ? renderFileContext(contexts[0]) : undefined;
    if (!block) {
      state.releasePath(path);
      return;
    }
    state.recordInjectedPath(path);
    return {
      content: [...event.content, { type: "text", text: block }],
    };
  } catch {
    state.releasePath(path);
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
