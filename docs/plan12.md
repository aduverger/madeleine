# Plan 12: Recovery, end-to-end hardening, and Pi MVP

PR scope: one PR  
Depends on: `plan11.md`  
Design decisions: D-006, D-014, D-015, D-016, D-019, D-020, D-021, D-023, D-024, D-025

## Goal

Finish the Pi MVP by retrying sealed pending Captures, validating crash
reattachment and the real Go/SQLite/TypeScript vertical slice, and documenting
installation and operations. Recovery must not delay or contaminate current
work.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
harnesses/pi/recovery.ts
harnesses/pi/lifecycle.ts
harnesses/pi/commands.ts
harnesses/pi/*.test.ts
test/e2e/*
README.md
```

## Startup behavior

Plan 9 already establishes the open-Capture policy:

1. Resolve the current Repository and Conversation.
2. Restore and validate persisted Pi state when available.
3. Reattach the Conversation's single open Capture after reload or abrupt
   process exit.
4. Start a new Capture only when that Conversation has no open Capture.
5. Start background retry for older `pending_summary` Captures.

- [x] Verify this policy with the real binary and persisted Pi entries.
- [x] Never inherit Capture state into a new or forked Conversation.
- [x] Treat multiple open Captures for one Conversation as invalid storage and
  disable write capture rather than choosing one arbitrarily.
- [x] Do not infer or attach filesystem changes made while Pi was stopped.
- [x] Empty open Captures remain open when reattached and are abandoned only
  when a later clean seal or manual capture confirms that they have no
  structured paths.

## Background pending-summary worker

- [x] Maintain one worker per extension runtime and one active summary model
  call at a time.
- [x] Snapshot `pending_summary` Captures for the current Conversation
  oldest-first after the current open Capture is attached or started.
- [x] Attempt each queued Capture at most once during that runtime.
- [x] Reuse Plan 11's persisted raw Transcript entries plus Plan 10's semantic
  projection, model-sized chunking, validation, and publication functions; add
  no second recovery implementation.
- [x] Continue after a per-Capture validation or publication failure while
  reporting one concise notification.
- [x] Associate an AbortController with the worker.
- [x] On session shutdown, abort and await worker cleanup before finalizing the
  current Capture. An aborted old Capture remains pending.
- [x] Never append recovery prompts or results to the active Conversation.

## Recovery invariants

- [x] Restart reattachment preserves the original Capture ID, start cursor,
  recorded paths, and injected-path deduplication state.
- [x] A clean shutdown or `/madeleine capture` freezes the old Capture before a
  new Capture starts.
- [x] Recording current writes cannot update a sealed pending Capture.
- [x] Publishing an old pending Capture cannot change the current open Capture's
  status or raw paths.
- [x] Pending-summary retry never blocks path recording for the current Capture.
- [x] Repeated crashes leave one open Capture and independently recoverable
  sealed Captures for the Conversation.

## End-to-end harness

Build a test harness using:

- the real compiled `madeleine` binary;
- a real temporary SQLite home;
- real temporary Git repositories for Repository identity only;
- a fake Pi `ExtensionAPI`, session manager, and deterministic model registry.

Cover:

- [x] Work interval A edits two files, quits, publishes one Episode, and work
  interval B reads one path and receives A's L1.
- [x] B calls `madeleine_episode` and receives A's L2 and Transcript ID.
- [x] B retrieves A's compact Transcript and pages through its raw Transcript
  without reading the original Pi session file.
- [x] `/reload` during A retains the Capture and injects each path once.
- [x] Hard crash after a write reattaches A and preserves its original boundary
  and paths.
- [x] `/madeleine capture` publishes or leaves A pending, then starts B in the
  same Conversation.
- [x] B writes while an older pending summary retries; the Captures retain
  disjoint structured path sets.
- [x] Summary failure followed by restart succeeds on automatic retry.
- [x] Multiple pending Captures recover oldest-first with one model call active.
- [x] Empty interval and structured edit/write behavior.
- [x] Shell-only, generated, formatted, human, and other-session changes are not
  attributed without a successful structured mutation event.
- [x] Missing binary, wrong protocol, SQLite busy, repository-discovery failure,
  missing model, malformed summary, and forced process termination all fail
  open.

## Concurrency verification

- [x] Add a Go integration test with concurrent exact-path Episode readers,
  Capture path writers, and Episode publishers against one WAL database.
- [x] Assert no lost writes and no unexpected `SQLITE_BUSY` after bounded retry/
  timeout handling.
- [x] Record benchmark helpers for 1, 10, 100, and 500 simulated agents, but do
  not make speculative performance thresholds release blockers.
- [x] Document measured lookup/publication latency and busy count from a local
  representative run in the PR description, not as committed product claims.

## README and operations

- [x] Replace the placeholder README with a concise explanation of the north
  star, Capture/Episode lifecycle, Pi MVP behavior, and trust/privacy model.
- [x] Document development installation:

```text
go install github.com/aduverger/madeleine/cmd/madeleine@main
pi install npm:@aduverger/madeleine-pi@0.1.0
```

- [x] Document release installation using `@v0.1.0`, noting that the tag is
  created after merge rather than by this PR.
- [x] Document `MADELEINE_HOME`, `MADELEINE_BIN`, database locations,
  `madeleine doctor`, all `/madeleine` commands including `capture`, and the
  Episode/Transcript tools.
- [x] Explain what is stored: intentionally mutated paths, summaries,
  timestamps, sanitized cursor-bounded semantic Transcript entries, and the
  compact evidence for published Episodes. Original harness transcript files,
  read calls/results, and edit/write file bodies are not copied.
- [x] Explain crash reattachment, automatic pending-summary recovery, manual
  capture/abandon, and how to disable or remove the Pi package.
- [x] State clearly that opaque shell and filesystem changes are not attributed
  in v0.1.
- [x] Link `design.md` and the completed plan files.
- [x] Keep future features clearly labeled rather than promising them in v0.1.

## Final CI and smoke tests

- [ ] Run `make check` and `npm run check` on Linux and macOS.
- [x] Verify `go install` from a clean module cache.
- [x] Verify `pi install` package discovery from a clean temporary Pi home.
- [ ] Manually run Pi against a disposable Git repository through edit,
  capture, shutdown, read-context, L2 lookup, compact/raw Transcript lookup,
  reload, and crash/resume scenarios.
- [x] Run `git diff --check` and confirm no generated DB, npm cache, binary, or
  test repository is tracked.
- [ ] Mark checkboxes in `plan1.md` through `plan12.md` only for work actually
  merged in the stack.

## Plan revisions and decision ledger

Listed least-confident first:

1. Recovery remains automatic rather than exposing its queue as a user command.
   Each pending Capture is attempted once per extension runtime and remains
   pending for the next runtime after failure. Current path recording never uses
   the recovery queue. The startup snapshot also excludes the Capture that was
   current when recovery began, even if a concurrent manual capture seals it
   before the list query completes.
2. The vertical-slice suite uses a deterministic fake Pi event/session/model
   runtime but crosses the real child-process JSON boundary into a freshly
   compiled Madeleine binary, temporary Git repositories, and temporary SQLite
   homes. Calling an authenticated external model in CI would make recovery
   tests nondeterministic and billable. If asking were free, I would have asked
   whether a billable manual Pi smoke run was required before opening the PR;
   that checklist item remains open.
3. `RPCClient` now merges its configured environment into the inherited process
   environment. This lets isolated installations select `MADELEINE_HOME`
   without losing `PATH` and other variables required by Git and the runtime.
   If asking were free, I would have confirmed whether callers expected a
   supplied environment to replace inheritance entirely.
4. Entire CLI commit `60773bd4b89e487a897958b00a1d168a7ea5aa01` was inspected
   at `cmd/entire/cli/agent/pi/lifecycle.go`, its lifecycle tests, the embedded
   Pi extension, and session/transcript readers. No Plan 12 recovery code was
   copied: Entire snapshots native transcript files and drives Git checkpoint
   turns, while Madeleine retries immutable SQLite Transcripts and publishes
   cursor-bounded Episodes. Existing attribution for earlier compatible process
   and extraction mechanics remains in `NOTICE`.
5. Startup always checks the canonical Conversation-scoped open-Capture list,
   even when Pi state names a Capture. This costs one list RPC but detects
   invalid multiple-open storage instead of trusting one persisted ID and
   choosing it arbitrarily.
6. The background worker may publish older pending Episodes while the current
   Capture records writes. SQLite WAL and Capture-owned rows make this safe, but
   the end-to-end suite must prove that summary retry does not create write
   starvation.
7. A crash no longer creates a second Capture. Reattaching the same open Capture
   preserves the intended coarse Episode boundary now that downtime filesystem
   changes cannot enter through Git reconciliation.
8. Git remains in the end-to-end harness only because Repository identity is
   Git-based. Dirty-start, staged, commit, and branch-switch attribution tests
   were removed because v0.1 deliberately ignores those unstructured changes.
9. `/madeleine capture` is the explicit way to create multiple Episodes inside
   one long-running Conversation. It shares finalization with shutdown rather
   than introducing a second publication path.
10. This recovery/MVP plan moved from Plan 11 to Plan 12. Persisted bounded
   transcripts and evidence retrieval must land first so end-to-end hardening
   validates the final evidence model rather than transcript-file references.

## MVP acceptance criteria

- [ ] The complete clean-interval, manual-capture, read-context, L2, and bounded
  Transcript retrieval flow works with real Pi.
- [x] Crash/resume reattaches one open Capture and preserves its structured paths
  without blocking startup.
- [x] Pending summaries retry automatically without interfering with the current
  Capture.
- [x] All failures preserve Pi behavior and recoverable data.
- [x] Exact-path retrieval remains deterministic and contains no semantic
  search/ranking or inferred filesystem attribution layer.
- [x] The application, CLI, schema, Pi package, installation, and recovery
  behavior agree with `design.md`.
- [ ] The repository is ready for a post-merge `v0.1.0` tag.

## Excluded from this PR and v0.1

Other harnesses, external transcript backfill, Git/filesystem reconciliation,
Agent Trace, MCP server, folder/symbol/rename memory, unsanitized harness-file
copies, multiplayer UI, daemon, Postgres, network sync, embeddings, and semantic
ranking.
