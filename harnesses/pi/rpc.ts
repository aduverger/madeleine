import { spawn } from "node:child_process";

import type { TranscriptEntry, TranscriptInput } from "./transcript.ts";

const protocolVersion = 1;
const defaultTimeoutMs = 2_000;
const defaultMaxOutputBytes = 16 * 1024 * 1024;

export const binaryInstallMessage =
  "Madeleine binary not found. Run `go install github.com/aduverger/madeleine/cmd/madeleine@latest`, then add `$(go env GOPATH)/bin` to PATH or set `MADELEINE_BIN`.";

export type AdapterErrorKind =
  | "unavailable"
  | "timeout"
  | "cancelled"
  | "oversized_output"
  | "invalid_response"
  | "process_failure"
  | "remote";

export class AdapterError extends Error {
  constructor(
    readonly kind: AdapterErrorKind,
    message: string,
    readonly code?: string,
  ) {
    super(message);
    this.name = "AdapterError";
  }
}

export interface DoctorCheck {
  name: string;
  ok: boolean;
  detail: string;
}

export interface EpisodeSummary {
  episode_id: string;
  ended_at: string;
  harness: string;
  l1: string;
}

export interface FileContext {
  path: string;
  episodes: EpisodeSummary[];
}

export type CaptureStatus = "open" | "pending_summary" | "finalized" | "abandoned";

export interface Capture {
  id: string;
  repository_id: string;
  conversation_id: string;
  conversation_key: { harness: string; external_id: string };
  worktree_root: string;
  status: CaptureStatus;
  transcript_id?: string;
  start_cursor: string;
  end_cursor?: string;
  started_at: string;
  ended_at?: string;
  last_seen_at: string;
  episode_id?: string;
}

export interface FinalizationDraft {
  capture_id: string;
  transcript_id?: string;
  status: CaptureStatus;
  empty: boolean;
  paths: string[];
  episode_id?: string;
}

export interface Episode {
  id: string;
  capture_id: string;
  repository_id: string;
  conversation_id: string;
  conversation_key: { harness: string; external_id: string };
  harness: string;
  paths: string[];
  l1: string;
  l2: string;
  transcript_id: string;
  started_at: string;
  ended_at: string;
  created_at: string;
}

export interface EpisodeDetail {
  episode_id: string;
  harness: string;
  paths: string[];
  l1: string;
  l2: string;
  transcript_id: string;
  started_at: string;
  ended_at: string;
}

export interface TranscriptView {
  transcript_id: string;
  view: "compact" | "raw";
  compact?: string;
  entries?: TranscriptEntry[];
  next_offset?: number;
}

interface ProcessResult {
  stdout: string;
  exitCode: number;
}

interface ResponseEnvelope {
  protocol_version: number;
  ok: boolean;
  result?: unknown;
  error?: { code: string; message: string };
}

interface RPCClientOptions {
  env?: NodeJS.ProcessEnv;
  timeoutMs?: number;
  maxOutputBytes?: number;
}

export class RPCClient {
  readonly binary: string;
  private readonly timeoutMs: number;
  private readonly maxOutputBytes: number;
  private readonly environment: NodeJS.ProcessEnv;

  constructor(options: RPCClientOptions = {}) {
    this.environment = { ...process.env, ...options.env };
    const configuredBinary = this.environment.MADELEINE_BIN;
    this.binary = configuredBinary?.trim() || "madeleine";
    this.timeoutMs = options.timeoutMs ?? defaultTimeoutMs;
    this.maxOutputBytes = options.maxOutputBytes ?? defaultMaxOutputBytes;
  }

  async doctor(repositoryRoot: string): Promise<DoctorCheck[]> {
    const processResult = await this.run(["doctor", "--json", "--repo", repositoryRoot]);
    if (processResult.exitCode !== 0 && processResult.exitCode !== 1) {
      throw new AdapterError("process_failure", "Madeleine doctor failed");
    }
    return decodeDoctorChecks(processResult.stdout);
  }

  async contextForPath(
    repositoryRoot: string,
    path: string,
    signal?: AbortSignal,
  ): Promise<FileContext[]> {
    return this.call(
      "context.for_paths",
      { repository_root: repositoryRoot, paths: [path] },
      validateFileContexts,
      signal,
    );
  }

  async getEpisode(
    repositoryRoot: string,
    episodeID: string,
    signal?: AbortSignal,
  ): Promise<EpisodeDetail> {
    return this.call(
      "episode.get",
      { repository_root: repositoryRoot, episode_id: episodeID },
      validateEpisodeDetail,
      signal,
    );
  }

  async startCapture(
    repositoryRoot: string,
    externalID: string,
    startCursor: string,
    signal?: AbortSignal,
  ): Promise<Capture> {
    return this.call(
      "capture.start",
      {
        repository_root: repositoryRoot,
        conversation_key: { harness: "pi", external_id: externalID },
        start_cursor: startCursor,
      },
      validateCapture,
      signal,
    );
  }

  async getCapture(captureID: string, signal?: AbortSignal): Promise<Capture> {
    return this.call("capture.get", { capture_id: captureID }, validateCapture, signal);
  }

  async listPendingCaptures(
    repositoryRoot: string,
    externalID?: string,
    signal?: AbortSignal,
  ): Promise<Capture[]> {
    const conversationKey = externalID
      ? { harness: "pi", external_id: externalID }
      : undefined;
    return this.call(
      "capture.list_pending",
      { repository_root: repositoryRoot, conversation_key: conversationKey },
      validateCaptures,
      signal,
    );
  }

  async recordWrite(captureID: string, path: string, signal?: AbortSignal): Promise<void> {
    await this.call(
      "capture.record_write",
      { capture_id: captureID, path },
      validateEmptyResult,
      signal,
    );
  }

  async sealCapture(
    captureID: string,
    endCursor: string,
    transcript?: TranscriptInput,
    signal?: AbortSignal,
  ): Promise<FinalizationDraft> {
    return this.call(
      "capture.seal",
      { capture_id: captureID, end_cursor: endCursor, transcript },
      validateFinalizationDraft,
      signal,
    );
  }

  async publishEpisode(
    captureID: string,
    l1: string,
    l2: string,
    compactEvidence: string,
    signal?: AbortSignal,
  ): Promise<Episode> {
    return this.call(
      "episode.publish",
      { capture_id: captureID, l1, l2, compact_evidence: compactEvidence },
      validateEpisode,
      signal,
    );
  }

  async getTranscript(
    repositoryRoot: string,
    transcriptID: string,
    view: "compact" | "raw",
    offset = 0,
    signal?: AbortSignal,
  ): Promise<TranscriptView> {
    return this.call(
      "transcript.get",
      { repository_root: repositoryRoot, transcript_id: transcriptID, view, offset },
      validateTranscriptView,
      signal,
    );
  }

  async abandonCapture(captureID: string, signal?: AbortSignal): Promise<void> {
    await this.call("capture.abandon", { capture_id: captureID }, validateEmptyResult, signal);
  }

  private async call<T>(
    method: string,
    params: unknown,
    validateResult: (value: unknown) => T,
    signal?: AbortSignal,
  ): Promise<T> {
    const request = JSON.stringify({ protocol_version: protocolVersion, params });
    const processResult = await this.run(["rpc", method], request, signal);
    const response = decodeEnvelope(processResult.stdout);

    if (!response.ok) {
      if (!isObject(response.error) || typeof response.error.code !== "string" || typeof response.error.message !== "string") {
        throw new AdapterError("invalid_response", "Madeleine returned an invalid error response");
      }
      throw new AdapterError("remote", response.error.message, response.error.code);
    }
    if (processResult.exitCode !== 0) {
      throw new AdapterError("process_failure", "Madeleine exited unsuccessfully");
    }
    return validateResult(response.result);
  }

  private run(
    args: string[],
    input?: string,
    signal?: AbortSignal,
  ): Promise<ProcessResult> {
    return new Promise((resolve, reject) => {
      let child;
      try {
        child = spawn(this.binary, args, {
          env: this.environment,
          shell: false,
          stdio: ["pipe", "pipe", "pipe"],
          windowsHide: true,
        });
      } catch {
        reject(new AdapterError("unavailable", "Madeleine is unavailable"));
        return;
      }

      const stdout: Buffer[] = [];
      let stdoutBytes = 0;
      let stderrBytes = 0;
      let settled = false;

      const cleanup = () => {
        clearTimeout(timeout);
        signal?.removeEventListener("abort", cancel);
      };
      const fail = (error: AdapterError) => {
        if (settled) return;
        settled = true;
        cleanup();
        child.kill();
        reject(error);
      };
      const cancel = () => fail(new AdapterError("cancelled", "Madeleine request was cancelled"));
      const timeout = setTimeout(
        () => fail(new AdapterError("timeout", "Madeleine request timed out")),
        this.timeoutMs,
      );

      if (signal?.aborted) {
        cancel();
        return;
      }
      signal?.addEventListener("abort", cancel, { once: true });

      child.stdout.on("data", (chunk: Buffer) => {
        stdoutBytes += chunk.length;
        if (stdoutBytes > this.maxOutputBytes) {
          fail(new AdapterError("oversized_output", "Madeleine stdout exceeded the output limit"));
          return;
        }
        stdout.push(chunk);
      });
      child.stderr.on("data", (chunk: Buffer) => {
        stderrBytes += chunk.length;
        if (stderrBytes > this.maxOutputBytes) {
          fail(new AdapterError("oversized_output", "Madeleine stderr exceeded the output limit"));
          return;
        }
      });
      child.on("error", () => fail(new AdapterError("unavailable", "Madeleine is unavailable")));
      child.on("close", (code) => {
        if (settled) return;
        settled = true;
        cleanup();
        resolve({
          stdout: Buffer.concat(stdout).toString("utf8"),
          exitCode: code ?? 1,
        });
      });
      child.stdin.on("error", () => undefined);
      child.stdin.end(input);
    });
  }
}

function decodeDoctorChecks(payload: string): DoctorCheck[] {
  const response = decodeEnvelope(payload);
  if (!response.ok) {
    throw new AdapterError("remote", "Madeleine operation failed", response.error?.code);
  }
  return validateDoctorChecks(response.result);
}

function decodeEnvelope(payload: string): ResponseEnvelope {
  let value: unknown;
  try {
    value = JSON.parse(payload);
  } catch {
    throw new AdapterError("invalid_response", "Madeleine returned invalid JSON");
  }
  if (!isObject(value) || value.protocol_version !== protocolVersion || typeof value.ok !== "boolean") {
    throw new AdapterError("invalid_response", "Madeleine returned an incompatible response");
  }
  if (value.ok && !("result" in value)) {
    throw new AdapterError("invalid_response", "Madeleine response has no result");
  }
  return value as unknown as ResponseEnvelope;
}

function validateDoctorChecks(value: unknown): DoctorCheck[] {
  if (!isObject(value) || !Array.isArray(value.checks)) {
    throw invalidResult();
  }
  return value.checks.map((check) => {
    if (!isObject(check) || typeof check.name !== "string" || typeof check.ok !== "boolean" || typeof check.detail !== "string") {
      throw invalidResult();
    }
    return { name: check.name, ok: check.ok, detail: check.detail };
  });
}

function validateFileContexts(value: unknown): FileContext[] {
  if (!Array.isArray(value)) throw invalidResult();
  return value.map((context) => {
    if (!isObject(context) || typeof context.path !== "string" || !Array.isArray(context.episodes)) {
      throw invalidResult();
    }
    const episodes = context.episodes.map((episode) => {
      if (
        !isObject(episode) ||
        typeof episode.episode_id !== "string" ||
        typeof episode.ended_at !== "string" ||
        typeof episode.harness !== "string" ||
        typeof episode.l1 !== "string"
      ) {
        throw invalidResult();
      }
      return {
        episode_id: episode.episode_id,
        ended_at: episode.ended_at,
        harness: episode.harness,
        l1: episode.l1,
      };
    });
    return { path: context.path, episodes };
  });
}

function validateCapture(value: unknown): Capture {
  if (
    !isObject(value) ||
    typeof value.id !== "string" ||
    typeof value.repository_id !== "string" ||
    typeof value.conversation_id !== "string" ||
    !isConversationKey(value.conversation_key) ||
    typeof value.worktree_root !== "string" ||
    !isCaptureStatus(value.status) ||
    (value.transcript_id !== undefined && typeof value.transcript_id !== "string") ||
    typeof value.start_cursor !== "string" ||
    (value.end_cursor !== undefined && typeof value.end_cursor !== "string") ||
    typeof value.started_at !== "string" ||
    (value.ended_at !== undefined && typeof value.ended_at !== "string") ||
    typeof value.last_seen_at !== "string" ||
    (value.episode_id !== undefined && typeof value.episode_id !== "string")
  ) {
    throw invalidResult();
  }
  return value as unknown as Capture;
}

function validateCaptures(value: unknown): Capture[] {
  if (!Array.isArray(value)) throw invalidResult();
  return value.map(validateCapture);
}

function validateFinalizationDraft(value: unknown): FinalizationDraft {
  if (
    !isObject(value) ||
    typeof value.capture_id !== "string" ||
    (value.transcript_id !== undefined && typeof value.transcript_id !== "string") ||
    !isCaptureStatus(value.status) ||
    typeof value.empty !== "boolean" ||
    !isStringArray(value.paths) ||
    (value.episode_id !== undefined && typeof value.episode_id !== "string")
  ) {
    throw invalidResult();
  }
  return value as unknown as FinalizationDraft;
}

function validateEmptyResult(value: unknown): void {
  if (!isObject(value) || Object.keys(value).length !== 0) throw invalidResult();
}

function isConversationKey(value: unknown): boolean {
  return (
    isObject(value) &&
    typeof value.harness === "string" &&
    typeof value.external_id === "string"
  );
}

function isCaptureStatus(value: unknown): value is CaptureStatus {
  return ["open", "pending_summary", "finalized", "abandoned"].includes(value as string);
}

function validateEpisode(value: unknown): Episode {
  if (
    !isObject(value) ||
    typeof value.id !== "string" ||
    typeof value.capture_id !== "string" ||
    typeof value.repository_id !== "string" ||
    typeof value.conversation_id !== "string" ||
    !isConversationKey(value.conversation_key) ||
    typeof value.harness !== "string" ||
    !isStringArray(value.paths) ||
    typeof value.l1 !== "string" ||
    typeof value.l2 !== "string" ||
    typeof value.transcript_id !== "string" ||
    typeof value.started_at !== "string" ||
    typeof value.ended_at !== "string" ||
    typeof value.created_at !== "string"
  ) {
    throw invalidResult();
  }
  return value as unknown as Episode;
}

function validateEpisodeDetail(value: unknown): EpisodeDetail {
  if (
    !isObject(value) ||
    typeof value.episode_id !== "string" ||
    typeof value.harness !== "string" ||
    !isStringArray(value.paths) ||
    typeof value.l1 !== "string" ||
    typeof value.l2 !== "string" ||
    typeof value.transcript_id !== "string" ||
    typeof value.started_at !== "string" ||
    typeof value.ended_at !== "string"
  ) {
    throw invalidResult();
  }
  return value as unknown as EpisodeDetail;
}

function validateTranscriptView(value: unknown): TranscriptView {
  if (
    !isObject(value) ||
    typeof value.transcript_id !== "string" ||
    (value.view !== "compact" && value.view !== "raw") ||
    (value.compact !== undefined && typeof value.compact !== "string") ||
    (value.entries !== undefined && (!Array.isArray(value.entries) || !value.entries.every(isTranscriptEntry))) ||
    (value.next_offset !== undefined &&
      (!Number.isInteger(value.next_offset) || (value.next_offset as number) < 0))
  ) {
    throw invalidResult();
  }
  if (value.view === "compact" && typeof value.compact !== "string") throw invalidResult();
  if (value.view === "raw" && !Array.isArray(value.entries)) throw invalidResult();
  return value as unknown as TranscriptView;
}

function isTranscriptEntry(value: unknown): value is TranscriptEntry {
  if (!isObject(value)) return false;
  if (["user", "assistant", "branch_summary"].includes(value.kind as string)) {
    return typeof value.text === "string";
  }
  return (
    value.kind === "mutation" &&
    (value.operation === "edit" || value.operation === "write") &&
    typeof value.path === "string" &&
    (value.status === "success" || value.status === "failure") &&
    (value.error === undefined || typeof value.error === "string")
  );
}

function invalidResult(): AdapterError {
  return new AdapterError("invalid_response", "Madeleine returned an invalid result");
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}
