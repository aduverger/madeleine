# Plan 5: Git baseline and final path reconciliation

PR scope: one PR  
Depends on: `plan4.md`  
Design decisions: D-002, D-007, D-009, D-016

## Goal

Make Capture final paths authoritative enough for real agent work by combining
structured Pi write evidence with non-mutating Git reconciliation. Preserve
dirty-at-start state correctly and never alter the user's repository.

## Files

```text
git_snapshot.go
git_reconcile.go
capture.go
capture_store.go
internal/gitcmd/gitcmd.go
migrations/004_git_baseline.sql
git_reconcile_test.go
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

- [ ] Obtain optional HEAD with `git rev-parse --verify HEAD`; unborn HEAD is
  valid and represented explicitly.
- [ ] Parse `git status --porcelain=v1 -z --untracked-files=all` without losing
  spaces, Unicode, deletions, or two-path rename/copy records.
- [ ] Request no rename interpretation for Madeleine's own comparisons.
- [ ] For every dirty/untracked start path, store:
  - its porcelain state;
  - a SHA-256 fingerprint of lstat mode plus regular-file bytes or symlink
    target, with an explicit missing marker;
  - its index mode/blob identity from `git ls-files --stage` when present.
- [ ] Do all Git/filesystem work before the Store transaction that saves the
  baseline.
- [ ] Add the baseline atomically with `StartCapture`; if persistence fails,
  remove the just-created Capture rather than leaving it without a baseline.

## End reconciliation

- [ ] Capture the end HEAD and end porcelain state using the same parser.
- [ ] If both start and end HEAD exist and differ, use
  `git diff --name-only -z --no-renames <start>..<end>`.
- [ ] If either HEAD is unborn, rely on status/baseline comparison rather than
  inventing a commit range.
- [ ] Include every path newly present in end status.
- [ ] For paths dirty at start, include the path when porcelain state,
  worktree fingerprint, or index identity differs at seal.
- [ ] Include a dirty-start path that became clean, missing, staged, or
  otherwise different from its start snapshot.
- [ ] Normalize every Git path through the same repository path function used
  by structured writes.
- [ ] Union Git-derived paths with existing `capture_paths`; set source `git`
  only for paths not already sourced by a tool.
- [ ] Sort the final set lexically before `SealCapture` freezes it.

## State and failure behavior

- [ ] Run reconciliation only while the Capture is `open`.
- [ ] Do not change Capture state when Git discovery, parsing, hashing, or
  persistence fails; return the error so recovery can retry later.
- [ ] Repeated sealing of `pending_summary` must not rerun Git or change paths.
- [ ] Delete baseline rows on successful publication or abandonment.
- [ ] Never run `git add`, `git update-index`, `git stash`, `git checkout`,
  `git reset`, `git commit`, or commands that write Git objects/refs.

## Integration tests

Use real temporary Git repositories and assert both Madeleine results and that
Git status/index/HEAD are unchanged by observation.

- [ ] Clean tracked file modified by a shell command.
- [ ] New untracked file and deleted tracked file.
- [ ] Staged-only change and staged change followed by worktree change.
- [ ] File dirty before Capture and modified again during Capture.
- [ ] File dirty before Capture and restored to HEAD during Capture.
- [ ] Commit created during Capture with a clean worktree at seal.
- [ ] Several commits and a changed end branch.
- [ ] Unborn repository with untracked files.
- [ ] Rename represented as old-path deletion plus new-path addition.
- [ ] Structured write that Git also sees appears only once and keeps tool
  provenance.
- [ ] Structured write fully reverted before seal remains included.
- [ ] Shell-only change fully reverted before seal is absent by design.
- [ ] Filenames containing spaces, newlines, Unicode, and leading dashes.
- [ ] Git failure leaves the Capture open and recoverable.

## Acceptance criteria

- [ ] Final paths are the deterministic union of structured writes and Git
  changes since the Capture baseline.
- [ ] A dirty-at-start file is included only when its end state differs from its
  captured start state or a structured write recorded it.
- [ ] Madeleine does not mutate any observable Git state.
- [ ] All previous Capture/Episode idempotency tests remain green.

## Excluded from this PR

Logical rename identity, line attribution, filesystem watching, commit
association metadata, CLI transport, and Pi hooks.
