# Plan 11: Persisted bounded transcripts and evidence retrieval

PR scope: one PR  
Depends on: `plan10.md`  
Design decisions: D-003, D-004, D-006, D-016, D-017, D-021, D-022, D-023, D-025

## Goal

Make the evidence behind an Episode durable and agent-accessible. At Capture
sealing, Pi must submit a cursor-bounded, sanitized semantic transcript to
Madeleine. Madeleine persists it under a generated Transcript ID; Pi derives a
model-sized compact view from that evidence, generates L1/L2, and atomically
publishes the compact view with the Episode. Retrieval exposes both `compact`
and `raw` without depending on the original Pi session file.

## Entire reuse gate

- [ ] Inspect relevant `entireio/cli` transcript compaction, storage, pagination,
  and retrieval implementations and tests before coding.
- [ ] Adapt compatible mechanics rather than rebuilding them, while preserving
  Madeleine's cursor boundaries and structured-mutation policy.
- [ ] Record upstream paths and commit, and retain required attribution.
- [ ] Record concrete semantic mismatches for mechanics that are not reused.

## Files

```text
internal/store/migrations/*.sql
internal/store/transcript.go
internal/store/records.go
internal/madeleine/capture.go
internal/madeleine/episode.go
internal/madeleine/transcript.go
internal/madeleine/types.go
internal/rpc/methods.go
harnesses/pi/transcript.ts
harnesses/pi/summary.ts
harnesses/pi/lifecycle.ts
harnesses/pi/rpc.ts
harnesses/pi/episode-tool.ts
harnesses/pi/transcript-tool.ts
harnesses/pi/*.test.ts
docs/design.md
docs/plan11.md
docs/plan12.md
README.md
```

## Domain and schema

Introduce `Transcript` as a generated, repository-owned evidence record rather
than another lifecycle unit.

```text
Capture --0..1--> Transcript <--1-- Episode
Transcript --1..n--> TranscriptEntry
```

- [ ] Generate a UUIDv7 Transcript ID when a non-empty Capture is first sealed.
- [ ] Add `transcripts` with Capture, Repository, Conversation, harness, format
  version, source start/end cursors, nullable-until-publication compact text,
  timestamps, and a unique Capture relationship.
- [ ] Add `transcript_entries` keyed by `(transcript_id, position)` with a stable
  entry kind and versioned JSON content.
- [ ] Add `transcript_id` to Captures and Episodes; Episode publication copies
  the sealed Capture's Transcript ID.
- [ ] Remove `transcript_ref` from Conversations, Captures, Episodes, RPC types,
  renderers, and migrations. The application has never shipped, so edit the
  original migrations and add no compatibility migration or dual-read path.
- [ ] Keep start/end cursors on an open or pending Capture and on its Transcript
  as opaque source-boundary metadata. Episode APIs expose the Transcript ID,
  not a harness file path.
- [ ] Use Pi's persisted session UUID (`getSessionId()`), not its session-file
  path, as the external Conversation ID. Ephemeral sessions retain a generated
  runtime UUID.
- [ ] Delete Capture raw paths after Episode publication as before; retain the
  immutable Transcript and its entries with the Episode.

## Canonical bounded transcript

The Pi adapter owns Pi entry interpretation. It walks the end-to-start parent
chain and submits only entries after the Capture start cursor through the end
cursor.

- [ ] Define a versioned structured-entry payload shared by sealing, retry,
  summary, and retrieval.
- [ ] Store chronological user text, assistant text, branch summaries, and
  structured `edit`/`write` operation, path, and success/failure metadata.
- [ ] Retain a short bounded error for failed mutations, but omit write content,
  edit old/new text, and successful mutation result prose.
- [ ] Exclude Pi `compaction` entries. Pi retains the original messages, and a
  compaction may summarize ancestors outside the Capture boundary.
- [ ] Exclude read calls and outputs, image/audio bodies, thinking, custom state,
  unrelated tool-result bulk, and complete Madeleine-context blocks.
- [ ] Preserve entry kinds and sanitized mutation metadata as structured JSON
  rather than storing only one rendered prompt string.
- [ ] Do not impose an arbitrary total transcript limit: one Capture spans a
  complete session interval and may legitimately exceed a model context window.
  Bound RPC framing and retrieved pages without discarding accepted entries.
- [ ] Mark both views as untrusted historical data at every model and tool
  boundary.

## Raw and compact views

The names describe two representations of the same bounded Madeleine
transcript; they are not new L3/L4 summary levels.

### Raw

- [ ] Return the canonical chronological entries without Pi compaction entries.
- [ ] Preserve the full sanitized semantic text while retaining the explicit
  exclusions above; it may equal compact evidence when no chunking is needed.
- [ ] Page by stable entry position with a bounded page size and `next_offset`.
- [ ] Never read the original Pi JSONL file during retrieval.

### Compact

- [ ] Render the full semantic projection using Plan 10's policy and
  authoritative structured paths.
- [ ] If it fits the active model, use that projection directly. Otherwise use
  Plan 10's model-context-sized chronological chunk summaries and recursive
  synthesis to produce compact evidence.
- [ ] Persist the exact final compact evidence passed to the successful L1/L2
  model call so later retrieval shows what the summarizer saw.
- [ ] Return compact text in one bounded response.

## Atomic sealing and recovery

- [ ] Extend `capture.seal` to accept the versioned structured entries.
- [ ] In one SQLite transaction, validate the open Capture, determine whether it
  has paths, insert its Transcript and entries when non-empty, set end boundary
  and `transcript_id`, and transition to `pending_summary` or `abandoned`.
- [ ] Discard the submitted transcript for an empty Capture.
- [ ] Make identical seal retries idempotent. Reject a different transcript for
  an already-sealed Capture as a conflict.
- [ ] Return `transcript_id`, paths, and status in `FinalizationDraft`.
- [ ] Extend `episode.publish` to accept the exact compact evidence used for its
  L1/L2. Store compact text, insert the Episode, and finalize the Capture in the
  existing publication transaction.
- [ ] Make an identical publication retry succeed; reject different compact
  evidence or summaries for an already-published Episode.
- [ ] Explicit and background retries reconstruct semantic projection from
  persisted entries and Capture paths, then rerun Plan 10's context-sized
  compaction using the active retry model. They never read the Pi session file.
- [ ] If projection or atomic sealing fails, keep the Capture open. If model or
  publication fails after sealing, keep the raw Transcript pending; compact text
  becomes immutable only when Episode publication succeeds.

## Retrieval API and Pi tool

- [ ] Add repository-scoped `transcript.get` with Transcript ID, view, and raw
  page offset parameters.
- [ ] Verify the Transcript belongs to the resolved Repository before returning
  any content.
- [ ] Return compact text or a page of typed raw entries plus `next_offset`.
- [ ] Include `transcript_id` in Episode detail and remove transcript reference
  and cursor presentation from `madeleine_episode`.
- [ ] Add `madeleine_transcript { transcript_id, view, offset? }`.
- [ ] Keep `view` strict to `compact | raw`; default to `compact` only if the
  TypeBox schema can express that without compatibility ambiguity.
- [ ] Use Pi's standard tool-output truncation as a final safety bound. Raw
  pagination must normally keep each response below that limit.
- [ ] Return repository-safe errors without leaking database paths or another
  Repository's Transcript existence.

## Summary integration

- [ ] Keep Plan 10's strict final L1/L2 JSON contract, semantic projection, and
  active-model chunking unchanged.
- [ ] Include authoritative Capture paths returned by sealing before deciding
  whether projection requires intermediate compaction.
- [ ] Publish the exact final evidence given to the successful L1/L2 call as the
  Transcript's compact view.
- [ ] Do not store prompt instructions, model identity, raw model response,
  discarded intermediate hierarchy levels, or hidden reasoning in Transcript
  records. The final combined segment summaries remain part of compact evidence.
- [ ] Treat an existing identical Episode publication as success.

## Tests

- [ ] Schema has Transcript ownership and no `transcript_ref` columns.
- [ ] Pi session UUID Conversation identity survives reload and resume without a
  session-file path.
- [ ] Linear and forked cursor ranges persist only the selected Capture interval.
- [ ] Raw view excludes compaction entries but retains original messages across
  a compaction.
- [ ] Branch summaries are retained without traversing abandoned raw branches.
- [ ] Read calls/output, edit/write bodies, successful result prose, binary
  content, custom state, thinking, and recursive Madeleine context are excluded.
- [ ] Atomic seal success, empty abandonment, identical retry, conflicting
  retry, large session-scale transcript, and transaction rollback.
- [ ] Crash after sealing recovers from persisted entries and paths without the
  Pi session file.
- [ ] The evidence portion of the successful final summary prompt exactly
  equals persisted compact text.
- [ ] Summary and publish failures leave Transcript-linked pending Captures.
- [ ] Retry succeeds after deleting or moving the original Pi session file.
- [ ] Compact retrieval, multi-page raw retrieval, invalid offset/view, missing
  Transcript, and cross-Repository denial.
- [ ] `madeleine_episode` exposes Transcript ID and
  `madeleine_transcript` renders both untrusted views within Pi output bounds.

## Acceptance criteria

- [ ] Every published Episode references one immutable bounded Transcript.
- [ ] The agent can retrieve compact evidence and page through raw evidence for
  an Episode without accessing a Pi session file.
- [ ] The compact evidence is byte-for-byte the source used for L1/L2.
- [ ] No Pi compaction summary can introduce pre-Capture Conversation content.
- [ ] A chunk, summary, or publication failure preserves all data required for
  retry in SQLite.
- [ ] No database or public RPC response stores or exposes a transcript file
  path.

## Excluded from this PR

Importing old external transcripts, storing image/audio payloads, full read-tool
outputs, semantic transcript search, transcript mutation, transcript deletion
UI, cross-repository retrieval, other harness adapters, and generated L3/L4
summaries.

## Plan revisions and decision ledger

Listed least-confident first:

1. "Raw" means the fullest persisted Madeleine semantic evidence view, not a
   byte copy of Pi's JSONL. It deliberately excludes reads, file-content tool
   payloads, successful result prose, and privileged/internal content that do
   not improve file-intent evidence. User, assistant, and branch-summary text
   remain complete even when the session exceeds a model context window.
2. No arbitrary total storage limit is planned. Cursor bounds and semantic
   filtering define Transcript scope; model context limits define chunk size,
   while RPC framing and retrieval pagination bound individual operations.
3. Compact text is stored in addition to structured entries. It may contain
   Capture-specific model-generated chunk summaries, so it is not
   deterministically derivable. Persisting it proves exactly what the final
   summary model saw and prevents a later attempt or renderer change from
   rewriting a published Episode's evidence.
4. Structured Transcript insertion is part of `capture.seal`, rather than a
   separate `transcript.put` followed by sealing. Compact evidence is stored
   atomically with Episode publication, avoiding a separate mutable
   `prepare_compact` state while keeping every failed attempt recoverable from
   raw SQLite entries.
5. Pi compaction summaries are excluded from both views. The append-only raw
   messages remain available, while the summary's semantic start boundary is
   not reliably limited to the Capture start cursor.
6. The existing recovery/MVP plan was moved from Plan 11 to Plan 12 so final
   end-to-end hardening validates persisted evidence rather than immediately
   obsolete transcript references.
