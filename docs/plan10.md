# Plan 10: Pi summary generation and clean publication

PR scope: one PR  
Depends on: `plan9.md`  
Design decisions: D-003, D-006, D-013, D-016, D-017, D-021, D-023, D-024

## Goal

Complete the clean-run pipeline: seal a session-scale Capture, project its
cursor-bounded semantic Pi transcript, compact that evidence against the active
model's context window when necessary, generate validated L1/L2, and publish an
immutable Episode. Any failure must leave `pending_summary` recoverable.

## Entire reuse gate

- [x] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [x] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [x] Record reused upstream paths and commit, and retain required attribution.
- [x] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
harnesses/pi/transcript.ts
harnesses/pi/summary.ts
harnesses/pi/lifecycle.ts
harnesses/pi/commands.ts
harnesses/pi/rpc.ts
harnesses/pi/package.json
harnesses/pi/*.test.ts
```

## Transcript boundaries

- [x] Read Pi session entries through `ctx.sessionManager.getEntries()`.
- [x] Locate the sealed Capture's start and end entry IDs.
- [x] Walk parent relationships from the end cursor to reconstruct that branch,
  then reverse to chronological order.
- [x] Exclude the start cursor itself and all ancestors before it; include the
  end entry.
- [x] Fail with a recoverable summary error when required cursors cannot be
  resolved. Do not silently summarize the whole Conversation.

## Projection policy

- [x] Include user text, assistant text, branch summaries, and mutation tool
  calls/results between the boundaries.
- [x] Omit Pi compaction summaries: the original in-bound messages remain in
  the append-only transcript, while a compaction may also summarize content
  from before the Capture boundary.
- [x] Include the structured mutation paths from `FinalizationDraft` as
  authoritative metadata.
- [x] Omit read tool outputs, image/audio payloads, internal custom state, and
  unrelated tool-result bulk.
- [x] Strip complete `<madeleine-context ...>...</madeleine-context>` blocks
  before constructing summary input.
- [x] Retain mutation tool names, paths, success/failure, and a short error for
  failed mutations.
- [x] Exclude `write` content, `edit` old/new text, and successful mutation
  result prose. Authoritative paths already preserve the relevant file facts.
- [x] Preserve the complete sanitized semantic projection; do not discard old
  in-bound messages through a fixed character limit.
- [x] Mark projected transcript and intermediate summaries as untrusted source
  data in every model prompt.

## Session-scale compaction

- [x] Read the active model's `contextWindow` token limit from `ctx.model`.
- [x] Estimate prompt tokens with Pi's exported conservative `estimateTokens`
  helper.
- [x] Reserve the requested output tokens plus a safety margin before choosing
  the maximum input size.
- [x] If the semantic projection fits, generate L1/L2 in one model call.
- [x] Otherwise split it chronologically into model-sized chunks, summarize
  each chunk, and synthesize L1/L2 from the ordered intermediate summaries.
- [x] Recursively compact intermediate summaries only when their combined text
  still cannot fit the final call.
- [x] Never use Pi compaction summaries as a shortcut because their cumulative
  content may begin before the Capture boundary.

## Summary contract

The model must return exactly one JSON object and no Markdown fence:

```json
{
  "l1": "One or two sentences, maximum 400 characters.",
  "l2": "A 300-800 token brief covering goal, decisions, actions, tests and caveats."
}
```

- [x] Put the prompt text and a `summary_prompt_version = 1` constant in one
  module.
- [x] Validate exact object shape, string types, trimmed non-empty values, and
  the L1 Unicode limit.
- [x] Reject prose surrounding JSON, missing/extra fields, code fences, empty
  values, and oversized L1 rather than repairing or truncating silently.
- [x] Do not persist model name, prompt text, hidden reasoning, or raw response
  in the MVP.

## Active-model call

- [x] Use `ctx.model`; if absent or unauthenticated, leave the Capture pending.
- [x] Call `ctx.modelRegistry.complete` for each segment or final synthesis with
  one user message, the active model, `cacheRetention: "none"`, a fresh UUIDv7
  session ID, and a bounded output-token limit.
- [x] Use an AbortController with a 30-second timeout per model call and combine
  it with any lifecycle cancellation.
- [x] Reject truncated, empty, error, and aborted intermediate responses.
- [x] Extract final text content only and validate the strict JSON contract.
- [x] Never insert the summarization request/response into the active Pi
  Conversation.

## Clean finalization

- [x] Implement one finalization function shared by non-reload shutdown,
  `/madeleine rollover`, and explicit retry.
- [x] For every non-reload shutdown, stop background work, seal the current
  Capture, and return immediately for an empty/abandoned draft.
- [x] Project, summarize, then call `episode.publish` with the Capture ID and
  validated L1/L2.
- [x] Treat an identical publish retry as success.
- [x] On model, parse, timeout, or publish failure, notify once and leave
  `pending_summary` unchanged.
- [x] Clear current in-memory Capture state after sealing has durably frozen its
  paths and boundaries; publication failure leaves the old Capture
  `pending_summary` without blocking a replacement Capture.

## Manual rollover

- [x] Upgrade the existing `/madeleine rollover` command to use the shared
  finalization function.
- [x] Wait for Pi to become idle, seal and attempt to publish the current
  Capture, then start a replacement in the same Conversation.
- [x] If sealing fails, keep the current Capture active and do not start a
  replacement.
- [x] If summary or publication fails after sealing, start the replacement and
  report that the previous Capture remains pending.
- [x] On success, report the published Episode ID and replacement Capture ID.

## Explicit retry command

- [x] Add `/madeleine retry [capture-id]`.
- [x] With an ID, require it to be pending in the current Repository and
  Conversation.
- [x] Without an ID, retry pending Captures for the current Conversation
  oldest-first, one at a time.
- [x] Reconstruct each Capture's transcript range independently.
- [x] Report per-Capture success/failure without abandoning failures.

## Tests

- [x] Linear branch projection and exclusion of pre-start entries.
- [x] Forked branch reconstruction using parent IDs.
- [x] Branch-summary inclusion and compaction-summary omission, including a
  Capture spanning compaction through its raw messages.
- [x] Read-output, mutation payload, successful result prose, and binary-content
  omission; mutation metadata, bounded failures, and recursive
  Madeleine-context stripping.
- [x] Long semantic evidence is preserved and split according to the active
  model's context window without exceeding the estimated input budget.
- [x] Valid summary, surrounding prose, code fence, extra key, missing key,
  empty value, Unicode-overlong L1, and empty model response.
- [x] No model/auth, model rejection, timeout, cancellation, and publish error
  all preserve `pending_summary`.
- [x] Clean quit/new/resume/fork publishes exactly one Episode; reload does not.
- [x] Manual rollover publishes or leaves pending the old Capture and always
  starts exactly one replacement after successful sealing.
- [x] Empty sealed Capture makes no model call.
- [x] Retry one and retry queue behavior.

## Acceptance criteria

- [x] A normal Pi work interval with modified files creates one queryable
  Episode before shutdown or rollover completes, or leaves an explicitly
  pending Capture.
- [x] The Episode summary covers the full Capture interval, not ancestors from
  before the Capture or only a recent tail.
- [x] Injected Madeleine context cannot recursively become memory.
- [x] No summary failure loses paths or prevents later retry.

## Excluded from this PR

Automatic background retry of older pending summaries, persistence of raw and
intermediate summary evidence, end-to-end crash hardening, additional summarizer
providers, and past transcript import.

## Implementation revisions and decision ledger

Listed least-confident first:

1. An initial fixed projection limit of 48,000 characters was removed after
   clarifying that one Capture spans an entire Pi session and commonly exceeds
   a model context window. The adapter now preserves all sanitized semantic
   evidence, sizes calls from `ctx.model.contextWindow`, and recursively
   summarizes chronological chunks only when required. It reserves 1,200 output
   tokens plus the larger of 1,024 tokens or 5% of the model context as an
   estimation margin. Each model call has its own 30-second timeout, so a large
   Capture can take longer than 30 seconds overall. Calls are sequential, and
   each hierarchy level must reduce the estimated evidence size or fail visibly.
   These values were not specified by the original plan.
2. Mutation projection intentionally stores only operation, path, status, and a
   failed-operation error capped at 1,000 Unicode characters. Complete write
   bodies, edit replacements, successful result prose, and reads mostly repeat
   code already available through navigation and were consuming session-scale
   summary context without adding durable intent.
3. L2's 300-800 token range is enforced through the prompt, not a local token
   counter. The strict parser validates shape, types, non-empty trimmed strings,
   and L1's Unicode limit as specified; adding a provider-specific tokenizer for
   an approximate prose target would add dependency and model-coupling cost.
4. Explicit retry reconstructs a pending Capture's `FinalizationDraft` by
   idempotently calling `capture.seal` with its persisted end cursor. This keeps
   Capture paths authoritative without adding another RPC method or exposing
   persistence details through the adapter.
5. `rpc.ts` and `package.json` were added to the implementation file set because
   the existing TypeScript client did not expose the already-supported
   `episode.publish` RPC and the two new runtime modules must ship in the npm
   package. No Go API or protocol method was added.
6. Entire CLI commit `60773bd4b89e487a897958b00a1d168a7ea5aa01` was inspected.
   Parent-chain branch selection and condensed Pi projection mechanics were
   adapted from `cmd/entire/cli/agent/pi/pijsonl/pijsonl.go`,
   `cmd/entire/cli/agent/pi/transcript.go`, and
   `cmd/entire/cli/transcript/compact/pi.go`, with their tests as references.
   Entire's line offsets, copied transcript bytes, and external summarizer CLI
   do not fit Madeleine's in-memory cursor boundaries and active Pi model. The
   existing MIT attribution in `NOTICE` and `harnesses/pi/NOTICE` already names
   this commit.
7. After implementation review, branch-summary entries were added to
   projection. Pi persists these as top-level `branch_summary` entries; the
   `branchSummary` role is only their derived model-context representation.
   Including the stored summary preserves useful context from an abandoned
   branch without traversing that branch's raw entries.
8. Pi compaction summaries are omitted from L1/L2 input. Pi retains the original
   messages in its append-only transcript, so including both duplicates content;
   additionally, a compaction can summarize ancestors from before the Capture
   boundary. The exact raw messages between the Capture cursors remain the
   source of truth.
