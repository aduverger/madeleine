# Plan 2: SQLite Store and persistent repository identity

PR scope: one PR  
Depends on: `plan1.md`  
Design decisions: D-005, D-008, D-010, D-016, D-019, D-020, D-021

## Goal

Implement local SQLite lifecycle, migrations, data-directory policy, and the
canonical persistence of Repositories, aliases, and Conversations. No Capture or
Episode tables are added yet.

## Entire reuse gate

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
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

- [ ] Define `Options` with an optional `Home string`.
- [ ] Implement `Open(ctx, Options) (*Store, error)`.
- [ ] Implement idempotent `(*Store).Close() error`.
- [ ] Resolve an empty `Options.Home` using:
  - macOS: `~/Library/Application Support/madeleine`;
  - Linux: `$XDG_DATA_HOME/madeleine`, otherwise `~/.local/share/madeleine`.
- [ ] Let the CLI supply `MADELEINE_HOME`; keep environment lookup out of the
  reusable Store.
- [ ] Create the directory with mode `0700` where supported and use
  `<home>/madeleine.db`.
- [ ] Do not silently fall back to another path when directory creation fails.

## Connection behavior

- [ ] Enable WAL journal mode, foreign keys, and a 5000 ms busy timeout for
  every connection.
- [ ] Bound the connection pool and ensure per-connection pragmas cannot be
  skipped by a newly opened pooled connection.
- [ ] Verify settings during `Open` and fail visibly if WAL or foreign keys
  cannot be enabled.
- [ ] Keep transactions short; do not hold a transaction while invoking Git or
  doing filesystem work.
- [ ] Wrap `SQLITE_BUSY` and constraint failures with operation context.

## Migration mechanism

- [ ] Embed numbered `.sql` files with `go:embed`.
- [ ] Add `schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT)`.
- [ ] Sort migrations numerically and apply each once in its own transaction.
- [ ] Take an immediate write lock while checking/applying a migration so two
  processes cannot race the same version.
- [ ] Reject a database containing a migration version newer than the binary.
- [ ] Roll back the whole migration on any statement failure.

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

- [ ] Add foreign-key indexes needed for Repository and Conversation joins.
- [ ] Store all timestamps as UTC RFC3339Nano text.

## Repository matching

- [ ] Resolve current Git facts outside a database transaction.
- [ ] Match aliases in priority order: common Git directory, worktree root,
  normalized origin.
- [ ] When exactly one Repository matches, attach any new non-conflicting
  aliases to it in one transaction.
- [ ] When none match, generate a Repository ID and insert it with all aliases.
- [ ] When aliases point to different existing Repositories, return
  `ErrConflict`; never merge silently.
- [ ] Return the current resolved root in the public `Repository`, even when it
  differs from the first root ever seen.

## Conversation persistence

- [ ] Add an internal get-or-create operation using Repository ID plus
  `ConversationKey`.
- [ ] Update a non-empty transcript reference and `updated_at` on reuse.
- [ ] Reject empty harness or external ID with `ErrInvalidState`.
- [ ] Do not expose harness-specific parsing in the Store.

## Tests

- [ ] Open and migrate an empty temporary home.
- [ ] Reopen an already migrated database without changing rows.
- [ ] Race two Store processes/connections applying the first migration.
- [ ] Inject a failing migration in a test fixture and assert rollback.
- [ ] Reject a fixture with a future schema version.
- [ ] Verify WAL, foreign keys, and busy timeout on more than one connection.
- [ ] Test matching by common directory, root, and origin in priority order.
- [ ] Test new clone alias attachment and explicit conflicting aliases.
- [ ] Test concurrent get-or-create of the same Repository and Conversation.
- [ ] Test no-origin repositories and transcript-reference updates.

## Acceptance criteria

- [ ] Deleting the temporary DB and reopening produces the expected schema.
- [ ] Multiple short-lived processes can concurrently resolve the same
  Repository without creating duplicates.
- [ ] The database contains no Capture, Episode, transcript body, or JSON blob.
- [ ] `go test ./...`, `go vet ./...`, and both CI platforms pass.

## Excluded from this PR

Capture state, path activity, Episode history, Git baselines, RPC, and Pi.
