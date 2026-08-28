# Plan 5: Git baseline and final path reconciliation

PR scope: one PR  
Depends on: `plan4.md`  
Design decisions: D-002, D-007, D-009, D-016, D-021

> Historical note: paths and public-package references describe the merged PR.
> Plan 6 later internalizes the Go implementation when Madeleine becomes a
> standalone application.

## Goal

Make Capture final paths authoritative enough for real agent work by combining
structured Pi write evidence with non-mutating Git reconciliation. Preserve
dirty-at-start state correctly and never alter the user's repository.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
git_snapshot.go
git_reconcile.go
capture.go
capture_store.go
internal/gitcmd/gitcmd.go
migrations/004_git_baseline.sql
git_reconcile_test.go
capture_test.go
store_test.go
internal/gitcmd/gitcmd_test.go
episode_store.go
NOTICE
```

## Schema

Extend `captures` with nullable `git_start_head` and a flag recording whether
HEAD existed. Create:

```text
capture_git_baseline_paths
  capture_id TEXT NOT NULL REFERENCES captures(id)
  path TEXT NOT NULL
  porcelain_status TEXT NOT NULL
  worktree_fingerprint TEXT
  index_identity TEXT
  PRIMARY KEY(capture_id, path)
```

The baseline table contains only paths dirty or untracked at Capture start. It
is deleted when the Capture becomes finalized or abandoned.

## Snapshot representation

- [x] Obtain optional HEAD with `git rev-parse --verify HEAD`; unborn HEAD is
  valid and represented explicitly.
- [x] Parse `git status --porcelain=v1 -z --untracked-files=all` without losing
  spaces, Unicode, deletions, or two-path rename/copy records.
- [x] Request no rename interpretation for Madeleine's own comparisons.
- [x] For every dirty/untracked start path, store:
  - its porcelain state;
  - a SHA-256 fingerprint of lstat mode plus regular-file bytes or symlink
    target, with an explicit missing marker;
  - its index mode/blob identity from `git ls-files --stage` when present.
- [x] Do all Git/filesystem work before the Store transaction that saves the
  baseline.
- [x] Add the baseline atomically with `StartCapture`; if persistence fails,
  remove the just-created Capture rather than leaving it without a baseline.

## End reconciliation

- [x] Capture the end HEAD and end porcelain state using the same parser.
- [x] If both start and end HEAD exist and differ, use
  `git diff --name-only -z --no-renames <start>..<end>`.
- [x] If exactly one HEAD is unborn, treat it as an empty committed tree and
  enumerate the existing HEAD with read-only `git ls-tree`; if both are unborn,
  rely on status/baseline comparison.
- [x] Include every path newly present in end status.
- [x] For paths dirty at start, include the path when porcelain state,
  worktree fingerprint, or index identity differs at seal.
- [x] Include a dirty-start path that became clean, missing, staged, or
  otherwise different from its start snapshot.
- [x] Normalize every Git path through the same repository path function used
  by structured writes.
- [x] Union Git-derived paths with existing `capture_paths`; set source `git`
  only for paths not already sourced by a tool.
- [x] Sort the final set lexically before `SealCapture` freezes it.

## State and failure behavior

- [x] Run reconciliation only while the Capture is `open`.
- [x] Do not change Capture state when Git discovery, parsing, hashing, or
  persistence fails; return the error so recovery can retry later.
- [x] Repeated sealing of `pending_summary` must not rerun Git or change paths.
- [x] Delete baseline rows on successful publication or abandonment.
- [x] Never run `git add`, `git update-index`, `git stash`, `git checkout`,
  `git reset`, `git commit`, or commands that write Git objects/refs.

## Integration tests

Use real temporary Git repositories and assert both Madeleine results and that
Git status/index/HEAD are unchanged by observation.

- [x] Clean tracked file modified by a shell command.
- [x] New untracked file and deleted tracked file.
- [x] Staged-only change and staged change followed by worktree change.
- [x] File dirty before Capture and modified again during Capture.
- [x] File dirty before Capture and restored to HEAD during Capture.
- [x] Commit created during Capture with a clean worktree at seal.
- [x] Several commits and a changed end branch.
- [x] Unborn repository with untracked files.
- [x] First commit created from an unborn start with a clean worktree at seal.
- [x] Populated start HEAD changed to a clean unborn/orphan branch.
- [x] Rename represented as old-path deletion plus new-path addition.
- [x] Structured write that Git also sees appears only once and keeps tool
  provenance.
- [x] Structured write fully reverted before seal remains included.
- [x] Shell-only change fully reverted before seal is absent by design.
- [x] Filenames containing spaces, newlines, Unicode, and leading dashes.
- [x] Git failure leaves the Capture open and recoverable.

## Implementation assumptions, plan changes, and upstream provenance

Listed least-confident first:

1. Migration 4 does not backfill a trustworthy Git baseline for `open`
   Captures created by an older binary. The core API has not shipped, and a
   synthetic baseline would silently misattribute pre-upgrade changes; this PR
   therefore relies on the repository rule that unshipped persistence does not
   receive compatibility scaffolding.
2. Each Git observation command has a 20-second timeout, matching Entire's
   status-walk budget. A timeout returns an error and leaves the Capture open;
   the core does not silently publish an incomplete path set.
3. Optional HEAD uses `git rev-parse --verify --quiet HEAD`. Exit status 1 is
   the explicit unborn representation; every other failure remains an error.
4. Worktree fingerprints hash tagged presence state, numeric `lstat` mode, and
   regular-file bytes or symlink target. Other filesystem object types retain
   presence and mode identity without reading their contents.
5. Index identity stores the stable `git ls-files --stage` prefix (`mode blob
   stage`). Multiple unmerged stages are sorted and newline-joined so conflict
   state compares deterministically without adding another table.
6. The porcelain parser preserves both paths of rename/copy records with their
   original XY state. Madeleine invokes status with `--no-renames`, so its own
   snapshots normally receive deletion/addition records instead.
7. `git ls-files --stage -z` reads the whole index only when at least one path
   needs a snapshot. This avoids command-line path limits and keeps the
   implementation simpler than batching arbitrary pathspecs.
8. `capture_test.go`, `store_test.go`, `internal/gitcmd/gitcmd_test.go`,
   `episode_store.go`, and `NOTICE` were added to the plan's file list to update
   existing behavior/schema expectations, remove baselines during publication,
   pin inherited Git-environment isolation, and retain upstream attribution.
9. Entire CLI was inspected at commit
   `20dc86ea5e48dc9778c215277f4a35101274cda7`. Compatible mechanics were adapted
   from `cmd/entire/cli/checkpoint/ephemeral.go`,
   `collect_changed_files_index_test.go`, `cmd/entire/cli/gitrepo/env.go`, and
   `cmd/entire/cli/git_operations.go`: NUL-delimited porcelain status, full
   untracked enumeration, `--no-optional-locks`, and removal of inherited
   `GIT_DIR`, `GIT_WORK_TREE`, and `GIT_INDEX_FILE`. Entire captures checkpoint
   contents rather than comparing a durable dirty-at-start baseline and has no
   equivalent SQLite transaction, so its checkpoint store, go-git status walk,
   and object-writing code were not reused. `NOTICE` now records this commit and
   retains Entire's MIT license text.
10. Review clarified the one-HEAD case: the missing HEAD is an empty committed
    tree, so the existing endpoint's paths are included with `git ls-tree`.
    This symmetric rule covers both a normal first commit and the rarer move to
    a clean unborn/orphan branch without special lifecycle handling.
11. Local verification passed with `make check`, `go test -race ./...`, and
    `git diff --check`. The integration suite compares HEAD, porcelain status,
    index bytes, index file identity, and index modification time immediately
    before and after Git observation.

## Acceptance criteria

- [x] Final paths are the deterministic union of structured writes and Git
  changes since the Capture baseline.
- [x] A dirty-at-start file is included only when its end state differs from its
  captured start state or a structured write recorded it.
- [x] Madeleine does not mutate any observable Git state.
- [x] All previous Capture/Episode idempotency tests remain green.

## Excluded from this PR

Logical rename identity, line attribution, filesystem watching, commit
association metadata, CLI transport, and Pi hooks.
