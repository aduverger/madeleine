import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

export interface FakeAction {
  result?: unknown;
  error?: { code: string; message: string };
  exitCode?: number;
  protocolVersion?: number;
  rawStdout?: string;
  stdoutBytes?: number;
  stderrBytes?: number;
  delayMs?: number;
  echoParams?: boolean;
  contextEpisodes?: unknown[];
}

export interface FakeSpec {
  doctor?: FakeAction;
  methods?: Record<string, FakeAction>;
}

export interface FakeMadeleine {
  binary: string;
  requests(): Promise<Array<{ args: string[]; request?: unknown }>>;
  cleanup(): Promise<void>;
}

export async function createFakeMadeleine(spec: FakeSpec): Promise<FakeMadeleine> {
  const directory = await mkdtemp(join(tmpdir(), "madeleine-fake-"));
  const binary = join(directory, "madeleine fake");
  const requestsPath = join(directory, "requests.jsonl");
  const source = `#!/usr/bin/env node
import { appendFile } from "node:fs/promises";
const spec = ${JSON.stringify(spec)};
const args = process.argv.slice(2);
let input = "";
for await (const chunk of process.stdin) input += chunk;
let request;
if (input) request = JSON.parse(input);
await appendFile(${JSON.stringify(requestsPath)}, JSON.stringify({ args, request }) + "\\n");
const action = args[0] === "doctor" ? spec.doctor : spec.methods?.[args[1]];
if (!action) process.exit(2);
const respond = () => {
  if (action.stderrBytes) process.stderr.write("e".repeat(action.stderrBytes));
  if (action.stdoutBytes) {
    process.stdout.write("x".repeat(action.stdoutBytes));
  } else if (action.rawStdout !== undefined) {
    process.stdout.write(action.rawStdout);
  } else {
    let result = action.echoParams ? request?.params : action.result;
    if (action.contextEpisodes) {
      result = [{ path: request?.params?.paths?.[0], episodes: action.contextEpisodes }];
    }
    const envelope = action.error
      ? { protocol_version: action.protocolVersion ?? 1, ok: false, error: action.error }
      : { protocol_version: action.protocolVersion ?? 1, ok: true, result };
    process.stdout.write(JSON.stringify(envelope) + "\\n");
  }
  process.exitCode = action.exitCode ?? (action.error ? 1 : 0);
};
if (action.delayMs) setTimeout(respond, action.delayMs); else respond();
`;
  await writeFile(binary, source, "utf8");
  await chmod(binary, 0o755);

  return {
    binary,
    async requests() {
      try {
        const lines = (await readFile(requestsPath, "utf8")).trim().split("\n").filter(Boolean);
        return lines.map((line) => JSON.parse(line));
      } catch {
        return [];
      }
    },
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

export function healthyDoctorResult(): unknown {
  return {
    checks: [
      { name: "binary_version", ok: true, detail: "dev" },
      { name: "data_directory", ok: true, detail: "accessible" },
      { name: "application", ok: true, detail: "initialized" },
      { name: "schema_version", ok: true, detail: "version 4" },
      { name: "git_executable", ok: true, detail: "git version" },
      { name: "repository", ok: true, detail: "resolved" },
    ],
  };
}
