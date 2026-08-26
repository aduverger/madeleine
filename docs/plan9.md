# Plan 9: Pi summary generation and clean publication

PR scope: one PR  
Depends on: `plan8.md`  
Design decisions: D-003, D-006, D-013, D-016, D-017, D-021

## Goal

Complete the clean-run pipeline: seal a Capture, project its bounded Pi
transcript, generate validated L1/L2 with the active model, and publish an
immutable Episode. Any failure must leave `pending_summary` recoverable.

## Entire reuse gate

- [ ] Inspect the relevant `entireio/cli` implementation and tests before
  coding this PR.
- [ ] Prefer copying or adapting compatible mechanics to reimplementation;
  Madeleine's interfaces and invariants remain authoritative.
- [ ] Record reused upstream paths and commit, and retain required attribution.
- [ ] If equivalent code is not reused, record the concrete mismatch in the PR.

## Files

```text
extensions/madeleine/transcript.ts
extensions/madeleine/summary.ts
extensions/madeleine/lifecycle.ts
extensions/madeleine/commands.ts
extensions/madeleine/*.test.ts
```

## Transcript boundaries

- [ ] Read Pi session entries through `ctx.sessionManager.getEntries()`.
- [ ] Locate the sealed Capture's start and end entry IDs.
- [ ] Walk parent relationships from the end cursor to reconstruct that branch,
  then reverse to chronological order.
- [ ] Exclude the start cursor itself and all ancestors before it; include the
  end entry.
- [ ] Fail with a recoverable summary error when required cursors cannot be
  resolved. Do not silently summarize the whole Conversation.

## Projection policy

- [ ] Include user text, assistant text, compaction summaries, and mutation
  tool calls/results between the boundaries.
- [ ] Include final reconciled paths from `FinalizationDraft` as authoritative
  metadata.
- [ ] Omit read tool outputs, image/audio payloads, internal custom state, and
  unrelated tool-result bulk.
- [ ] Strip complete `<madeleine-context ...>...</madeleine-context>` blocks
  before constructing summary input.
- [ ] Retain mutation tool names, relevant inputs, success/failure, and bounded
  textual results.
- [ ] Bound individual entries and total projection size with named constants;
  preserve the first user goal, compaction summaries, and latest relevant
  entries when truncation is necessary.
- [ ] Mark the projected transcript as untrusted source data in the prompt.

## Summary contract

The model must return exactly one JSON object and no Markdown fence:

```json
{
  "l1": "One or two sentences, maximum 400 characters.",
  "l2": "A 300-800 token brief covering goal, decisions, actions, tests and caveats."
}
```

- [ ] Put the prompt text and a `summary_prompt_version = 1` constant in one
  module.
- [ ] Validate exact object shape, string types, trimmed non-empty values, and
  the L1 Unicode limit.
- [ ] Reject prose surrounding JSON, missing/extra fields, code fences, empty
  values, and oversized L1 rather than repairing or truncating silently.
- [ ] Do not persist model name, prompt text, hidden reasoning, or raw response
  in the MVP.

## Active-model call

- [ ] Use `ctx.model`; if absent or unauthenticated, leave the Capture pending.
- [ ] Call `ctx.modelRegistry.complete` with one user message, the active model,
  `cacheRetention: "none"`, a fresh UUIDv7 session ID, and `maxTokens: 1200`.
- [ ] Use an AbortController with a 30-second timeout and combine it with any
  lifecycle cancellation.
- [ ] Extract text content only and validate the strict JSON contract.
- [ ] Never insert the summarization request/response into the active Pi
  Conversation.

## Clean finalization

- [ ] For every non-reload shutdown, stop background work, seal the current
  Capture, and return immediately for an empty/abandoned draft.
- [ ] Project, summarize, then call `episode.publish` with the Capture ID and
  validated L1/L2.
- [ ] Treat an identical publish retry as success.
- [ ] On model, parse, timeout, or publish failure, notify once and leave
  `pending_summary` unchanged.
- [ ] Clear current in-memory Capture state only after empty abandonment or
  confirmed Episode publication.

## Explicit retry command

- [ ] Add `/madeleine retry [capture-id]`.
- [ ] With an ID, require it to be pending in the current Repository and
  Conversation.
- [ ] Without an ID, retry pending Captures for the current Conversation
  oldest-first, one at a time.
- [ ] Reconstruct each Capture's transcript range independently.
- [ ] Report per-Capture success/failure without abandoning failures.

## Tests

- [ ] Linear branch projection and exclusion of pre-start entries.
- [ ] Forked branch reconstruction using parent IDs.
- [ ] Compaction entries and a Capture spanning compaction.
- [ ] Read-output omission, mutation retention, binary-content omission, and
  recursive Madeleine-context stripping.
- [ ] Projection truncation preserves the required goal/summary/tail policy.
- [ ] Valid summary, surrounding prose, code fence, extra key, missing key,
  empty value, Unicode-overlong L1, and empty model response.
- [ ] No model/auth, model rejection, timeout, cancellation, and publish error
  all preserve `pending_summary`.
- [ ] Clean quit/new/resume/fork publishes exactly one Episode; reload does not.
- [ ] Empty sealed Capture makes no model call.
- [ ] Retry one and retry queue behavior.

## Acceptance criteria

- [ ] A normal Pi run with modified files creates one queryable Episode before
  shutdown completes or leaves an explicitly pending Capture.
- [ ] The Episode summary covers only the Capture interval, not the whole Pi
  Conversation.
- [ ] Injected Madeleine context cannot recursively become memory.
- [ ] No summary failure loses paths or prevents later retry.

## Excluded from this PR

Automatic crash recovery on startup, concurrent recovery/current-run
coordination, additional summarizer providers, and past transcript import.
