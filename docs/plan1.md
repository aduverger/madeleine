# Plan 1: Go foundation and repository identity

PR scope: one PR  
Depends on: nothing  
Design decisions: D-001, D-002, D-010, D-016, D-020, D-021

## Goal

Establish the Go module, contributor checks, domain vocabulary, and authoritative
Git/path resolution used by every later PR. This PR does not open a database or
ship a functional CLI.

## Entire reuse gate

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

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

- [ ] Add opaque string types: `RepositoryID`, `ConversationID`, `CaptureID`,
  and `EpisodeID`.
- [ ] Add `Harness string` and reserve `HarnessPi = "pi"`.
- [ ] Add `Repository` with ID, resolved worktree root, Git common directory,
  and optional normalized origin.
- [ ] Add `ConversationKey` with `Harness` and `ExternalID`.
- [ ] Add `CaptureStatus` values `open`, `pending_summary`, `finalized`, and
  `abandoned`.
- [ ] Add the final request/result structs named in `design.md` so subsequent
  PRs extend behavior without renaming the public contract.
- [ ] Represent timestamps as `time.Time` in Go and UTC RFC3339Nano at the JSON
  boundary.
- [ ] Use generated UUIDv7 strings for repository, conversation, Capture, and
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

- [ ] Define `ErrNotFound`, `ErrConflict`, `ErrInvalidState`,
  `ErrNotGitRepository`, and `ErrOutsideRepository` as sentinel errors.
- [ ] Wrap errors with operation and relevant opaque ID/path while preserving
  `errors.Is` behavior.
- [ ] Never include transcript text, credentials, environment contents, or
  SQLite DSNs in errors.

## Git command helper

- [ ] Execute Git directly with `exec.CommandContext`; never invoke a shell.
- [ ] Accept an explicit working directory and argument slice.
- [ ] Capture stdout and bounded stderr separately.
- [ ] Apply a five-second timeout to discovery commands.
- [ ] Return a typed command error containing exit status and sanitized stderr.
- [ ] Add no `go-git`, libgit2, or Git configuration mutation.

## Repository resolution

- [ ] Implement `ResolveRepository(ctx, path)` using the nearest Git worktree.
- [ ] Resolve the canonical worktree root with `git rev-parse --show-toplevel`.
- [ ] Resolve and absolutize `git rev-parse --git-common-dir` relative to the
  command working directory when Git returns a relative path.
- [ ] Read `remote.origin.url` when present; a missing origin is valid.
- [ ] Normalize origins by parsing SCP-like and URL-like forms, removing
  transport/user information and a trailing `.git`, lowercasing the host, and
  preserving repository path case.
- [ ] Clean roots with `filepath.Abs` and `filepath.EvalSymlinks`; do not resolve
  individual file symlinks when forming file identity.
- [ ] Return `ErrNotGitRepository` for directories outside Git.

Equivalent origins that must normalize identically:

```text
git@github.com:aduverger/madeleine.git
ssh://git@github.com/aduverger/madeleine.git
https://github.com/aduverger/madeleine.git
```

## Path normalization

- [ ] Accept an absolute path or a path relative to the Repository worktree.
- [ ] Convert it to a cleaned, slash-separated, case-preserving relative path.
- [ ] Reject the root itself, an empty path, and any result equal to `..` or
  beginning with `../`.
- [ ] Preserve a symlink's path inside the repository instead of replacing it
  with the target path.
- [ ] Do not query Git or the filesystem to change case.

## Project tooling

- [ ] Initialize `go.mod` as `github.com/aduverger/madeleine` with
  `go 1.26.0`; do not add an explicit `toolchain` directive.
- [ ] Commit `go.sum` and use Go modules as the only Go dependency manager.
- [ ] Add Make targets: `fmt-check`, `test`, `vet`, `build`, and `check`.
- [ ] Make `check` run formatting verification, `go test ./...`,
  `go vet ./...`, and `go build ./...` without rewriting source.
- [ ] Add GitHub Actions for `ubuntu-latest` and `macos-latest` with Go 1.26.x.
- [ ] Leave the existing Apache-2.0 license unchanged.

## Tests

- [ ] Table-test origin normalization, including HTTPS, SSH, SCP syntax,
  missing origin, mixed host case, trailing slash, and trailing `.git`.
- [ ] Create temporary normal repositories and linked worktrees and assert
  stable root/common-directory discovery.
- [ ] Test repositories with no origin and paths containing spaces.
- [ ] Test relative, absolute, nested, case-preserving, symlink, empty, root,
  and outside-repository paths.
- [ ] Test Git timeout and sanitized command errors with a fake executable.
- [ ] Run the full `make check` target on both CI platforms.

## Acceptance criteria

- [ ] Another package can import `github.com/aduverger/madeleine` and use the
  declared public types without importing an internal package.
- [ ] Equivalent repository origins normalize consistently.
- [ ] No function accepts a path that escapes the resolved worktree.
- [ ] The repository remains free of runtime services, database code, and Pi
  integration.

## Excluded from this PR

SQLite, persistent repository matching, Capture behavior, Episode behavior,
Git change reconciliation, the CLI, and Pi integration.
