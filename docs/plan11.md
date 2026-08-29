# Plan 11: Crash recovery, end-to-end hardening, and Pi MVP

PR scope: one PR  
Depends on: `plan10.md`  
Design decisions: D-006, D-014, D-015, D-016, D-019, D-020, D-021, D-023

## Goal

Finish the Pi MVP by recovering interrupted Captures without delaying or
contaminating the new run, validating the real Go/SQLite/Git/TypeScript vertical
slice, and documenting installation and operations.

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
harnesses/pi/*.test.ts
test/e2e/*
README.md
```

## Startup recovery ordering

For `session_start` reasons other than `reload`:

1. Resolve current Repository and Conversation.
2. List its pending Captures.
3. For every `open` stale Capture, seal it using the current transcript leaf
   **before** taking a new Git baseline.
4. Start a new independent Capture for the current runtime.
5. Persist its Pi state entry.
6. Start background retry for frozen `pending_summary` Captures.

- [ ] This ordering is mandatory: it prevents new-run changes from entering the
  old Git diff and prevents old changes from entering the new baseline.
- [ ] If a stale open Capture cannot be sealed, do not reattach it. Start of the
  new Capture returns a visible conflict and Madeleine write capture remains
  disabled for safety; Episode retrieval remains enabled.
- [ ] Empty stale Captures become abandoned and are not queued.
- [ ] Never recover Captures from another Repository or Conversation
  automatically.

## Background worker

- [ ] Maintain one worker per extension runtime and one active summary model
  call at a time.
- [ ] Snapshot the pending queue oldest-first after the new Capture starts.
- [ ] Attempt each queued Capture at most once during that runtime.
- [ ] Reuse Plan 10's projection, model, validation, and publication functions;
  add no second recovery implementation.
- [ ] Continue to the next Capture after a per-Capture validation/publication
  failure, while reporting a concise notification.
- [ ] Associate an AbortController with the worker.
- [ ] On any session shutdown, abort and await worker cleanup before handling
  the current Capture. An aborted old Capture remains pending.
- [ ] Never append recovery prompts or results to the active Conversation.

## Separation invariants

- [ ] Capture A's end cursor is fixed before Capture B's start cursor.
- [ ] Capture A's final paths are frozen before Capture B's Git baseline.
- [ ] Recording B writes cannot update A rows.
- [ ] Publishing A cannot change B's status or raw paths.
- [ ] Injection deduplication is scoped by B's Capture ID, not inherited from A.
- [ ] A repeated crash leaves both Captures independently recoverable.

## End-to-end harness

Build a test harness using:

- the real compiled `madeleine` binary;
- a real temporary SQLite home;
- real temporary Git repositories;
- a fake Pi `ExtensionAPI`, session manager, and deterministic model registry.

Cover:

- [ ] Run A edits two files, quits, publishes one Episode, and Run B reads one
  path and receives A's L1.
- [ ] Run B calls `madeleine_episode` and receives A's L2.
- [ ] `/reload` during A retains the Capture and injects each path once.
- [ ] Hard crash after a write leaves A open; reopened Conversation seals A,
  starts B, and publishes A in the background.
- [ ] B writes while A recovery runs; the Episodes contain disjoint expected
  boundaries and path sets.
- [ ] Summary failure followed by restart succeeds on automatic retry.
- [ ] Multiple pending Captures recover oldest-first with one model call active.
- [ ] Empty run, dirty-at-start repository, shell edit, staged change, untracked
  file, and commit-during-run behavior.
- [ ] Missing binary, wrong protocol, SQLite busy, Git failure, missing model,
  malformed summary, and forced process termination all fail open.

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
  `madeleine doctor`, and `/madeleine` commands.
- [ ] Explain what is stored: paths, summaries, timestamps, and transcript
  references; transcript bodies remain harness-owned.
- [ ] Explain pending recovery, explicit retry/abandon, and how to disable/remove
  the Pi package.
- [ ] Link `design.md` and the completed plan files.
- [ ] Keep future features clearly labeled rather than promising them in v0.1.

## Final CI and smoke tests

- [ ] Run `make check` and `npm run check` on Linux and macOS.
- [ ] Verify `go install` from a clean module cache.
- [ ] Verify `pi install` package discovery from a clean temporary Pi home.
- [ ] Manually run Pi against a disposable Git repository through edit,
  shutdown, read-context, L2 lookup, reload, and crash/resume scenarios.
- [ ] Run `git diff --check` and confirm no generated DB, npm cache, binary, or
  test repository is tracked.
- [ ] Mark checkboxes in `plan1.md` through `plan11.md` only for work actually
  merged in the stack.

## MVP acceptance criteria

- [ ] The complete clean-run, read-context, and L2 flow works with real Pi.
- [ ] Crash/resume produces an independent new Capture and automatically
  retries the old one without blocking startup.
- [ ] All failures preserve Pi behavior and pending recoverable data.
- [ ] Exact-path retrieval remains deterministic and contains no semantic
  search/ranking layer.
- [ ] The application, CLI, schema, Pi package, installation, and recovery behavior
  agree with `design.md`.
- [ ] The repository is ready for a post-merge `v0.1.0` tag.

## Excluded from this PR and v0.1

Other harnesses, transcript backfill, Agent Trace, MCP server, folder/symbol/
rename memory, copied transcripts, multiplayer UI, daemon, Postgres, network
sync, embeddings, and semantic ranking.
