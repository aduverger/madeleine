import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const packageRoot = fileURLToPath(new URL("..", import.meta.url));
const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url)));
const expected = {
  name: "@aduverger/madeleine-pi",
  repository: "git+https://github.com/aduverger/madeleine.git",
  repositoryDirectory: "harnesses/pi",
  homepage: "https://github.com/aduverger/madeleine#readme",
  bugs: "https://github.com/aduverger/madeleine/issues",
  author: "Alexandre Duverger",
};

for (const [field, value] of Object.entries(expected)) {
  let actual = pkg[field];
  if (field === "repository") actual = pkg.repository?.url;
  if (field === "repositoryDirectory") actual = pkg.repository?.directory;
  if (field === "bugs") actual = pkg.bugs?.url;
  if (actual !== value) throw new Error(`package ${field} must be ${value}`);
}
if (pkg.publishConfig?.access !== "public") throw new Error("package must publish with public access");
if (JSON.stringify(pkg.pi?.extensions) !== JSON.stringify(["./index.ts"])) {
  throw new Error("Pi extension manifest must expose only ./index.ts");
}
for (const name of [
  "@earendil-works/pi-ai",
  "@earendil-works/pi-coding-agent",
  "typebox",
]) {
  if (pkg.dependencies?.[name]) throw new Error(`${name} must remain a peer dependency`);
  if (pkg.peerDependencies?.[name] !== "*") throw new Error(`${name} must use Pi's bundled runtime`);
}

const packResult = JSON.parse(
  execFileSync("npm", ["pack", "--dry-run", "--json", "--ignore-scripts"], {
    cwd: packageRoot,
    encoding: "utf8",
    env: {
      ...process.env,
      npm_config_cache: fileURLToPath(new URL("../.npm-cache", import.meta.url)),
    },
  }),
)[0];
const packed = new Set(packResult.files.map((file) => file.path));
const modules = [
  "commands",
  "episode-tool",
  "index",
  "lifecycle",
  "recovery",
  "render",
  "rpc",
  "state",
  "summary",
  "transcript-tool",
  "transcript",
];
const allowed = new Set([
  "LICENSE",
  "NOTICE",
  "README.md",
  "package.json",
  "scripts/verify-package.mjs",
  ...modules.map((module) => `${module}.ts`),
]);
for (const file of packed) {
  if (!allowed.has(file)) throw new Error(`package contains unapproved file: ${file}`);
}
for (const file of allowed) {
  if (!packed.has(file)) throw new Error(`package omits required file: ${file}`);
}
for (const file of packed) {
  if (/\.test\.|package-lock\.json|tsconfig|vitest/.test(file)) {
    throw new Error(`package contains forbidden development file: ${file}`);
  }
}
console.log("package verification passed");
