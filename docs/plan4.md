# Plan 4: Episode publication and exact-path retrieval

PR scope: one PR  
Depends on: `plan3.md`  
Design decisions: D-001, D-002, D-003, D-004, D-005, D-012, D-021

## Goal

Publish a sealed Capture as an immutable Episode and implement the two core
read questions: "which Episodes changed this path?" and "what is the full L2
for this Episode?"

## Entire reuse gate

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
episode.go
episode_store.go
context.go
migrations/003_episodes.sql
episode_test.go
context_test.go
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

- [ ] Add an index on `(repository_id, path, episode_id)` and an Episode time
  index supporting newest-first joins.
- [ ] Do not add summary JSON, embeddings, FTS, directory rows, or line data.

## Publish Episode

- [ ] Require the Capture to be `pending_summary` and to contain at least one
  frozen path.
- [ ] Validate trimmed non-empty L1/L2 and reject L1 over 400 Unicode
  characters.
- [ ] Generate one Episode ID and copy authoritative repository, Conversation,
  harness, timestamps, transcript reference, and cursor boundaries from the
  Capture.
- [ ] In one transaction, insert the Episode, batch-insert its exact paths,
  mark the Capture `finalized` with the Episode ID, and delete raw Capture path
  rows.
- [ ] Repeating the identical publication returns the existing Episode.
- [ ] Repeating with a different L1 or L2 returns `ErrConflict`.
- [ ] Never partially expose an Episode if any insert/update fails.

## Context lookup

- [ ] `ContextForPaths` resolves the current Repository and normalizes every
  requested path.
- [ ] Deduplicate input paths while preserving caller order.
- [ ] Return one `FileContext` per normalized input path.
- [ ] For each path, return at most five Episode summaries ordered by
  `ended_at DESC, id DESC`.
- [ ] Each summary includes Episode ID, ended time, harness, and L1.
- [ ] Return an empty Episode slice for a valid path with no matching Episodes.
- [ ] Do not perform prefix, fuzzy, rename, semantic, or cross-repository lookup.

## Episode detail

- [ ] `GetEpisode` requires both Repository root and Episode ID.
- [ ] Return L1, L2, exact paths, timestamps, harness, Conversation identity,
  transcript reference, and cursors.
- [ ] Return `ErrNotFound` when the ID belongs to another Repository, avoiding
  cross-repository disclosure.
- [ ] Return paths in lexical order.

## Tests

- [ ] Publish a sealed Capture and assert all Episode fields and paths.
- [ ] Inject failures after Episode insertion and path insertion and assert full
  transaction rollback.
- [ ] Verify identical retry and conflicting retry behavior.
- [ ] Verify Capture raw paths disappear only after commit and the terminal row
  references the Episode.
- [ ] Create more than five Episodes for one path and verify deterministic
  newest-first truncation.
- [ ] Query several paths with duplicate inputs and cross-path Episode overlap.
- [ ] Verify exact matching does not include descendants or similarly named
  files.
- [ ] Verify Repository-scoped detail access.
- [ ] Verify Unicode L1 length and empty summary validation.

## Acceptance criteria

- [ ] Episode publication is atomic and safe to retry after an unknown client
  outcome.
- [ ] The hot lookup is one indexed relational query without relevance scoring.
- [ ] Episode detail never returns data from another Repository.
- [ ] No transcript body or generated L3 is persisted.

## Excluded from this PR

Git-derived paths, CLI transport, context rendering, and Pi summarization.
