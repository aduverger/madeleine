# Plan 9: Pi Capture lifecycle

PR scope: one PR  
Depends on: `plan8.md`  
Design decisions: D-004, D-005, D-006, D-007, D-011, D-014, D-016, D-021, D-023

## Goal

Connect Pi lifecycle and successful mutation events to Captures. Preserve one
Capture across `/reload`, create new Captures for distinct runs, and expose
operational status/abandon commands. Episode summarization starts in Plan 10.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
harnesses/pi/index.ts
harnesses/pi/state.ts
harnesses/pi/lifecycle.ts
harnesses/pi/commands.ts
harnesses/pi/*.test.ts
```

## Persisted Pi state

Use `pi.appendEntry("madeleine-state-v1", data)` with:

```text
version: 1
conversation_id: string
capture_id: string
injected_paths: string[]
```

- [x] Custom entries do not enter model context and are not given a renderer.
- [x] Restore only the newest valid entry for the current Conversation.
- [x] Append a new snapshot after Capture creation and after each newly
  injected path; sort/deduplicate paths before saving.
- [x] Replace Plan 8's runtime-only injection set with this state so reload
  retains deduplication.

## Conversation identity

- [x] Use the cleaned absolute current Pi session-file path as the stable
  external Conversation ID and transcript reference when available.
- [x] For an ephemeral session, generate a UUIDv7 runtime Conversation ID and
  leave transcript reference empty; document that crash recovery cannot span
  process death for that session.
- [x] Scope every Conversation to `ctx.cwd`'s resolved Repository and harness
  `pi`.

## Lifecycle matrix

| Event | Required behavior |
|---|---|
| `session_start: startup` | Start a new Capture when no older open Capture blocks it. |
| `session_start: new` | Start a new Capture for the new Conversation. |
| `session_start: resume` | Start a new Capture; never reattach a previous run. |
| `session_start: fork` | Start a new Capture for the forked Conversation. |
| `session_start: reload` | Reattach the existing open Capture. |
| `session_shutdown: reload` | Preserve the open Capture; do not seal. |
| other `session_shutdown` | Seal the current Capture. |

- [x] Store `ctx.sessionManager.getLeafId()` as the start/end cursor.
- [x] On reload, validate the persisted Capture with `capture.get` and require
  `status=open`.
- [x] If reload state is missing, query pending Captures for the Conversation
  and reattach its single open Capture; start a new one only when none exists.
- [x] Until Plan 11, an older open Capture encountered on non-reload startup
  causes a one-time warning and disables new write capture for that run rather
  than reattaching or corrupting boundaries.

## Mutation recording

- [x] Listen to successful `tool_result` events for built-in `edit` and
  `write` only.
- [x] Extract and normalize the typed path; failed results record nothing.
- [x] Record the first occurrence immediately with `capture.record_write`.
- [x] Keep an in-memory `path -> last persisted monotonic time` map and refresh
  a repeated path no more often than every 30 seconds.
- [x] Await the short RPC call but preserve the original Pi result on all
  failures.
- [x] Do not parse Bash commands; Plan 5's seal reconciliation covers surviving
  shell changes.

## Shutdown sealing

- [x] Cancel extension-owned in-flight RPC work before non-reload shutdown.
- [x] Call `capture.seal` with the current leaf ID.
- [x] Treat an empty result as successfully abandoned.
- [x] Leave non-empty Captures `pending_summary`; Plan 10 publishes them.
- [x] Notify once on failure and leave an open Capture for later recovery.

## Commands

Register one Pi command, `/madeleine`, and parse these strict subcommands:

```text
/madeleine status
/madeleine abandon <capture-id>
/madeleine doctor
```

- [x] `status` lists open/pending Captures for the current Repository and marks
  the current one.
- [x] `abandon` only accepts an ID returned for the current Repository and asks
  for UI confirmation before RPC; finalized Captures cannot be abandoned.
- [x] `doctor` shows the existing structured checks in Pi UI.
- [x] Unknown or missing subcommands print concise usage without invoking RPC.

## Tests

- [x] Exercise every lifecycle matrix row with a fake session manager.
- [x] Verify reload reattachment and persisted injection deduplication.
- [x] Verify startup/resume never reattaches an old run.
- [x] Successful edit/write, repeated throttled write, failed tool result,
  read, Bash, and malformed path cases.
- [x] Seal success, empty abandonment, Git/RPC failure, and reload no-op.
- [x] Persist/restore state across a simulated extension module reload.
- [x] Ephemeral Conversation behavior.
- [x] Status output, abandon confirmation/cancel, wrong-Repository ID, and
  doctor output.

## Acceptance criteria

- [x] Pi writes are durable in SQLite before the Pi run ends.
- [x] `/reload` retains exactly one open Capture and does not duplicate
  historical context.
- [x] Clean non-reload shutdown leaves a deterministic sealed draft.
- [x] The adapter still fails open when every lifecycle RPC is forced to fail.

## Implementation record and decision ledger

Recorded least-confident decisions first:

1. Pi can report no leaf entry at the start of an empty session, while
   `capture.start` requires a non-empty, later-resolvable cursor. In that case
   the adapter appends one non-context `madeleine-boundary-v1` custom entry and
   uses its real Pi entry ID. No synthetic cursor is stored.
2. Historical reads remain available when an older open Capture disables writes.
   Because there is then no current Capture ID for a valid state snapshot,
   injection deduplication is runtime-only for that blocked run. Normal and
   reload-attached runs persist every successful injection as specified.
3. For an ephemeral session, reload restores the newest valid state on the
   current Pi branch because no independent session-file identity exists.
   Process death loses that identity by design and creates a new UUIDv7.
4. `/madeleine abandon` re-queries pending Captures in the current Repository
   immediately before confirmation; “an ID returned for the current
   Repository” means an ID returned by that authoritative query, not a cached
   prior `/madeleine status` invocation.

Scope deviations required by the implementation:

- `harnesses/pi/rpc.ts` and its tests gained the existing Capture RPC methods
  and strict result validation; lifecycle code does not duplicate protocol
  handling.
- `harnesses/pi/package.json` now publishes `state.ts`, `lifecycle.ts`, and
  `commands.ts`; otherwise an installed npm package could not load `index.ts`.
- `harnesses/pi/index.ts` reuses one persisted `PiState` for both lifecycle and
  Plan 8 read deduplication.

Entire reuse review:

- Inspected `entireio/cli` commit
  `60773bd4b89e487a897958b00a1d168a7ea5aa01`, especially
  `.pi/extensions/entire/index.ts`,
  `cmd/entire/cli/agent/pi/entire_extension.ts`,
  `cmd/entire/cli/agent/pi/lifecycle.go`, and
  `cmd/entire/cli/agent/pi/lifecycle_test.go`.
- Entire's fail-open Pi event bridge confirms that lifecycle subprocess failure
  must not affect Pi. Its Git-checkpoint/session cache semantics do not provide
  Madeleine's SQLite Capture ownership, reload reattachment, immediate exact
  write persistence, throttling, or repository-scoped commands. No source was
  copied, so no new upstream attribution was required.

## Excluded from this PR

Model summarization, Episode publication, automatic stale-Capture recovery, and
multi-agent Capture activity display.
