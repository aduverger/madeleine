# Plan 6: Versioned CLI and JSON RPC boundary

PR scope: one PR  
Depends on: `plan5.md`  
Design decisions: D-010, D-016, D-020, D-021

## Goal

Expose the Go library to non-Go harnesses through a stable one-request-per-
process protocol. The CLI must be script-safe: stdout contains only the response
object, stderr contains diagnostics, and operation failures use nonzero status.

## Pre-implementation package structure

The core was reorganized before this plan so the CLI does not grow around a
flat application package:

```text
madeleine.go, types.go  public Go API and Store facade
internal/store/         domain implementation, SQLite, and migrations
internal/gitstate/      read-only Git snapshots and reconciliation
internal/gitcmd/        Git process execution
internal/repopath/      canonical repository-relative path normalization
cmd/madeleine/          executable entry point introduced by this plan
internal/rpc/            JSON protocol introduced by this plan
```

`internal/store` writes SQL directly. There is no storage interface,
configurable backend, or speculative `store/sqlite` hierarchy. The root facade
uses explicit conversions so its documented public structs do not expose types
from an `internal` package. `cmd/madeleine` is preferred to a root `cli/`
package because the command is an executable boundary, not a reusable public Go
API; command orchestration may move to `internal/cli` only if it becomes
substantial.

### Restructure decision ledger

Listed least-confident first:

1. The public facade uses explicit conversions between root API structs and
   private Store structs. Type aliases were simpler but made `go doc` hide the
   public fields behind the inaccessible `internal/store` package; keeping the
   root API self-documenting was judged more important than eliminating this
   boundary mapping.
2. Existing white-box tests moved with the implementation into
   `internal/store`; a public end-to-end facade test now covers every conversion
   path used by the normal Capture-to-Episode flow.
3. Embedded migrations moved under `internal/store/migrations` with unchanged
   SQL and version history. This changes source layout only, not database paths
   or schema behavior.
4. Git snapshot/reconciliation and path normalization became separate internal
   packages because both have coherent ownership outside SQLite persistence.
5. No root `cli/` package was added. Plan 6 starts with `cmd/madeleine` and
   `internal/rpc`, adding `internal/cli` only if command orchestration later
   becomes large enough to own a package.

## Entire reuse gate

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
cmd/madeleine/main.go
internal/rpc/protocol.go
internal/rpc/dispatch.go
internal/rpc/errors.go
internal/rpc/methods.go
internal/rpc/*_test.go
cmd/madeleine/main_test.go
```

Use the standard library `flag`, `encoding/json`, and `os` packages. Do not add
Cobra or another CLI framework.

## Command surface

```text
madeleine version
madeleine doctor [--json] [--repo <path>]
madeleine rpc <method>
```

RPC methods:

```text
capture.start
capture.get
capture.record_write
capture.list_pending
capture.seal
capture.abandon
episode.publish
context.for_paths
episode.get
```

## Protocol

- [ ] Require one JSON object on stdin for every RPC call.
- [ ] Require `protocol_version: 1` in each request.
- [ ] Reuse the public request/result structs under a thin RPC envelope; do not
  define a second domain model.
- [ ] Reject trailing non-whitespace after the request object.
- [ ] Emit exactly one compact JSON object followed by `\n`.
- [ ] Success envelope:

```json
{"protocol_version":1,"ok":true,"result":{}}
```

- [ ] Error envelope:

```json
{"protocol_version":1,"ok":false,"error":{"code":"invalid_state","message":"..."}}
```

- [ ] Keep stdout empty only when the process cannot initialize enough to
  encode a protocol response; report that case on stderr.
- [ ] Add no ANSI color when stdout is JSON.

## Environment and Store initialization

- [ ] Resolve `MADELEINE_HOME` in the CLI and pass it through `Options.Home`.
- [ ] Open one Store per invocation and close it after encoding the result.
- [ ] Treat an empty `MADELEINE_HOME` as unset.
- [ ] Do not add config files, global flags for alternate databases, or daemon
  discovery.

## Error mapping

- [ ] Map sentinel errors to stable codes:
  - `not_found`;
  - `conflict`;
  - `invalid_state`;
  - `not_git_repository`;
  - `outside_repository`.
- [ ] Add `invalid_request`, `unsupported_protocol`, `unknown_method`,
  `database_busy`, and `internal` boundary codes.
- [ ] Keep the human message useful but do not expose SQL statements, DSNs,
  credentials, environment values, or transcript content.
- [ ] Use exit status `2` for invalid invocation/protocol input and `1` for an
  attempted operation that failed.

## Doctor and version

- [ ] `version` prints the semantic version plus optional build commit using
  build variables that default to `dev` and `unknown`.
- [ ] `doctor` checks binary version, data directory access, Store open and
  schema version, Git executable availability, and Repository resolution when
  `--repo` or the current directory is supplied.
- [ ] Human doctor output gives one line per check and exits nonzero if a
  required check fails.
- [ ] `doctor --json` uses the protocol envelope and returns structured checks.
- [ ] Being outside Git is a failed Repository check but must not prevent the
  database checks from running.

## Tests

- [ ] Golden-test every request and response JSON shape.
- [ ] Test malformed JSON, missing/unknown protocol version, unknown method,
  missing method argument, trailing JSON, and empty stdin.
- [ ] Test every sentinel-error mapping and generic internal sanitization.
- [ ] Capture stdout/stderr separately and assert no diagnostic contaminates
  stdout.
- [ ] Test exit statuses for success, invalid invocation, and operation error.
- [ ] Run two real CLI subprocesses concurrently against one temporary home.
- [ ] Test `MADELEINE_HOME`, paths containing spaces, and a read-only home.
- [ ] Test human and JSON doctor output inside and outside Git.
- [ ] Build and run the binary on Linux and macOS CI.

## Acceptance criteria

- [ ] A TypeScript process can call every Store operation without shell
  interpolation or parsing human output.
- [ ] Protocol version mismatch fails explicitly rather than being guessed.
- [ ] `go install github.com/aduverger/madeleine/cmd/madeleine@<ref>` produces a
  self-contained binary on supported platforms.
- [ ] No RPC method contains Pi-specific behavior or rendering.

## Excluded from this PR

Long-lived RPC, sockets, streaming, MCP, authentication, and Pi integration.
