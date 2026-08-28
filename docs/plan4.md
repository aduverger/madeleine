# Plan 4: Episode publication and exact-path retrieval

PR scope: one PR  
Depends on: `plan3.md`  
Design decisions: D-001, D-002, D-003, D-004, D-005, D-012, D-021

## Goal

Publish a sealed Capture as an immutable Episode and implement the two core
read questions: "which Episodes changed this path?" and "what is the full L2
for this Episode?"

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
episode.go
episode_store.go
context.go
migrations/003_episodes.sql
episode_test.go
context_test.go
api_external_test.go
store_test.go
```

## Schema

Create:

```text
episodes
  id TEXT PRIMARY KEY
  source_capture_id TEXT NOT NULL UNIQUE REFERENCES captures(id)
  conversation_id TEXT NOT NULL REFERENCES conversations(id)
  repository_id TEXT NOT NULL REFERENCES repositories(id)
  harness TEXT NOT NULL
  started_at TEXT NOT NULL
  ended_at TEXT NOT NULL
  l1 TEXT NOT NULL
  l2 TEXT NOT NULL
  transcript_ref TEXT
  start_cursor TEXT
  end_cursor TEXT
  created_at TEXT NOT NULL

episode_files
  episode_id TEXT NOT NULL REFERENCES episodes(id)
  repository_id TEXT NOT NULL REFERENCES repositories(id)
  path TEXT NOT NULL
  PRIMARY KEY(episode_id, path)
```

- [x] Add an index on `(repository_id, path, episode_id)` and an Episode time
  index supporting newest-first joins.
- [x] Do not add summary JSON, embeddings, FTS, directory rows, or line data.

## Publish Episode

- [x] Require the Capture to be `pending_summary` and to contain at least one
  frozen path.
- [x] Validate trimmed non-empty L1/L2 and reject L1 over 400 Unicode
  characters.
- [x] Generate one Episode ID and copy authoritative repository, Conversation,
  harness, timestamps, transcript reference, and cursor boundaries from the
  Capture.
- [x] In one transaction, insert the Episode, batch-insert its exact paths,
  mark the Capture `finalized` with the Episode ID, and delete raw Capture path
  rows.
- [x] Repeating the identical publication returns the existing Episode.
- [x] Repeating with a different L1 or L2 returns `ErrConflict`.
- [x] Never partially expose an Episode if any insert/update fails.

## Context lookup

- [x] `ContextForPaths` resolves the current Repository and normalizes every
  requested path.
- [x] Deduplicate input paths while preserving caller order.
- [x] Return one `FileContext` per normalized input path.
- [x] For each path, return at most five Episode summaries ordered by
  `ended_at DESC, id DESC`.
- [x] Each summary includes Episode ID, ended time, harness, and L1.
- [x] Return an empty Episode slice for a valid path with no matching Episodes.
- [x] Do not perform prefix, fuzzy, rename, semantic, or cross-repository lookup.

## Episode detail

- [x] `GetEpisode` requires both Repository root and Episode ID.
- [x] Return L1, L2, exact paths, timestamps, harness, Conversation identity,
  transcript reference, and cursors.
- [x] Return `ErrNotFound` when the ID belongs to another Repository, avoiding
  cross-repository disclosure.
- [x] Return paths in lexical order.

## Tests

- [x] Publish a sealed Capture and assert all Episode fields and paths.
- [x] Inject failures after Episode insertion and path insertion and assert full
  transaction rollback.
- [x] Publish enough paths to require multiple bounded inserts.
- [x] Fail during a later path batch and verify earlier batches roll back.
- [x] Verify identical retry and conflicting retry behavior.
- [x] Verify Capture raw paths disappear only after commit and the terminal row
  references the Episode.
- [x] Create more than five Episodes for one path and verify deterministic
  newest-first truncation.
- [x] Query several paths with duplicate inputs and cross-path Episode overlap.
- [x] Verify exact matching does not include descendants or similarly named
  files.
- [x] Verify Repository-scoped detail access.
- [x] Verify Unicode L1 length and empty summary validation.

## Implementation assumptions, plan changes, and upstream provenance

Listed least-confident first:

1. Exact paths are inserted in batches of 300 rows, or 900 bound variables,
   within the existing publication transaction. This remains below SQLite's
   common variable limits, avoids imposing an undocumented Capture path cap,
   and preserves all-or-nothing publication without a new storage abstraction.
2. Leading and trailing Unicode whitespace is removed from L1 and L2 before
   validation and persistence. Retry equality compares these canonical stored
   values, so whitespace-only differences are idempotent rather than conflicts.
3. A finalized Capture whose `episode_id` is empty is treated as invalid stored
   state. A normal finalized retry loads the referenced Episode inside the same
   transaction and compares L1/L2 before returning it.
4. A `pending_summary` Capture must also have non-empty start/end cursors and an
   end timestamp before publication. Plan 3 guarantees these fields, and making
   the invariant explicit prevents publishing a corrupted partial Capture.
5. `ContextForPaths` resolves the Repository even for an empty path list, then
   returns a non-nil empty result. For non-empty input it uses one CTE/window
   query for all deduplicated paths and reconstructs caller order in Go.
6. The existing nullable `captures.episode_id` column remains without a foreign
   key because it predates the Episode table. Integrity is supplied by the
   unique `episodes.source_capture_id` foreign key plus the atomic publication
   transaction, avoiding a cyclic Capture/Episode foreign-key migration.
7. `api_external_test.go` and `store_test.go` were added to the plan's file list
   to pin the newly implemented public methods and update schema-version/table
   expectations for migration 3.
8. Entire was inspected at commit
   `20dc86ea5e48dc9778c215277f4a35101274cda7`. Relevant paths were
   `cmd/entire/cli/checkpoint/store.go`, `persistent.go`,
   `persistent_write.go`, `persistent_read_store_test.go`,
   `persistent_write_test.go`, and the complete test-only
   `cmd/entire/cli/checkpoint/fsstore/fsstore.go` and `fsstore_test.go`. Entire
   atomically exposes checkpoints through Git ref updates or atomic JSON-file
   replacement and retains deterministic file lists, but it has no SQLite
   Capture-to-Episode transaction, exact-path relational index, or equivalent
   Repository-scoped lookup. Copying those stores would violate D-008, so no
   upstream code was copied. Madeleine retains the compatible mechanics of
   atomic visibility, stable retry identity, and deterministic path order;
   existing `NOTICE` attribution remains unchanged.
9. Local verification passed with `make check`, `go test -race ./...`, repeated
   focused publication/context/detail tests, shuffled repeated tests, and
   `git diff --check`. SQLite `EXPLAIN QUERY PLAN` confirmed the hot lookup uses
   `episode_files_repository_path_episode_idx` with both Repository and path
   constraints.

## Acceptance criteria

- [x] Episode publication is atomic and safe to retry after an unknown client
  outcome.
- [x] The hot lookup is one indexed relational query without relevance scoring.
- [x] Episode detail never returns data from another Repository.
- [x] No transcript body or generated L3 is persisted.

## Excluded from this PR

Git-derived paths, CLI transport, context rendering, and Pi summarization.
