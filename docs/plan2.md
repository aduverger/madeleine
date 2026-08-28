# Plan 2: SQLite Store and persistent repository identity

PR scope: one PR  
Depends on: `plan1.md`  
Design decisions: D-005, D-008, D-010, D-016, D-019, D-020, D-021

> Historical note: paths and public-package references describe the merged PR.
> Plan 6 later internalizes the Go implementation when Madeleine becomes a
> standalone application.

## Goal

Implement local SQLite lifecycle, migrations, data-directory policy, and the
canonical persistence of Repositories, aliases, and Conversations. No Capture or
Episode tables are added yet.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
go.mod
go.sum
api_external_test.go
store.go
store_migrations.go
repository_store.go
conversation_store.go
migrations/001_store.sql
store_test.go
```

Use `database/sql` with the blank-imported `modernc.org/sqlite` driver. Do not
introduce an ORM, query builder, repository interface, or alternate backend.

## Store API

- [x] Define `Options` with an optional `Home string`.
- [x] Implement `Open(ctx, Options) (*Store, error)`.
- [x] Implement idempotent `(*Store).Close() error`.
- [x] Resolve an empty `Options.Home` using:
  - macOS: `~/Library/Application Support/madeleine`;
  - Linux: `$XDG_DATA_HOME/madeleine`, otherwise `~/.local/share/madeleine`.
- [x] Let the CLI supply `MADELEINE_HOME`; keep environment lookup out of the
  reusable Store.
- [x] Create the directory with mode `0700` where supported and use
  `<home>/madeleine.db`.
- [x] Do not silently fall back to another path when directory creation fails.

## Connection behavior

- [x] Enable WAL journal mode, foreign keys, and a 5000 ms busy timeout for
  every connection.
- [x] Bound the connection pool and ensure per-connection pragmas cannot be
  skipped by a newly opened pooled connection.
- [x] Verify settings during `Open` and fail visibly if WAL or foreign keys
  cannot be enabled.
- [x] Keep transactions short; do not hold a transaction while invoking Git or
  doing filesystem work.
- [x] Wrap `SQLITE_BUSY` and constraint failures with operation context.

## Migration mechanism

- [x] Embed numbered `.sql` files with `go:embed`.
- [x] Add `schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT)`.
- [x] Sort migrations numerically and apply each once in its own transaction.
- [x] Take an immediate write lock while checking/applying a migration so two
  processes cannot race the same version.
- [x] Reject a database containing a migration version newer than the binary.
- [x] Roll back the whole migration on any statement failure.

## Initial schema

`migrations/001_store.sql` must create:

```text
repositories
  id TEXT PRIMARY KEY
  created_at TEXT NOT NULL

repository_aliases
  repository_id TEXT NOT NULL REFERENCES repositories(id)
  kind TEXT NOT NULL                 -- common_git_dir | worktree_root | origin
  value TEXT NOT NULL
  created_at TEXT NOT NULL
  PRIMARY KEY(kind, value)

conversations
  id TEXT PRIMARY KEY
  repository_id TEXT NOT NULL REFERENCES repositories(id)
  harness TEXT NOT NULL
  external_id TEXT NOT NULL
  transcript_ref TEXT
  created_at TEXT NOT NULL
  updated_at TEXT NOT NULL
  UNIQUE(repository_id, harness, external_id)
```

- [x] Add foreign-key indexes needed for Repository and Conversation joins.
- [x] Store all timestamps as UTC RFC3339Nano text.

## Repository matching

- [x] Resolve current Git facts outside a database transaction.
- [x] Match aliases in priority order: common Git directory, worktree root,
  normalized origin.
- [x] When exactly one Repository matches, attach any new non-conflicting
  aliases to it in one transaction.
- [x] When none match, generate a Repository ID and insert it with all aliases.
- [x] When aliases point to different existing Repositories, return
  `ErrConflict`; never merge silently.
- [x] Return the current resolved root in the public `Repository`, even when it
  differs from the first root ever seen.

## Conversation persistence

- [x] Add an internal get-or-create operation using Repository ID plus
  `ConversationKey`.
- [x] Update a non-empty transcript reference and `updated_at` on reuse.
- [x] Reject empty harness or external ID with `ErrInvalidState`.
- [x] Do not expose harness-specific parsing in the Store.

## Tests

- [x] Open and migrate an empty temporary home.
- [x] Reopen an already migrated database without changing rows.
- [x] Race two Store processes/connections applying the first migration.
- [x] Inject a failing migration in a test fixture and assert rollback.
- [x] Reject a fixture with a future schema version.
- [x] Verify WAL, foreign keys, and busy timeout on more than one connection.
- [x] Test matching by common directory, root, and origin in priority order.
- [x] Test new clone alias attachment and explicit conflicting aliases.
- [x] Test concurrent get-or-create of the same Repository and Conversation.
- [x] Test no-origin repositories and transcript-reference updates.

## Implementation notes, assumptions, and deviations

- The plan required a bounded pool but did not choose a size. The Store uses a
  fixed maximum of four open and four idle connections; this is deliberately
  small for short-lived local operations and is not exposed as configuration.
- "Empty" Conversation key fields means exactly the empty string. Harness and
  external IDs are opaque and are not trimmed or otherwise rewritten.
- An empty `Options.Home` fails explicitly on operating systems other than
  macOS and Linux. Those are the only platforms accepted by D-020; no fallback
  location is invented.
- Plan 1 already defined `Options` and package-level `ResolveRepository` for Git
  discovery. Plan 2 preserves that function and adds
  `(*Store).ResolveRepository` for canonical matching and persistence. The
  Conversation get-or-create operation remains unexported.
- WAL is database-persistent rather than connection-local. Applying
  `journal_mode(WAL)` as a DSN pragma on every connection caused fresh-database
  startup races between short-lived processes, so `Open` enables it once with a
  context-bounded retry and verifies it. The connection-local foreign-key and
  5000 ms busy-timeout pragmas remain in the DSN and therefore apply to every
  pooled connection; tests hold four distinct connections and verify all three
  settings on each.
- SQLite uses `modernc.org/sqlite` v1.47.0 through its blank-imported
  `database/sql` driver. `go.mod`, `go.sum`, and `api_external_test.go` were
  added to the plan's file list because the driver dependency and public Store
  contract cannot be implemented or checked without those existing files.
- Entire was inspected at commit
  `6561a64b80b862c3fc63108ece4064ee6ebe8cff`. Relevant upstream paths were
  `cmd/entire/cli/checkpoint/store.go`,
  `cmd/entire/cli/checkpoint/migrate.go` and its tests,
  `cmd/entire/cli/checkpoint/fsstore/fsstore.go` and its tests, and
  `cmd/entire/cli/paths/paths.go` and its tests. Entire had no SQLite driver,
  numbered SQL migrations, user-global database lifecycle, or equivalent
  Repository/Conversation schema at that commit; its `go.mod` contained no
  SQLite dependency. Reusing its Git-backed or JSON-file checkpoint stores
  would violate D-008 and Madeleine's schema. Madeleine therefore uses fresh
  SQLite code while retaining Entire's applicable idempotent migration and
  atomic write-boundary mechanics. The existing `NOTICE` already attributes
  mechanics adapted from this commit.
- Local checks passed on macOS: `make check`, `go test -race ./...`, repeated
  concurrent migration/process tests, and `git diff --check`. The CI acceptance
  checkbox remains open until both configured GitHub Actions platforms run.

## Acceptance criteria

- [x] Deleting the temporary DB and reopening produces the expected schema.
- [x] Multiple short-lived processes can concurrently resolve the same
  Repository without creating duplicates.
- [x] The database contains no Capture, Episode, transcript body, or JSON blob.
- [ ] `go test ./...`, `go vet ./...`, and both CI platforms pass.

## Excluded from this PR

Capture state, path activity, Episode history, Git baselines, RPC, and Pi.
