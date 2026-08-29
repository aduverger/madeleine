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

- [x] Inspect relevant `entireio/cli` transcript compaction, storage, pagination,
  and retrieval implementations and tests before coding.
- [x] Adapt compatible mechanics rather than rebuilding them, while preserving
  Madeleine's cursor boundaries and structured-mutation policy.
- [x] Record upstream paths and commit, and retain required attribution.
- [x] Record concrete semantic mismatches for mechanics that are not reused.

Inspected Entire commit `60773bd4b89e487a897958b00a1d168a7ea5aa01`, primarily:

- `cmd/entire/cli/transcript/compact/pi.go` and `pi_test.go` for active-branch
  selection, typed message decoding, and offset-order invariants;
- `cmd/entire/cli/agent/pi/transcript.go` for Pi mutation extraction;
- `cmd/entire/cli/checkpoint/persistent_compact_transcript_test.go` for durable
  compact/full evidence behavior;
- `cmd/entire/cli/checkpoint_api_reader.go` and `cmd/entire/cli/sessions.go` for
  bounded transcript retrieval; and
- `cmd/entire/cli/transcript/parse.go`, its tests, and
  `cmd/entire/cli/integration_test/transcript_offset_test.go` for stable offset
  behavior.

Madeleine adapts the branch-before-boundary and stable-offset invariants. It does
not reuse Entire's compact payload or Git-backed transcript storage: Entire
retains general tool inputs/results and complete transcript files, whereas
Madeleine deliberately stores only sanitized semantic entries in repository-
scoped SQLite records. No copied upstream code required additional attribution.

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
harnesses/pi/state.ts
harnesses/pi/index.ts
harnesses/pi/render.ts
harnesses/pi/episode-tool.ts
harnesses/pi/transcript-tool.ts
harnesses/pi/package.json
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

- [x] Generate a UUIDv7 Transcript ID when a non-empty Capture is first sealed.
- [x] Add `transcripts` with Capture, Repository, Conversation, harness, format
  version, source start/end cursors, nullable-until-publication compact text,
  timestamps, and a unique Capture relationship.
- [x] Add `transcript_entries` keyed by `(transcript_id, position)` with a stable
  entry kind and versioned JSON content.
- [x] Add `transcript_id` to Captures and Episodes; Episode publication copies
  the sealed Capture's Transcript ID.
- [x] Remove `transcript_ref` from Conversations, Captures, Episodes, RPC types,
  renderers, and migrations. The application has never shipped, so edit the
  original migrations and add no compatibility migration or dual-read path.
- [x] Keep start/end cursors on an open or pending Capture and on its Transcript
  as opaque source-boundary metadata. Episode APIs expose the Transcript ID,
  not a harness file path.
- [x] Use Pi's persisted session UUID (`getSessionId()`), not its session-file
  path, as the external Conversation ID. Ephemeral sessions retain a generated
  runtime UUID.
- [x] Delete Capture raw paths after Episode publication as before; retain the
  immutable Transcript and its entries with the Episode.

## Canonical bounded transcript

The Pi adapter owns Pi entry interpretation. It walks the end-to-start parent
chain and submits only entries after the Capture start cursor through the end
cursor.

- [x] Define a versioned structured-entry payload shared by sealing, retry,
  summary, and retrieval.
- [x] Store chronological user text, assistant text, branch summaries, and
  structured `edit`/`write` operation, path, and success/failure metadata.
- [x] Retain a short bounded error for failed mutations, but omit write content,
  edit old/new text, and successful mutation result prose.
- [x] Exclude Pi `compaction` entries. Pi retains the original messages, and a
  compaction may summarize ancestors outside the Capture boundary.
- [x] Exclude read calls and outputs, image/audio bodies, thinking, custom state,
  unrelated tool-result bulk, and complete Madeleine-context blocks.
- [x] Preserve entry kinds and sanitized mutation metadata as structured JSON
  rather than storing only one rendered prompt string.
- [x] Do not impose an arbitrary total transcript limit: one Capture spans a
  complete session interval and may legitimately exceed a model context window.
  Bound RPC framing and retrieved pages without discarding accepted entries.
- [x] Mark both views as untrusted historical data at every model and tool
  boundary.

## Raw and compact views

The names describe two representations of the same bounded Madeleine
transcript; they are not new L3/L4 summary levels.

### Raw

- [x] Return the canonical chronological entries without Pi compaction entries.
- [x] Preserve the full sanitized semantic text while retaining the explicit
  exclusions above; it may equal compact evidence when no chunking is needed.
- [x] Page by stable entry position with a bounded page size and `next_offset`.
- [x] Never read the original Pi JSONL file during retrieval.

### Compact

- [x] Render the full semantic projection using Plan 10's policy and
  authoritative structured paths.
- [x] If it fits the active model, use that projection directly. Otherwise use
  Plan 10's model-context-sized chronological chunk summaries and recursive
  synthesis to produce compact evidence.
- [x] Persist the exact final compact evidence passed to the successful L1/L2
  model call so later retrieval shows what the summarizer saw.
- [x] Return compact text in one bounded response.

## Atomic sealing and recovery

- [x] Extend `capture.seal` to accept the versioned structured entries.
- [x] In one SQLite transaction, validate the open Capture, determine whether it
  has paths, insert its Transcript and entries when non-empty, set end boundary
  and `transcript_id`, and transition to `pending_summary` or `abandoned`.
- [x] Discard the submitted transcript for an empty Capture.
- [x] Make identical seal retries idempotent. Reject a different transcript for
  an already-sealed Capture as a conflict.
- [x] Return `transcript_id`, paths, and status in `FinalizationDraft`.
- [x] Extend `episode.publish` to accept the exact compact evidence used for its
  L1/L2. Store compact text, insert the Episode, and finalize the Capture in the
  existing publication transaction.
- [x] Make an identical publication retry succeed; reject different compact
  evidence or summaries for an already-published Episode.
- [x] Explicit and background retries reconstruct semantic projection from
  persisted entries and Capture paths, then rerun Plan 10's context-sized
  compaction using the active retry model. They never read the Pi session file.
- [x] If projection or atomic sealing fails, keep the Capture open. If model or
  publication fails after sealing, keep the raw Transcript pending; compact text
  becomes immutable only when Episode publication succeeds.

## Retrieval API and Pi tool

- [x] Add repository-scoped `transcript.get` with Transcript ID, view, and raw
  page offset parameters.
- [x] Verify the Transcript belongs to the resolved Repository before returning
  any content.
- [x] Return compact text or a page of typed raw entries plus `next_offset`.
- [x] Include `transcript_id` in Episode detail and remove transcript reference
  and cursor presentation from `madeleine_episode`.
- [x] Add `madeleine_transcript { transcript_id, view, offset? }`.
- [x] Keep `view` strict to `compact | raw`; default to `compact` only if the
  TypeBox schema can express that without compatibility ambiguity.
- [x] Use Pi's standard tool-output truncation as a final safety bound. Raw
  pagination must normally keep each response below that limit.
- [x] Return repository-safe errors without leaking database paths or another
  Repository's Transcript existence.

## Summary integration

- [x] Keep Plan 10's strict final L1/L2 JSON contract, semantic projection, and
  active-model chunking unchanged.
- [x] Include authoritative Capture paths returned by sealing before deciding
  whether projection requires intermediate compaction.
- [x] Publish the exact final evidence given to the successful L1/L2 call as the
  Transcript's compact view.
- [x] Do not store prompt instructions, model identity, raw model response,
  discarded intermediate hierarchy levels, or hidden reasoning in Transcript
  records. The final combined segment summaries remain part of compact evidence.
- [x] Treat an existing identical Episode publication as success.

## Tests

- [x] Schema has Transcript ownership and no `transcript_ref` columns.
- [x] Pi session UUID Conversation identity survives reload and resume without a
  session-file path.
- [x] Linear and forked cursor ranges persist only the selected Capture interval.
- [x] Raw view excludes compaction entries but retains original messages across
  a compaction.
- [x] Branch summaries are retained without traversing abandoned raw branches.
- [x] Tree navigation before the active Capture boundary seals the source
  interval, keeps a fallback source Capture through Pi summarization, and starts
  a destination Capture after navigation; preservation failure cancels
  navigation and a generated branch summary becomes the destination's first
  semantic entry.
- [x] The ancestry check mirrors Pi's effective destination when selecting a
  user or custom message, and aborted or failed branch summarization leaves the
  source fallback Capture active.
- [x] Read calls/output, edit/write bodies, successful result prose, binary
  content, custom state, thinking, and recursive Madeleine context are excluded.
- [x] Atomic seal success, empty abandonment, identical retry, conflicting
  retry, large session-scale transcript, and transaction rollback.
- [x] Crash after sealing recovers from persisted entries and paths without the
  Pi session file.
- [x] The evidence portion of the successful final summary prompt exactly
  equals persisted compact text.
- [x] Summary and publish failures leave Transcript-linked pending Captures.
- [x] Retry succeeds after deleting or moving the original Pi session file.
- [x] Compact retrieval, multi-page raw retrieval, invalid offset/view, missing
  Transcript, and cross-Repository denial.
- [x] Raw database pages exceeding the child-process or Pi output bound keep
  every complete entry reachable in order and leave their next offset visible.
- [x] The Transcript tool's enum schema is accepted by Google-compatible Pi
  providers.
- [x] `madeleine_episode` exposes Transcript ID and
  `madeleine_transcript` renders both untrusted views within Pi output bounds.

## Acceptance criteria

- [x] Every published Episode references one immutable bounded Transcript.
- [x] The agent can retrieve compact evidence and page through raw evidence for
  an Episode without accessing a Pi session file.
- [x] The compact evidence is byte-for-byte the source used for L1/L2.
- [x] No Pi compaction summary can introduce pre-Capture Conversation content.
- [x] A chunk, summary, or publication failure preserves all data required for
  retry in SQLite.
- [x] No database or public RPC response stores or exposes a transcript file
  path.

## Excluded from this PR

Importing old external transcripts, storing image/audio payloads, full read-tool
outputs, semantic transcript search, transcript mutation, transcript deletion
UI, cross-repository retrieval, other harness adapters, and generated L3/L4
summaries.

## Implementation outcome

Plan 11 is implemented. A non-empty seal now atomically stores a generated
Transcript and its structured entries. Summary and retry paths page those SQLite
entries, render the authoritative Capture paths, and publish the exact final
model evidence with the Episode in one transaction. Pi preserves an active
source Capture throughout tree navigation, retains generated branch summaries
inside the destination Transcript, and exposes repository-safe, output-aware
compact and raw retrieval without retaining a session-file reference.

## Plan revisions and decision ledger

Listed least-confident first:

1. SQLite raw retrieval uses both a maximum 50-entry page and an 8 MiB encoded
   entry budget, safely below the adapter's 16 MiB child-process output cap for
   multi-entry pages. The Pi adapter then exposes the largest complete prefix
   that fits its escaped 50KB/2000-line wrapper and advances to the first hidden
   entry; pagination metadata is outside truncatable content. If one entry alone exceeds a bound,
   the page still returns that entry and Pi may show a truncated version; an
   entry above the transport cap remains unavailable through this RPC.
   Intra-entry continuation is deferred as disproportionate MVP complexity. A
   negative offset, compact-view offset, or positive offset beyond available
   entries remains invalid.
2. Before Pi tree navigation moves outside the active Capture boundary, the
   adapter seals at the source leaf and immediately opens a fallback Capture on
   that branch. Failed or cancelled Pi summarization therefore leaves capture
   active. On successful navigation the empty fallback is abandoned and the
   destination Capture begins before a generated branch summary so the summary
   is retained. The ancestry check mirrors Pi's parent target for selected user
   and custom messages. Navigation whose effective target still descends from
   the Capture start cursor does not split the Capture.
3. Mutation entries are emitted when an `edit` or `write` tool result matches a
   bounded-branch tool call. This stores operation, path, and success/failure at
   the chronological result position; incomplete calls without a result are
   omitted because their outcome is unknown.
4. Abandoning a sealed pending Capture deletes its unpublished Transcript after
   clearing the Capture relationship. Published Transcripts remain immutable;
   the existing abandon command continues to mean deleting unfinished data.
5. `madeleine_transcript.view` is an optional Pi `StringEnum` with a default of
   `compact`, keeping Google tool schemas compatible. `raw` must be selected
   explicitly before an offset is accepted.
6. Transcript tables and relationships were added by editing the original
   migrations because the application has never shipped. No compatibility
   migration, dual-read, or legacy file-reference field remains.
7. The persisted Transcript payload is harness-agnostic Madeleine-domain data.
   Pi owns only the translation from Pi session entries; future Claude Code,
   Codex, or other adapters submit and retrieve the same canonical entry kinds,
   so history can cross harness boundaries.
8. "Raw" means the fullest persisted Madeleine semantic evidence view, not a
   byte copy of Pi's JSONL. It deliberately excludes reads, file-content tool
   payloads, successful result prose, and privileged/internal content that do
   not improve file-intent evidence. User, assistant, and branch-summary text
   remain complete even when the session exceeds a model context window.
9. No arbitrary total storage limit is planned. Cursor bounds and semantic
   filtering define Transcript scope; model context limits define chunk size,
   while RPC framing and retrieval pagination bound individual operations.
10. Compact text is stored in addition to structured entries. It may contain
   Capture-specific model-generated chunk summaries, so it is not
   deterministically derivable. Persisting it proves exactly what the final
   summary model saw and prevents a later attempt or renderer change from
   rewriting a published Episode's evidence.
11. Structured Transcript insertion is part of `capture.seal`, rather than a
   separate `transcript.put` followed by sealing. Compact evidence is stored
   atomically with Episode publication, avoiding a separate mutable
   `prepare_compact` state while keeping every failed attempt recoverable from
   raw SQLite entries.
12. Pi compaction summaries are excluded from both views. The append-only raw
   messages remain available, while the summary's semantic start boundary is
   not reliably limited to the Capture start cursor.
13. The existing recovery/MVP plan was moved from Plan 11 to Plan 12 so final
   end-to-end hardening validates persisted evidence rather than immediately
   obsolete transcript references.
