# Plan 9: Pi Capture lifecycle

PR scope: one PR  
Depends on: `plan8.md`  
Design decisions: D-004, D-005, D-006, D-007, D-011, D-014, D-016, D-021, D-023

## Goal

Connect Pi lifecycle and successful mutation events to Captures. Preserve one
Capture across `/reload`, create new Captures for distinct runs, and expose
operational status/abandon commands. Episode summarization starts in Plan 10.

## Entire reuse gate

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

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

- [ ] Custom entries do not enter model context and are not given a renderer.
- [ ] Restore only the newest valid entry for the current Conversation.
- [ ] Append a new snapshot after Capture creation and after each newly
  injected path; sort/deduplicate paths before saving.
- [ ] Replace Plan 8's runtime-only injection set with this state so reload
  retains deduplication.

## Conversation identity

- [ ] Use the cleaned absolute current Pi session-file path as the stable
  external Conversation ID and transcript reference when available.
- [ ] For an ephemeral session, generate a UUIDv7 runtime Conversation ID and
  leave transcript reference empty; document that crash recovery cannot span
  process death for that session.
- [ ] Scope every Conversation to `ctx.cwd`'s resolved Repository and harness
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

- [ ] Store `ctx.sessionManager.getLeafId()` as the start/end cursor.
- [ ] On reload, validate the persisted Capture with `capture.get` and require
  `status=open`.
- [ ] If reload state is missing, query pending Captures for the Conversation
  and reattach its single open Capture; start a new one only when none exists.
- [ ] Until Plan 11, an older open Capture encountered on non-reload startup
  causes a one-time warning and disables new write capture for that run rather
  than reattaching or corrupting boundaries.

## Mutation recording

- [ ] Listen to successful `tool_result` events for built-in `edit` and
  `write` only.
- [ ] Extract and normalize the typed path; failed results record nothing.
- [ ] Record the first occurrence immediately with `capture.record_write`.
- [ ] Keep an in-memory `path -> last persisted monotonic time` map and refresh
  a repeated path no more often than every 30 seconds.
- [ ] Await the short RPC call but preserve the original Pi result on all
  failures.
- [ ] Do not parse Bash commands; Plan 5's seal reconciliation covers surviving
  shell changes.

## Shutdown sealing

- [ ] Cancel extension-owned in-flight RPC work before non-reload shutdown.
- [ ] Call `capture.seal` with the current leaf ID.
- [ ] Treat an empty result as successfully abandoned.
- [ ] Leave non-empty Captures `pending_summary`; Plan 10 publishes them.
- [ ] Notify once on failure and leave an open Capture for later recovery.

## Commands

Register one Pi command, `/madeleine`, and parse these strict subcommands:

```text
/madeleine status
/madeleine abandon <capture-id>
/madeleine doctor
```

- [ ] `status` lists open/pending Captures for the current Repository and marks
  the current one.
- [ ] `abandon` only accepts an ID returned for the current Repository and asks
  for UI confirmation before RPC; finalized Captures cannot be abandoned.
- [ ] `doctor` shows the existing structured checks in Pi UI.
- [ ] Unknown or missing subcommands print concise usage without invoking RPC.

## Tests

- [ ] Exercise every lifecycle matrix row with a fake session manager.
- [ ] Verify reload reattachment and persisted injection deduplication.
- [ ] Verify startup/resume never reattaches an old run.
- [ ] Successful edit/write, repeated throttled write, failed tool result,
  read, Bash, and malformed path cases.
- [ ] Seal success, empty abandonment, Git/RPC failure, and reload no-op.
- [ ] Persist/restore state across a simulated extension module reload.
- [ ] Ephemeral Conversation behavior.
- [ ] Status output, abandon confirmation/cancel, wrong-Repository ID, and
  doctor output.

## Acceptance criteria

- [ ] Pi writes are durable in SQLite before the Pi run ends.
- [ ] `/reload` retains exactly one open Capture and does not duplicate
  historical context.
- [ ] Clean non-reload shutdown leaves a deterministic sealed draft.
- [ ] The adapter still fails open when every lifecycle RPC is forced to fail.

## Excluded from this PR

Model summarization, Episode publication, automatic stale-Capture recovery, and
multi-agent Capture activity display.
