# Plan 7: Versioned CLI and JSON RPC boundary

PR scope: one PR  
Depends on: `plan6.md`  
Design decisions: D-016, D-020, D-021, D-022

## Goal

Expose the private Madeleine application to harnesses through a stable one-
request-per-process protocol. The CLI must be script-safe: stdout contains only
the response object, stderr contains diagnostics, and operation failures use
nonzero status.

## Application dependency

- [x] `cmd/madeleine` contains only build variables and the process entry point.
- [x] `internal/cli` owns process setup, command selection, doctor, version, and
  exit-code policy.
- [x] `internal/rpc` depends on `internal/madeleine` and dispatches to one
  `*madeleine.Service` opened per invocation.
- [x] Reuse `internal/madeleine` request/result structs under the RPC envelope;
  do not create transport copies or restore a root Go package.
- [x] Keep every Go package private; versioned JSON is the supported external
  contract.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
cmd/madeleine/main.go
internal/cli/*.go
internal/cli/*_test.go
internal/rpc/*.go
internal/rpc/*_test.go
internal/madeleine/service.go
internal/store/database.go
internal/store/migrations.go
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

- [x] Require one JSON object on stdin for every RPC call.
- [x] Require `protocol_version: 1` in each request.
- [x] Reuse the `internal/madeleine` request/result structs under a thin RPC
  envelope; do not define a second domain model.
- [x] Reject trailing non-whitespace after the request object.
- [x] Emit exactly one compact JSON object followed by `\n`.
- [x] Success envelope:

```json
{"protocol_version":1,"ok":true,"result":{}}
```

- [x] Error envelope:

```json
{"protocol_version":1,"ok":false,"error":{"code":"invalid_state","message":"..."}}
```

- [x] Keep stdout empty only when the process cannot initialize enough to
  encode a protocol response; report that case on stderr.
- [x] Add no ANSI color when stdout is JSON.

## Environment and Service initialization

- [x] Resolve `MADELEINE_HOME` in the CLI and pass it through `Options.Home`.
- [x] Open one Service per invocation and close it after encoding the result.
- [x] Treat an empty `MADELEINE_HOME` as unset.
- [x] Do not add config files, global flags for alternate databases, or daemon
  discovery.

## Error mapping

- [x] Map sentinel errors to stable codes:
  - `not_found`;
  - `conflict`;
  - `invalid_state`;
  - `not_git_repository`;
  - `outside_repository`.
- [x] Add `invalid_request`, `unsupported_protocol`, `unknown_method`,
  `database_busy`, and `internal` boundary codes.
- [x] Keep the human message useful but do not expose SQL statements, DSNs,
  credentials, environment values, or transcript content.
- [x] Use exit status `2` for invalid invocation/protocol input and `1` for an
  attempted operation that failed.

## Doctor and version

- [x] `version` prints the semantic version plus optional build commit using
  build variables that default to `dev` and `unknown`.
- [x] `doctor` checks binary version, data directory access, application
  initialization and schema version, Git executable availability, and Repository
  resolution when `--repo` or the current directory is supplied.
- [x] Human doctor output gives one line per check and exits nonzero if a
  required check fails.
- [x] `doctor --json` uses the protocol envelope and returns structured checks.
- [x] Being outside Git is a failed Repository check but must not prevent the
  database checks from running.

## Tests

- [x] Golden-test every request and response JSON shape.
- [x] Test malformed JSON, missing/unknown protocol version, unknown method,
  missing method argument, trailing JSON, and empty stdin.
- [x] Test every sentinel-error mapping and generic internal sanitization.
- [x] Capture stdout/stderr separately and assert no diagnostic contaminates
  stdout.
- [x] Test exit statuses for success, invalid invocation, and operation error.
- [x] Run two real CLI subprocesses concurrently against one temporary home.
- [x] Test `MADELEINE_HOME`, paths containing spaces, and a read-only home.
- [x] Test human and JSON doctor output inside and outside Git.
- [x] Build and run the binary on Linux and macOS CI.

## Acceptance criteria

- [x] A TypeScript process can call every Service operation without shell
  interpolation or parsing human output.
- [x] Protocol version mismatch fails explicitly rather than being guessed.
- [x] `go install github.com/aduverger/madeleine/cmd/madeleine@<ref>` produces a
  self-contained binary on supported platforms.
- [x] No RPC method contains Pi-specific behavior or rendering.

## Excluded from this PR

Long-lived RPC, sockets, streaming, MCP, authentication, and Pi integration.

## Plan changes

After reviewing the first implementation against `Emidat/dev-machines`, GitHub
CLI, Hugo, kubectl, lazygit, and Gum, the user chose a thinner executable entry
point:

- `cmd/madeleine/main.go` now supplies build variables and delegates directly to
  `internal/cli.Main`;
- command selection, environment and stream wiring, doctor, version, and OS exit
  policy moved to `internal/cli`;
- `internal/rpc` now returns protocol outcomes rather than owning numeric process
  exit codes, while retaining request decoding, safe error mapping, response
  encoding, Service lifetime, and method dispatch;
- CLI and subprocess tests moved out of the executable package and alongside the
  command adapter.

A root `cli/` package was not added. The cited `dev-machines/cli` directory is a
separate module in a monorepo, while Madeleine is one application module and
keeps all implementation packages private.

After further user review, request parsing was simplified for this trusted,
one-request CLI boundary. It now reads stdin once and uses `json.Unmarshal` for
the envelope and method parameters. The custom object scanner, trailing-value
helper, second decoder pass, and strict unknown-field policy were removed.

## Entire inspection and reuse record

Inspected `entireio/cli` at commit
`60773bd4b89e487a897958b00a1d168a7ea5aa01`, especially:

- `cmd/entire/main.go`;
- `cmd/entire/cli/root.go`;
- `cmd/entire/cli/versioninfo/versioninfo.go`;
- `cmd/entire/cli/versioninfo/versioninfo_test.go`;
- `cmd/entire/cli/doctor.go`;
- `cmd/entire/cli/doctor_test.go`.

Madeleine adapted Entire's fallback from ldflag defaults to Go embedded build
information, including tagged module versions and VCS revisions. `NOTICE` lists
the inspected commit under Entire's MIT attribution. Entire's Cobra command
tree and doctor implementation were not reused: Plan 7 requires only three
standard-library commands, while Entire's doctor repairs Entire-specific Git
metadata, hooks, logs, and sessions and has no equivalent one-shot JSON RPC
contract.

## Assumptions and deviations

Listed least-confident first:

1. The plan specified response envelopes but not the exact request shape. RPC
   requests therefore use `{"protocol_version":1,"params":{...}}`. Unknown
   fields are accepted so additive client changes remain compatible; the
   explicit protocol version covers breaking changes.
2. `doctor --json` returns a successful protocol envelope containing all check
   results even when a required check fails, while the process exits `1`. This
   separates successful diagnosis from the health state being diagnosed.
3. A data-directory check creates and removes one private temporary probe file.
   Application initialization remains a separate check, so a malformed database
   does not incorrectly imply that its directory is unwritable.
4. `capture.get` and `capture.abandon` use one transport-only
   `{capture_id: ...}` parameter wrapper because their canonical Service methods
   accept a scalar ID; no duplicate Capture domain type was introduced.
5. RPC methods that return only an error encode an empty object as `result`, not
   `null`, keeping the documented success shape consistent.
6. `version` prints `<version>` when the commit is unknown and
   `<version> (<commit>)` otherwise. Tagged `go install` builds recover their
   module version from embedded Go build information even without release
   ldflags.
7. `internal/madeleine.Service.SchemaVersion` and the store's schema query were
   added solely to let doctor report the migrated database version without
   exposing SQLite outside the store layer.
