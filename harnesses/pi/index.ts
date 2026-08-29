import {
  isReadToolResult,
  type ExtensionAPI,
  type ExtensionContext,
  type ToolResultEvent,
} from "@earendil-works/pi-coding-agent";
import { access } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";

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
  const inputPath = event.input.path;
  if (typeof inputPath !== "string" || inputPath.length === 0) return;

  const path = await resolvePiReadPath(inputPath, ctx.cwd);
  if (claimedPaths.has(path)) return;
  claimedPaths.add(path);

  try {
    const contexts = await client.contextForPath(ctx.cwd, path, ctx.signal);
    const block = contexts.length === 1 ? renderFileContext(contexts[0]) : undefined;
    if (!block) {
      claimedPaths.delete(path);
      return;
    }
    return {
      content: [...event.content, { type: "text", text: block }],
    };
  } catch {
    claimedPaths.delete(path);
    return;
  }
}

const unicodeSpaces = /[\u00A0\u2000-\u200A\u202F\u205F\u3000]/g;

async function resolvePiReadPath(inputPath: string, cwd: string): Promise<string> {
  let path = inputPath.replace(unicodeSpaces, " ");
  if (path.startsWith("@")) path = path.slice(1);
  if (path === "~") path = homedir();
  if (path.startsWith("~/")) path = join(homedir(), path.slice(2));

  const resolved = resolve(cwd, path);
  const decomposed = resolved.normalize("NFD");
  const candidates = [
    resolved,
    resolved.replace(/ (AM|PM)\./gi, "\u202F$1."),
    decomposed,
    resolved.replaceAll("'", "’"),
    decomposed.replaceAll("'", "’"),
  ];
  for (const candidate of candidates) {
    try {
      await access(candidate);
      return candidate;
    } catch {
      // Try the next filename form accepted by Pi's built-in read tool.
    }
  }
  return resolved;
}

function doctorPassed(checks: DoctorCheck[]): boolean {
  const checksByName = new Map(checks.map((check) => [check.name, check.ok]));
  return requiredDoctorChecks.every((name) => checksByName.get(name) === true);
}

export default function madeleineExtension(pi: ExtensionAPI): void {
  registerMadeleine(pi);
}
