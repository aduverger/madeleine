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

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

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

- [ ] Verify this policy with the real binary and persisted Pi entries.
- [ ] Never inherit Capture state into a new or forked Conversation.
- [ ] Treat multiple open Captures for one Conversation as invalid storage and
  disable write capture rather than choosing one arbitrarily.
- [ ] Do not infer or attach filesystem changes made while Pi was stopped.
- [ ] Empty open Captures remain open when reattached and are abandoned only
  when a later clean seal or rollover confirms that they have no structured
  paths.

## Background pending-summary worker

- [ ] Maintain one worker per extension runtime and one active summary model
  call at a time.
- [ ] Snapshot `pending_summary` Captures for the current Conversation
  oldest-first after the current open Capture is attached or started.
- [ ] Attempt each queued Capture at most once during that runtime.
- [ ] Reuse Plan 11's persisted compact transcript plus Plan 10's model,
  validation, and publication functions; add no second recovery implementation.
- [ ] Continue after a per-Capture validation or publication failure while
  reporting one concise notification.
- [ ] Associate an AbortController with the worker.
- [ ] On session shutdown, abort and await worker cleanup before finalizing the
  current Capture. An aborted old Capture remains pending.
- [ ] Never append recovery prompts or results to the active Conversation.

## Recovery invariants

- [ ] Restart reattachment preserves the original Capture ID, start cursor,
  recorded paths, and injected-path deduplication state.
- [ ] A clean shutdown or manual rollover freezes the old Capture before a new
  Capture starts.
- [ ] Recording current writes cannot update a sealed pending Capture.
- [ ] Publishing an old pending Capture cannot change the current open Capture's
  status or raw paths.
- [ ] Pending-summary retry never blocks path recording for the current Capture.
- [ ] Repeated crashes leave one open Capture and independently recoverable
  sealed Captures for the Conversation.

## End-to-end harness

Build a test harness using:

- the real compiled `madeleine` binary;
- a real temporary SQLite home;
- real temporary Git repositories for Repository identity only;
- a fake Pi `ExtensionAPI`, session manager, and deterministic model registry.

Cover:

- [ ] Work interval A edits two files, quits, publishes one Episode, and work
  interval B reads one path and receives A's L1.
- [ ] B calls `madeleine_episode` and receives A's L2 and Transcript ID.
- [ ] B retrieves A's compact Transcript and pages through its raw Transcript
  without reading the original Pi session file.
- [ ] `/reload` during A retains the Capture and injects each path once.
- [ ] Hard crash after a write reattaches A and preserves its original boundary
  and paths.
- [ ] `/madeleine rollover` publishes or leaves A pending, then starts B in the
  same Conversation.
- [ ] B writes while an older pending summary retries; the Captures retain
  disjoint structured path sets.
- [ ] Summary failure followed by restart succeeds on automatic retry.
- [ ] Multiple pending Captures recover oldest-first with one model call active.
- [ ] Empty interval and structured edit/write behavior.
- [ ] Shell-only, generated, formatted, human, and other-session changes are not
  attributed without a successful structured mutation event.
- [ ] Missing binary, wrong protocol, SQLite busy, repository-discovery failure,
  missing model, malformed summary, and forced process termination all fail
  open.

## Concurrency verification

- [ ] Add a Go integration test with concurrent exact-path Episode readers,
  Capture path writers, and Episode publishers against one WAL database.
- [ ] Assert no lost writes and no unexpected `SQLITE_BUSY` after bounded retry/
  timeout handling.
- [ ] Record benchmark helpers for 1, 10, 100, and 500 simulated agents, but do
  not make speculative performance thresholds release blockers.
- [ ] Document measured lookup/publication latency and busy count from a local
  representative run in the PR description, not as committed product claims.

## README and operations

- [ ] Replace the placeholder README with a concise explanation of the north
  star, Capture/Episode lifecycle, Pi MVP behavior, and trust/privacy model.
- [ ] Document development installation:

```text
go install github.com/aduverger/madeleine/cmd/madeleine@main
pi install npm:@aduverger/madeleine-pi@0.1.0
```

- [ ] Document release installation using `@v0.1.0`, noting that the tag is
  created after merge rather than by this PR.
- [ ] Document `MADELEINE_HOME`, `MADELEINE_BIN`, database locations,
  `madeleine doctor`, all `/madeleine` commands including `rollover` and
  `retry`, and the Episode/Transcript tools.
- [ ] Explain what is stored: intentionally mutated paths, summaries,
  timestamps, and sanitized cursor-bounded Transcript entries. Original harness
  transcript files and binary/read-output bulk are not copied.
- [ ] Explain crash reattachment, pending-summary retry, explicit
  retry/rollover/abandon, and how to disable or remove the Pi package.
- [ ] State clearly that opaque shell and filesystem changes are not attributed
  in v0.1.
- [ ] Link `design.md` and the completed plan files.
- [ ] Keep future features clearly labeled rather than promising them in v0.1.

## Final CI and smoke tests

- [ ] Run `make check` and `npm run check` on Linux and macOS.
- [ ] Verify `go install` from a clean module cache.
- [ ] Verify `pi install` package discovery from a clean temporary Pi home.
- [ ] Manually run Pi against a disposable Git repository through edit,
  rollover, shutdown, read-context, L2 lookup, compact/raw Transcript lookup,
  reload, and crash/resume scenarios.
- [ ] Run `git diff --check` and confirm no generated DB, npm cache, binary, or
  test repository is tracked.
- [ ] Mark checkboxes in `plan1.md` through `plan12.md` only for work actually
  merged in the stack.

## Plan revisions and decision ledger

Listed least-confident first:

1. The background worker may publish older pending Episodes while the current
   Capture records writes. SQLite WAL and Capture-owned rows make this safe, but
   the end-to-end suite must prove that summary retry does not create write
   starvation.
2. A crash no longer creates a second Capture. Reattaching the same open Capture
   preserves the intended coarse Episode boundary now that downtime filesystem
   changes cannot enter through Git reconciliation.
3. Git remains in the end-to-end harness only because Repository identity is
   Git-based. Dirty-start, staged, commit, and branch-switch attribution tests
   were removed because v0.1 deliberately ignores those unstructured changes.
4. Manual rollover is the explicit way to create multiple Episodes inside one
   long-running Conversation. It shares finalization with shutdown rather than
   introducing a second publication path.
5. This recovery/MVP plan moved from Plan 11 to Plan 12. Persisted bounded
   transcripts and evidence retrieval must land first so end-to-end hardening
   validates the final evidence model rather than transcript-file references.

## MVP acceptance criteria

- [ ] The complete clean-interval, rollover, read-context, L2, and bounded
  Transcript retrieval flow works with real Pi.
- [ ] Crash/resume reattaches one open Capture and preserves its structured paths
  without blocking startup.
- [ ] Pending summaries retry automatically without interfering with the current
  Capture.
- [ ] All failures preserve Pi behavior and recoverable data.
- [ ] Exact-path retrieval remains deterministic and contains no semantic
  search/ranking or inferred filesystem attribution layer.
- [ ] The application, CLI, schema, Pi package, installation, and recovery
  behavior agree with `design.md`.
- [ ] The repository is ready for a post-merge `v0.1.0` tag.

## Excluded from this PR and v0.1

Other harnesses, external transcript backfill, Git/filesystem reconciliation,
Agent Trace, MCP server, folder/symbol/rename memory, unsanitized harness-file
copies, multiplayer UI, daemon, Postgres, network sync, embeddings, and semantic
ranking.
