# Plan 3: Capture state machine

PR scope: one PR  
Depends on: `plan2.md`  
Design decisions: D-004, D-005, D-007, D-008, D-016, D-021

> Historical note: paths and public-package references describe the merged PR.
> Plan 6 later internalizes the Go implementation when Madeleine becomes a
> standalone application.

## Goal

Persist unfinished work directly in SQLite and implement the complete Capture
state machine. Until Plan 5, sealing uses only structured paths recorded through
`RecordWrite`; that temporary limitation must be explicit in tests.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
capture.go
capture_store.go
migrations/002_captures.sql
capture_test.go
api_external_test.go
store_test.go
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

- [x] Add a partial unique index permitting at most one `open` Capture per
  Conversation.
- [x] Add indexes for pending Capture lookup by Repository, Conversation,
  status, and start time.
- [x] Keep terminal Capture rows; do not add a TTL or cleanup job.

## Start and get

- [x] `StartCapture` resolves/persists the Repository and Conversation before
  opening its insertion transaction.
- [x] Generate a Capture ID and store the current worktree root, transcript
  reference, start cursor, and Store-assigned UTC time.
- [x] Return `ErrConflict` when the Conversation already has an open Capture.
- [x] `GetCapture` returns state and boundaries but not raw internal SQL rows.

## Record write

- [x] Resolve the Capture and require `status=open`.
- [x] Normalize the supplied path against the Capture's stored worktree root.
- [x] Upsert `(capture_id, path)` with source `tool`, preserving
  `first_seen_at` and updating `last_seen_at`.
- [x] Update Capture `last_seen_at` in the same transaction.
- [x] Reject writes to sealed, finalized, or abandoned Captures with
  `ErrInvalidState`.
- [x] Make duplicate calls safe under concurrent processes.

## List pending and seal

- [x] `ListPendingCaptures` filters by resolved Repository and optionally by
  Conversation, returning `open` and `pending_summary` oldest-first.
- [x] `SealCapture` requires an end cursor and executes one state transaction.
- [x] Freeze the current distinct path set in lexical order.
- [x] If the set is empty, set `ended_at`, transition to `abandoned`, and return
  `FinalizationDraft{Empty:true}`.
- [x] Otherwise set `end_cursor`, `ended_at`, and `pending_summary`, returning
  the frozen paths.
- [x] Repeating seal on `pending_summary` returns the identical draft.
- [x] Repeating seal on `abandoned` returns an empty result.
- [x] Sealing `finalized` returns the associated Episode reference; it must not
  reopen or mutate the Capture.

## Abandon

- [x] Allow abandon from `open` or `pending_summary`.
- [x] Delete raw Capture paths and transition to `abandoned` atomically.
- [x] Repeating abandon is a no-op.
- [x] Abandoning a finalized Capture returns `ErrInvalidState`.

## Tests

- [x] Cover every valid and invalid status transition as a table-driven state
  machine test.
- [x] Race two `StartCapture` calls for the same Conversation.
- [x] Race repeated writes to one path and independent writes to many paths.
- [x] Verify `first_seen_at` is stable and `last_seen_at` advances.
- [x] Reject traversal and outside-worktree paths.
- [x] Verify deterministic seal ordering and idempotent repeated seals.
- [x] Verify empty Capture abandonment and raw-path deletion.
- [x] Verify a terminal row remains for idempotency.
- [x] Verify pending queries are Repository/Conversation isolated.

## Implementation assumptions, plan changes, and upstream provenance

Listed least-confident first:

1. “Repeating seal on `abandoned` returns an empty result” is interpreted as an
   idempotent `FinalizationDraft` identifying the Capture, with status
   `abandoned`, `Empty:true`, and a non-nil empty path slice, rather than a
   zero-valued draft that loses the Capture identity.
2. An optional Conversation filter that does not match a persisted Conversation
   returns an empty list and does not create a Conversation. Listing remains a
   read of Capture state after the required Repository resolution.
3. `RepositoryRoot`, `StartCursor`, and `EndCursor` are required as exact,
   non-empty opaque strings. They are not trimmed or interpreted. The plan was
   explicit only about the end cursor; requiring the other two avoids resolving
   an accidental process working directory or creating a boundaryless Capture.
4. Empty sealing stores the required end cursor as well as `ended_at`. This
   preserves the observed terminal boundary even though no Episode will be
   generated.
5. “Freeze” means the ordered path set remains in `capture_paths` while the
   Capture is `pending_summary`, and the state machine rejects every later
   `RecordWrite`. Plan 4 atomically copies those rows into immutable Episode
   history and deletes them; Plan 3 does not add a duplicate frozen-path table.
6. Abandoning an already sealed `pending_summary` Capture preserves its existing
   `ended_at`; abandoning an `open` Capture assigns the abandonment time. The
   end boundary describes when capture stopped, not when later cleanup ran.
7. Oldest-first ordering uses `started_at ASC, id ASC`. The UUIDv7 ID is only a
   deterministic tie-breaker when timestamps are equal.
8. `api_external_test.go` and `store_test.go` were added to the plan's file list.
   The former pins the newly implemented public methods; the latter must expect
   migration 2 and use schema version 3 as its future-version fixture.
9. Entire was inspected at commit
   `345a4e03b0a59e562c45e5df4e0b1ec12e71dede`. Relevant paths were
   `cmd/entire/cli/session/phase.go` and `phase_test.go`,
   `cmd/entire/cli/session/state.go` and `state_test.go`,
   `cmd/entire/cli/agent/session.go`,
   `cmd/entire/cli/agent/opencode/transcript.go`,
   `cmd/entire/cli/checkpoint/store.go`, and
   `cmd/entire/cli/paths/worktree.go` and its tests. Entire persists JSON
   session state and Git-backed checkpoints, derives files from transcripts and
   Git state, and uses an `idle/active/ended` lifecycle. It has no equivalent
   SQLite Capture rows, partial uniqueness rule, transactional path upsert, or
   Madeleine `open/pending_summary/finalized/abandoned` lifecycle. Copying its
   code would violate D-008 and Madeleine's state semantics, so no upstream code
   was copied. The implementation adapts only the compatible mechanics: one
   canonical tested transition table, durable repository-relative paths,
   terminal idempotency, and abandonment when no files were captured. Existing
   `NOTICE` attribution remains intact.
10. Before Plan 5, filesystem or shell-only changes are intentionally invisible.
   A dedicated test creates an unrecorded file and confirms sealing abandons the
   Capture; only successful `RecordWrite` paths can keep it pending in this PR.
11. Terminology was clarified after implementation: Capture and Episode are the
   domain entities; `live` and `history` are descriptions, not parallel models.
   This changes documentation only and leaves the schema, API, and state machine
   unchanged.

## Acceptance criteria

- [x] A process can crash after `RecordWrite`, reopen the Store, and recover the
  open Capture and its paths.
- [x] Capture paths contain no reads, summaries, transcript bodies, or
  Pi-specific tool payloads.
- [x] Only one open Capture can exist for a Conversation.
- [x] All prior migrations and tests continue to pass.

## Excluded from this PR

Episodes, summary generation, Git reconciliation, RPC, and Pi hooks.
