# Plan 3: Live Capture state machine

PR scope: one PR  
Depends on: `plan2.md`  
Design decisions: D-004, D-005, D-007, D-008, D-016

## Goal

Persist unfinished work directly in SQLite and implement the complete Capture
state machine. Until Plan 5, sealing uses only structured paths recorded through
`RecordWrite`; that temporary limitation must be explicit in tests.

## Files

```text
capture.go
capture_store.go
migrations/002_captures.sql
capture_test.go
```

## Schema

Create:

```text
captures
  id TEXT PRIMARY KEY
  conversation_id TEXT NOT NULL REFERENCES conversations(id)
  repository_id TEXT NOT NULL REFERENCES repositories(id)
  worktree_root TEXT NOT NULL
  status TEXT NOT NULL CHECK(status IN (...))
  transcript_ref TEXT
  start_cursor TEXT
  end_cursor TEXT
  started_at TEXT NOT NULL
  ended_at TEXT
  last_seen_at TEXT NOT NULL
  episode_id TEXT

capture_paths
  capture_id TEXT NOT NULL REFERENCES captures(id)
  path TEXT NOT NULL
  source TEXT NOT NULL CHECK(source IN ('tool', 'git'))
  first_seen_at TEXT NOT NULL
  last_seen_at TEXT NOT NULL
  PRIMARY KEY(capture_id, path)
```

- [ ] Add a partial unique index permitting at most one `open` Capture per
  Conversation.
- [ ] Add indexes for pending Capture lookup by Repository, Conversation,
  status, and start time.
- [ ] Keep terminal Capture rows; do not add a TTL or cleanup job.

## Start and get

- [ ] `StartCapture` resolves/persists the Repository and Conversation before
  opening its insertion transaction.
- [ ] Generate a Capture ID and store the current worktree root, transcript
  reference, start cursor, and Store-assigned UTC time.
- [ ] Return `ErrConflict` when the Conversation already has an open Capture.
- [ ] `GetCapture` returns state and boundaries but not raw internal SQL rows.

## Record write

- [ ] Resolve the Capture and require `status=open`.
- [ ] Normalize the supplied path against the Capture's stored worktree root.
- [ ] Upsert `(capture_id, path)` with source `tool`, preserving
  `first_seen_at` and updating `last_seen_at`.
- [ ] Update Capture `last_seen_at` in the same transaction.
- [ ] Reject writes to sealed, finalized, or abandoned Captures with
  `ErrInvalidState`.
- [ ] Make duplicate calls safe under concurrent processes.

## List pending and seal

- [ ] `ListPendingCaptures` filters by resolved Repository and optionally by
  Conversation, returning `open` and `pending_summary` oldest-first.
- [ ] `SealCapture` requires an end cursor and executes one state transaction.
- [ ] Freeze the current distinct path set in lexical order.
- [ ] If the set is empty, set `ended_at`, transition to `abandoned`, and return
  `FinalizationDraft{Empty:true}`.
- [ ] Otherwise set `end_cursor`, `ended_at`, and `pending_summary`, returning
  the frozen paths.
- [ ] Repeating seal on `pending_summary` returns the identical draft.
- [ ] Repeating seal on `abandoned` returns an empty result.
- [ ] Sealing `finalized` returns the associated Episode reference; it must not
  reopen or mutate the Capture.

## Abandon

- [ ] Allow abandon from `open` or `pending_summary`.
- [ ] Delete raw Capture paths and transition to `abandoned` atomically.
- [ ] Repeating abandon is a no-op.
- [ ] Abandoning a finalized Capture returns `ErrInvalidState`.

## Tests

- [ ] Cover every valid and invalid status transition as a table-driven state
  machine test.
- [ ] Race two `StartCapture` calls for the same Conversation.
- [ ] Race repeated writes to one path and independent writes to many paths.
- [ ] Verify `first_seen_at` is stable and `last_seen_at` advances.
- [ ] Reject traversal and outside-worktree paths.
- [ ] Verify deterministic seal ordering and idempotent repeated seals.
- [ ] Verify empty Capture abandonment and raw-path deletion.
- [ ] Verify a terminal row remains for idempotency.
- [ ] Verify pending queries are Repository/Conversation isolated.

## Acceptance criteria

- [ ] A process can crash after `RecordWrite`, reopen the Store, and recover the
  open Capture and its paths.
- [ ] Live paths contain no reads, summaries, transcript bodies, or Pi-specific
  tool payloads.
- [ ] Only one open Capture can exist for a Conversation.
- [ ] All prior migrations and tests continue to pass.

## Excluded from this PR

Episodes, summary generation, Git reconciliation, RPC, and Pi hooks.
