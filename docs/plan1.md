# Plan 1: Go foundation and repository identity

PR scope: one PR  
Depends on: nothing  
Design decisions: D-001, D-002, D-010, D-016, D-020, D-021

> Historical note: paths and public-package references describe the merged PR.
> Plan 6 later internalizes the Go implementation when Madeleine becomes a
> standalone application.

## Goal

Establish the Go module, contributor checks, domain vocabulary, and authoritative
Git/path resolution used by every later PR. This PR does not open a database or
ship a functional CLI.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files and packages

```text
go.mod
go.sum
Makefile
.github/workflows/ci.yml
types.go
errors.go
repository.go
path.go
internal/gitcmd/gitcmd.go
*_test.go
```

The public package is the module-root package `madeleine`. `internal/gitcmd`
only owns bounded subprocess mechanics and returns low-level strings/bytes; it
must not duplicate Madeleine domain rules.

## Public domain types

- [x] Add opaque string types: `RepositoryID`, `ConversationID`, `CaptureID`,
  and `EpisodeID`.
- [x] Add `Harness string` and reserve `HarnessPi = "pi"`.
- [x] Add `Repository` with ID, resolved worktree root, Git common directory,
  and optional normalized origin.
- [x] Add `ConversationKey` with `Harness` and `ExternalID`.
- [x] Add `CaptureStatus` values `open`, `pending_summary`, `finalized`, and
  `abandoned`.
- [x] Add the final request/result structs named in `design.md` so subsequent
  PRs extend behavior without renaming the public contract.
- [x] Represent timestamps as `time.Time` in Go and UTC RFC3339Nano at the JSON
  boundary.
- [x] Use generated UUIDv7 strings for repository, conversation, Capture, and
  Episode IDs without exposing the UUID library in public field types.

Required request fields:

```text
StartCaptureRequest:
  RepositoryRoot, ConversationKey, TranscriptRef, StartCursor

RecordWriteRequest:
  CaptureID, Path

PendingCaptureQuery:
  RepositoryRoot, optional ConversationKey

SealCaptureRequest:
  CaptureID, EndCursor

PublishEpisodeRequest:
  CaptureID, L1, L2

ContextRequest:
  RepositoryRoot, Paths

EpisodeRequest:
  RepositoryRoot, EpisodeID
```

## Errors

- [x] Define `ErrNotFound`, `ErrConflict`, `ErrInvalidState`,
  `ErrNotGitRepository`, and `ErrOutsideRepository` as sentinel errors.
- [x] Wrap errors with operation and relevant opaque ID/path while preserving
  `errors.Is` behavior.
- [x] Never include transcript text, credentials, environment contents, or
  SQLite DSNs in errors.

## Git command helper

- [x] Execute Git directly with `exec.CommandContext`; never invoke a shell.
- [x] Accept an explicit working directory and argument slice.
- [x] Capture stdout and bounded stderr separately.
- [x] Apply a five-second timeout to discovery commands.
- [x] Return a typed command error containing exit status and sanitized stderr.
- [x] Add no `go-git`, libgit2, or Git configuration mutation.

## Repository resolution

- [x] Implement `ResolveRepository(ctx, path)` using the nearest Git worktree.
- [x] Resolve the canonical worktree root with `git rev-parse --show-toplevel`.
- [x] Resolve and absolutize `git rev-parse --git-common-dir` relative to the
  command working directory when Git returns a relative path.
- [x] Read `remote.origin.url` when present; a missing origin is valid.
- [x] Normalize origins by parsing SCP-like and URL-like forms, removing
  transport/user information and a trailing `.git`, lowercasing the host, and
  preserving repository path case.
- [x] Clean roots with `filepath.Abs` and `filepath.EvalSymlinks`; do not resolve
  individual file symlinks when forming file identity.
- [x] Return `ErrNotGitRepository` for directories outside Git.

Equivalent origins that must normalize identically:

```text
git@github.com:aduverger/madeleine.git
ssh://git@github.com/aduverger/madeleine.git
https://github.com/aduverger/madeleine.git
```

## Path normalization

- [x] Accept an absolute path or a path relative to the Repository worktree.
- [x] Convert it to a cleaned, slash-separated, case-preserving relative path.
- [x] Reject the root itself, an empty path, and any result equal to `..` or
  beginning with `../`.
- [x] Preserve a symlink's path inside the repository instead of replacing it
  with the target path.
- [x] Do not query Git or the filesystem to change case.

## Project tooling

- [x] Initialize `go.mod` as `github.com/aduverger/madeleine` with
  `go 1.26.0`; do not add an explicit `toolchain` directive.
- [x] Commit `go.sum` and use Go modules as the only Go dependency manager.
- [x] Add Make targets: `fmt-check`, `test`, `vet`, `build`, and `check`.
- [x] Make `check` run formatting verification, `go test ./...`,
  `go vet ./...`, and `go build ./...` without rewriting source.
- [x] Add GitHub Actions for `ubuntu-latest` and `macos-latest` with Go 1.26.x.
- [x] Leave the existing Apache-2.0 license unchanged.

## Tests

- [x] Table-test origin normalization, including HTTPS, SSH, SCP syntax,
  missing origin, mixed host case, trailing slash, and trailing `.git`.
- [x] Create temporary normal repositories and linked worktrees and assert
  stable root/common-directory discovery.
- [x] Test repositories with no origin and paths containing spaces.
- [x] Test relative, absolute, nested, case-preserving, symlink, empty, root,
  and outside-repository paths.
- [x] Test Git timeout and sanitized command errors with a fake executable.
- [ ] Run the full `make check` target on both CI platforms.

## Implementation notes, assumptions, and deviations

- Entire was inspected at commit
  `6561a64b80b862c3fc63108ece4064ee6ebe8cff`. Relevant upstream files were
  `cmd/entire/cli/gitexec/gitexec.go`, `cmd/entire/cli/paths/paths.go`,
  `cmd/entire/cli/paths/paths_test.go`,
  `cmd/entire/cli/checkpoint/git_common_dir.go`,
  `cmd/entire/cli/checkpoint/git_common_dir_test.go`,
  `cmd/entire/cli/session/state.go`, and
  `cmd/entire/cli/gitremote/gitremote.go` with its tests.
- Madeleine adapts Entire's direct argument-vector execution, separate stderr
  capture, relative Git-common-directory resolution, and lexical traversal
  checks. It does not reuse Entire's process-wide caches, `go-git` repository
  layer, GitHub owner/repository projection, or unbounded command errors because
  those conflict with this plan's explicit-path, bounded-error, dependency, and
  origin-identity requirements.
- `NOTICE` is added beyond the originally listed files to retain the upstream
  MIT attribution while leaving Madeleine's Apache-2.0 `LICENSE` unchanged.
- `ResolveRepository` returns repository facts with a zero `Repository.ID` in
  this foundation. Allocating a stable ID is deferred to Plan 2's canonical
  persistence transaction; generating a new ID during every stateless resolve
  would imply false stability.
- Optional origin, transcript reference, cursor, and opaque-ID fields use the
  empty string. Optional timestamps and `PendingCaptureQuery.ConversationKey`
  use pointers. Result structs include the fields required by Plans 2-10, but
  no Store behavior is introduced here.
- Non-network origins, including local paths and `file://` URLs, have no
  normalized origin alias. This keeps valid local Git repositories resolvable
  without inventing a cross-machine origin identity.
- UUIDv7 generation uses `github.com/google/uuid`; public fields remain the
  opaque Madeleine string types.
- Public timestamps remain `time.Time`. Plan 2's Store owns timestamp creation
  and will assign UTC values; Go's standard JSON encoding then emits
  RFC3339Nano. No parallel timestamp wrapper type is introduced.
- `make check`, `go test -race ./...`, and `git diff --check` pass locally on
  macOS. The cross-platform test checkbox remains open until the configured
  GitHub Actions job runs on both macOS and Ubuntu.

## Acceptance criteria

- [x] Another package can import `github.com/aduverger/madeleine` and use the
  declared public types without importing an internal package.
- [x] Equivalent repository origins normalize consistently.
- [x] No function accepts a path that escapes the resolved worktree.
- [x] The repository remains free of runtime services, database code, and Pi
  integration.

## Excluded from this PR

SQLite, persistent repository matching, Capture behavior, Episode behavior,
Git change reconciliation, the CLI, and Pi integration.
