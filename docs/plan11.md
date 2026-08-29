# Plan 11: Persisted bounded transcripts and evidence retrieval

PR scope: one PR  
Depends on: `plan10.md`  
Design decisions: D-003, D-004, D-006, D-016, D-017, D-021, D-022, D-023, D-025

## Goal

Make the evidence behind an Episode durable and agent-accessible. At Capture
sealing, Pi must submit a cursor-bounded, sanitized transcript to Madeleine;
Madeleine persists it under a generated Transcript ID, uses its compact view for
L1/L2 generation, and exposes both `compact` and `raw` views without depending
on the original Pi session file.

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
  version, source start/end cursors, nullable-until-prepared compact text,
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
  structured `edit`/`write` calls and results with success/failure.
- [ ] Exclude Pi `compaction` entries. Pi retains the original messages, and a
  compaction may summarize ancestors outside the Capture boundary.
- [ ] Exclude read outputs, image/audio bodies, thinking, custom state,
  unrelated tool-result bulk, and complete Madeleine-context blocks.
- [ ] Preserve entry kinds and mutation inputs as structured JSON rather than
  storing only one rendered prompt string.
- [ ] Apply named per-entry and total stored-transcript limits. Reject an
  oversized transcript before sealing rather than silently claiming complete
  raw evidence.
- [ ] Mark both views as untrusted historical data at every model and tool
  boundary.

## Raw and compact views

The names describe two representations of the same bounded Madeleine
transcript; they are not new L3/L4 summary levels.

### Raw

- [ ] Return the canonical chronological entries without Pi compaction entries.
- [ ] Preserve more text than the compact view while retaining the explicit
  exclusions and storage limits above.
- [ ] Page by stable entry position with a bounded page size and `next_offset`.
- [ ] Never read the original Pi JSONL file during retrieval.

### Compact

- [ ] Deterministically render the canonical entries using Plan 10's projection
  policy and named prompt limits.
- [ ] Reserve the first user goal and authoritative structured paths, then
  select all other entries newest-first without giving messages, mutations, or
  branch summaries different truncation priority.
- [ ] Persist the exact compact text passed to the L1/L2 model so later evidence
  retrieval shows what the summarizer saw.
- [ ] Return compact text in one bounded response.

## Atomic sealing and recovery

- [ ] Extend `capture.seal` to accept the versioned bounded structured entries;
  compact text is prepared only after sealing returns authoritative paths.
- [ ] In one SQLite transaction, validate the open Capture, determine whether it
  has paths, insert its Transcript and entries when non-empty, set end boundary
  and `transcript_id`, and transition to `pending_summary` or `abandoned`.
- [ ] Discard the submitted transcript for an empty Capture.
- [ ] Make identical seal retries idempotent. Reject a different transcript for
  an already-sealed Capture as a conflict.
- [ ] Return `transcript_id`, paths, and status in `FinalizationDraft`.
- [ ] Add an idempotent `transcript.prepare_compact` operation that sets compact
  text once. An identical retry succeeds; different text conflicts.
- [ ] After sealing, render compact text from the persisted structured entries
  and returned authoritative paths, persist it, then generate L1/L2 from that
  exact stored text.
- [ ] Explicit and background retries load compact text by Transcript ID. If a
  crash left it unset, reconstruct it from persisted entries and Capture paths,
  then set it through the same operation. Neither path reads the Pi session file.
- [ ] If projection or atomic sealing fails, keep the Capture open. If summary or
  publication fails after sealing, keep the Capture and Transcript pending.

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

- [ ] Keep Plan 10's strict L1/L2 JSON contract and active-model call unchanged.
- [ ] Feed the persisted compact view to the model exactly once per attempt.
- [ ] Include authoritative Capture paths returned by sealing in compact text
  before preparing the Transcript for summary.
- [ ] Do not store prompt instructions, model identity, raw model response, or
  hidden reasoning in Transcript records.
- [ ] Treat an existing identical Episode publication as success.

## Tests

- [ ] Schema has Transcript ownership and no `transcript_ref` columns.
- [ ] Pi session UUID Conversation identity survives reload and resume without a
  session-file path.
- [ ] Linear and forked cursor ranges persist only the selected Capture interval.
- [ ] Raw view excludes compaction entries but retains original messages across
  a compaction.
- [ ] Branch summaries are retained without traversing abandoned raw branches.
- [ ] Read output, binary content, custom state, thinking, and recursive
  Madeleine context are excluded.
- [ ] Atomic seal success, empty abandonment, identical retry, conflicting
  retry, oversized transcript, and transaction rollback.
- [ ] Crash between sealing and compact preparation recovers from persisted
  entries and paths without the Pi session file.
- [ ] Summary input exactly equals persisted compact text.
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
- [ ] A summary failure preserves all data required for retry in SQLite.
- [ ] No database or public RPC response stores or exposes a transcript file
  path.

## Excluded from this PR

Importing old external transcripts, storing image/audio payloads, full read-tool
outputs, semantic transcript search, transcript mutation, transcript deletion
UI, cross-repository retrieval, other harness adapters, and generated L3/L4
summaries.

## Plan revisions and decision ledger

Listed least-confident first:

1. "Raw" means the fullest persisted Madeleine evidence view, not a byte copy of
   Pi's JSONL. It deliberately excludes bulk and privileged/internal content
   that is not useful file-intent evidence. This interpretation keeps the new
   storage aligned with Madeleine's retrieval purpose and the user's request to
   persist the bounded transcript used by summarization.
2. Compact text is stored in addition to structured entries. It is derivable,
   but persisting it proves exactly what the summary model saw and prevents a
   later renderer change from rewriting an Episode's evidence.
3. Structured Transcript insertion is part of `capture.seal`, rather than a
   separate `transcript.put` followed by sealing. This avoids a crash window
   containing a sealed Capture whose only evidence still lives in a harness
   file. Compact text is set afterward because only sealing returns the frozen
   authoritative paths; a crash in between is recoverable from SQLite.
4. Pi compaction summaries are excluded from both views. The append-only raw
   messages remain available, while the summary's semantic start boundary is
   not reliably limited to the Capture start cursor.
5. The existing recovery/MVP plan was moved from Plan 11 to Plan 12 so final
   end-to-end hardening validates persisted evidence rather than immediately
   obsolete transcript references.
