# Madeleine design

Status: accepted design for the Pi MVP  
Repository: `github.com/aduverger/madeleine`  
License: Apache-2.0

## North star

> The codebase is the memory index.

Madeleine attaches historical agent context to the code an agent is already
exploring. The model performs relevance selection by navigating the repository;
Madeleine does not try to infer relevance globally from the current prompt.

The ordinary flow is:

```text
agent reads a file
    -> exact repository path
    -> Episodes that previously modified that path
    -> compact L1 summaries added to the read result
    -> optional L2 lookup when one Episode appears useful
```

This is historical context as an extension of code navigation, not a generic
memory system. It adds the conversations that produced the code to the agent's
existing view of current code and Git history.

## Product philosophy

1. **Navigation is retrieval.** A file the model chose to inspect is already a
   high-quality retrieval key.
2. **Exact structure before semantic similarity.** The MVP uses exact paths and
   chronological history. There are no embeddings, rerankers, query rewriting,
   or memory agents in the normal path.
3. **Progressive disclosure.** L1 is automatic and cheap. L2 is explicit.
   Persisted compact and raw Transcript views provide final evidence rather than
   another generated summary level.
4. **File level is sufficient to test the thesis.** Function, range, symbol,
   folder, and rename identity are deferred until real usage demonstrates that
   file-level context is too noisy.
5. **Facts and summaries have different lifecycles.** A Capture records raw
   activity while work is unfinished. An Episode contains the distilled,
   immutable result after publication.
6. **Harnesses deliver context; the core owns semantics.** Pi is the first
   complete adapter, not the definition of Madeleine's domain model.
7. **Fail open.** Failure to find the binary, query SQLite, resolve a Git
   repository, or call a summarization model must never break the harness's
   normal tools.
8. **Optimize from evidence.** SQLite and direct process invocation remain until
   benchmarks show a real bottleneck.
9. **Reuse before rebuilding.** Entire is the primary implementation reference.
   Before writing a mechanic it already solves, inspect its code and tests and
   reuse or adapt them when they fit Madeleine's semantics and dependency
   budget.

## Implementation posture: reuse Entire

Madeleine owns a smaller product model than
[Entire](https://github.com/entireio/cli), but that does not make its
implementation greenfield. Entire's open-source Go codebase contains proven
work around agent lifecycles, Git integration, session capture, recovery, and
cross-platform CLI behavior. Reusing that work is preferred to independently
reimplementing the same mechanics.

Every implementation PR must:

1. inspect the relevant Entire implementation and tests before coding;
2. copy or adapt compatible code when it preserves Madeleine's accepted
   semantics and keeps the dependency surface small;
3. record the upstream repository path and commit used, and retain attribution
   or notices required by the upstream license;
4. document why reuse was rejected when equivalent Entire code was inspected
   but did not fit.

Madeleine's domain model, RPC contract, and invariants remain authoritative. Reuse
the smallest coherent implementation rather than importing unrelated Entire
architecture. Fresh code is valid when the semantics differ, but it should be a
deliberate conclusion after inspection, not the default starting point.

## Domain model

Repository, Conversation, Capture, Episode, and Transcript are the domain
nouns. Transcript is immutable evidence owned by a sealed Capture, not another
lifecycle unit. `live` and `history` describe lifecycle state and product
behavior; they are not additional types, stores, or persistence models.

### Repository

A Git repository known to Madeleine. It has a generated stable ID and one or
more aliases derived from worktree roots, the Git common directory, and a
normalized origin URL.

### Conversation

A harness-owned thread inside a Repository. Its identity is:

```text
(repository, harness, external conversation ID)
```

For Pi, the external ID is the persisted session UUID exposed by Pi's session
manager. Ephemeral Pi sessions receive a generated runtime ID and cannot be
recovered across process death.

### Capture

Operational state for one unfinished interval of work. A Capture stores
intentionally modified paths, timestamps, and transcript boundaries.

Capture states are:

```text
open -> pending_summary -> finalized
  \                         
   -> abandoned
```

Only `open` and `pending_summary` Captures are unfinished and recoverable.
Terminal Capture rows are retained as small idempotency records, while their raw
paths are removed after successful Episode publication.

### Transcript

Sanitized semantic evidence for one non-empty sealed Capture. Transcript entry
kinds and content are Madeleine-domain data, independent of any harness session
format. Each harness adapter translates its native entries into this canonical
representation; Episodes and Transcript retrieval can therefore carry history
between different harnesses. The Transcript's versioned chronological entries
become immutable during sealing. If their projection
exceeds the active model's context, Pi creates Capture-specific chronological
chunk summaries; the exact final compact evidence used for successful L1/L2 is
frozen atomically with Episode publication. The completed Transcript also stores
opaque source cursors and a generated stable ID. It never stores or depends on
the harness transcript-file path.

### Episode

An immutable historical record produced from one finalized Capture. It holds:

- L1 and L2 summaries;
- the exact modified paths;
- start and end timestamps;
- Conversation and harness identity;
- a generated Transcript ID for its immutable bounded evidence.

The core does not require one Conversation to map to one Episode. The Pi MVP
keeps one Capture open until a clean session transition, process exit, or manual
rollover. Reload and process restart reattach an open Capture for the same
Conversation. This keeps Episodes coarser than individual prompts while allowing
multiple deliberate Episodes in a long-running Conversation.

### Capture and Episode lifecycle

The two persistence stages have distinct responsibilities:

```text
Published Episode                        Unfinished Capture
durable, semantic                        operational, recoverable
Episode <-> paths                        Capture <-> paths
L1 / L2 / Transcript ID                  first/last activity
bounded Transcript                       transcript cursors
append-oriented                          tiny upserts
```

The invariant is:

> A Capture contains facts while work is happening. An Episode contains the
> distilled historical record once publication succeeds.

Active Capture activity is factual, not coordination intelligence. In a later
multiplayer system it may expose facts such as "Capture A is writing foo.go".
An orchestrator, not the Madeleine core, decides whether another agent should
warn, wait, communicate, or ignore that fact.

## Lifecycle

### Normal Pi run

```text
session_start
    -> reattach the Conversation's open Capture, or start one with a transcript cursor

successful edit/write
    -> record exact path immediately

session_shutdown (quit/new/resume/fork)
    -> project the cursor-bounded sanitized semantic Transcript
    -> atomically persist structured Transcript entries and seal Capture
    -> abandon if the structured path set is empty
    -> fit projection to the active model through Capture-specific chunk summaries when needed
    -> generate L1/L2 from the final compact evidence
    -> atomically persist compact evidence and publish Episode
```

Summary or publication failure leaves the sealed Capture in
`pending_summary`; it never discards recoverable state.

### Hot reload

Pi `/reload` emits shutdown and start events, but it is still the same runtime
interval for Madeleine:

```text
session_shutdown(reason=reload) -> do not seal
session_start(reason=reload)    -> reattach the same open Capture
```

The Capture ID and already-injected paths are persisted through Pi custom
entries so a reload does not duplicate historical context.

### Tree navigation

Every non-no-op `/tree` navigation is a Capture boundary. Madeleine seals the
old interval at the source leaf and opens a temporary source Capture before Pi
runs an optional branch summary. If summarization fails or is cancelled, that
Capture continues recording on the unchanged branch. After successful
navigation, Madeleine abandons the unused temporary Capture and starts the
destination Capture. A generated branch summary is the first semantic entry
inside that destination boundary. This keeps paths and intent from an abandoned
branch together instead of mixing them into the destination branch's evidence.

### Crash and resume

An abrupt exit leaves the current Capture `open`. Opening the same persisted Pi
Conversation validates and reattaches that Capture, preserving its paths,
transcript start boundary, and injection state. No filesystem changes made while
Pi was stopped are inferred or attributed.

A clean shutdown seals the Capture, so a later resume starts a new one. Sealed
`pending_summary` Captures are retried oldest-first in one background worker.
Each is attempted once per runtime, with at most one recovery model call active.
Recovery is cancelled on shutdown and failures remain pending.

### Manual rollover

`/madeleine rollover` waits until Pi is idle, seals the current Capture through
the same finalization path used by session shutdown, and starts a new Capture in
the same Conversation. Once summary generation is available, successful sealing
publishes an Episode before reporting completion; a recoverable summary failure
leaves the old Capture pending while the new Capture records subsequent work.
The adapter composes the existing `capture.seal` and `capture.start` RPC methods;
there is no misleadingly atomic rollover method in the core API.

### Clean summary timeout

Summary generation uses the active Pi model through `ctx.modelRegistry`. Each
model call has a 30-second abort timeout. A session-scale Capture may require
several sequential chunk calls plus final synthesis, so the complete attempt can
take longer. Timeout, missing authentication, invalid JSON, truncated output, or
an empty response leaves the Capture pending for later automatic or explicit
retry.

## Historical context

### L1

L1 is one or two sentences, at most 400 Unicode characters. It states what the
Episode accomplished and why, sufficiently clearly for relevance screening.

For an exact path lookup, Madeleine returns at most five Episodes, newest first.
The renderer includes Episode IDs so the model can request more detail.

### L2

L2 targets roughly 300-800 tokens and covers:

- goal and outcome;
- important decisions and rationale;
- rejected alternatives when relevant;
- implementation details needed to continue safely;
- tests, caveats, and unfinished follow-ups.

The Pi adapter exposes `madeleine_episode { episode_id }`. It returns L2, paths,
timestamps, and a Transcript ID only when the Episode belongs to the current
Repository.

### Transcript evidence

Every non-empty sealed Capture owns one immutable, sanitized, cursor-bounded
Transcript in SQLite. It has two representations of the same evidence:

- `compact` is the exact bounded text passed to the L1/L2 model;
- `raw` is the fuller chronological structured entry sequence, paged by stable
  position.

Raw entries use a versioned Madeleine-domain vocabulary: `user`, `assistant`,
`branch_summary`, and `mutation`. They include complete user and assistant text,
branch summaries, and structured mutation operation/path/status metadata.
Authoritative normalized Capture paths are added when rendering compact evidence
and remain on the Episode. Harness-native entry names and file layouts do not
cross the adapter boundary.
Failed mutations retain a short error. Compact evidence is that semantic
projection when it fits, otherwise ordered Capture-specific summaries of it.
Both exclude Pi compaction entries, read calls and outputs, write content, edit
old/new bodies, successful mutation result prose, image/audio bodies, thinking,
internal custom state, and recursively injected Madeleine context. Pi's
append-only raw messages remain available across compaction, while a compaction
summary may contain ancestors from before the Capture boundary.

The Pi adapter exposes `madeleine_transcript` for repository-scoped compact or
raw retrieval. SQLite returns stable position-based pages bounded by both entry
count and encoded size, except that one indivisible entry is always returned;
the adapter then exposes the largest complete prefix
that fits Pi's smaller escaped output bound and points to the first hidden
entry. Pagination remains visible even when one oversized entry itself must be
truncated. These are evidence views, not generated L3/L4
summaries, and no retrieval depends on the original Pi session file.

### Injection safety

Historical summaries and Transcript entries are untrusted data. The Pi renderer:

- encloses them in explicit Madeleine markers;
- says that they are reference material, not instructions;
- escapes or structurally separates stored text;
- injects a path only once per Capture;
- strips Madeleine blocks from later Episode-summary input.

If lookup or rendering fails, the original tool result is returned unchanged.

## Capture and path attribution

### Structured tool events

Successful Pi `edit` and `write` results are the primary source. A failed tool
call never records a path. The first successful mutation of a path is persisted
immediately; repeat touches refresh `last_seen_at` at most every 30 seconds.

Reads are retrieval keys only. They are not stored as Capture activity or
Episode history in the MVP.

### Unstructured changes

The MVP intentionally does not infer Episode attribution from Git or filesystem
state. Shell scripts, generators, formatters, external commits, human edits, and
other sessions can change files without demonstrating that the current agent
reasoned about them. Attaching all such paths would reduce retrieval precision.

A source file changed exclusively through an opaque shell command can therefore
be missed. Each harness should report successful mutations from its typed edit
and write tools. Broader attribution may later be implemented as a separate Go
package if dogfooding shows that these false negatives matter more than noisy
history.

## Repository and path identity

Repository IDs are generated, not derived cryptographically from a remote. New
locations are matched in this order:

1. Git common-directory alias;
2. worktree-root alias;
3. normalized `remote.origin.url` alias;
4. otherwise register a new Repository.

Origin normalization removes transport/user syntax and a trailing `.git`,
lowercases the host, and preserves repository-path case. A collision between
distinct known Repositories is an explicit conflict rather than an arbitrary
merge.

File keys are lexical, case-preserving, slash-separated, repository-relative
paths. Absolute tool paths are converted relative to the resolved worktree.
Empty paths and paths escaping the worktree are rejected. File symlinks retain
their repository path rather than becoming the identity of their external
target.

## Storage

### Choice

The MVP uses one user-global SQLite database in WAL mode through
`database/sql` and the CGo-free `modernc.org/sqlite` driver. This produces a
simple macOS/Linux binary without requiring a server or C toolchain.

Default location:

```text
macOS: ~/Library/Application Support/madeleine/madeleine.db
Linux: $XDG_DATA_HOME/madeleine/madeleine.db
       or ~/.local/share/madeleine/madeleine.db
```

`MADELEINE_HOME` overrides the directory.

SQLite settings:

- WAL journal mode;
- foreign keys enabled;
- five-second busy timeout on every connection;
- short explicit transactions;
- numbered embedded migrations;
- private data directory permissions where the platform supports them.

### Logical schema

```text
schema_migrations
repositories
repository_aliases
conversations
captures
capture_paths
transcripts
transcript_entries
episodes
episode_files
```

`capture_paths` uses independently owned `(capture_id, path)` rows. Multiple
agents touching one path therefore do not mutate one shared aggregate row.
Episode paths are likewise append-oriented through `(episode_id, path)`
relationships. Each non-empty sealed Capture owns one Transcript whose
versioned structured entries are frozen during sealing and whose exact final
compact summary evidence is retained atomically with the published Episode.

SQLite is canonical for the MVP. There is no second canonical harness-file
reference, rebuildable materialized index, or per-session journal file. Export
formats can be added later without a migration trap.

### Concurrency trajectory

SQLite WAL permits concurrent readers with a serialized writer. Madeleine's
write shape is favorable: tiny independent upserts and short finalization
transactions. Contention is handled with the busy timeout and explicit errors,
then measured before adding infrastructure.

If one workstation eventually has enough writers to make this insufficient,
the next step is a local single-writer broker that batches publications. Shared
multi-machine operation should use a server store such as PostgreSQL. Turso's
concurrent embedded database direction remains worth watching, but it is not an
MVP dependency.

## Application boundary

Madeleine is a standalone application. All Go implementation packages are
private; versioned JSON RPC is the sole supported external API.

```text
cmd/madeleine        minimal executable entry point
internal/cli         process setup, command selection, doctor, and version
internal/rpc         JSON protocol and Service dispatch
internal/madeleine   product rules and orchestration
internal/store       SQLite persistence and migrations
internal/gitcmd      Git process execution for repository identity
internal/repopath    repository-relative path normalization
```

The CLI delegates RPC calls through `internal/rpc` and runs doctor checks
against `internal/madeleine`; both paths use the same private application
service. `internal/store` contains SQL and persistence records and never imports
the product layer. SQLite remains the only implementation; there is no storage
interface or configurable backend.

The private application service retains one canonical operation vocabulary for
RPC dispatch:

```go
Open(context.Context, Options) (*Service, error)
ResolveRepository(context.Context, string) (Repository, error)

(*Service).StartCapture(context.Context, StartCaptureRequest) (Capture, error)
(*Service).GetCapture(context.Context, CaptureID) (Capture, error)
(*Service).RecordWrite(context.Context, RecordWriteRequest) error
(*Service).ListPendingCaptures(context.Context, PendingCaptureQuery) ([]Capture, error)
(*Service).SealCapture(context.Context, SealCaptureRequest) (FinalizationDraft, error)
(*Service).PublishEpisode(context.Context, PublishEpisodeRequest) (Episode, error)
(*Service).AbandonCapture(context.Context, CaptureID) error
(*Service).ContextForPaths(context.Context, ContextRequest) ([]FileContext, error)
(*Service).GetEpisode(context.Context, EpisodeRequest) (EpisodeDetail, error)
(*Service).GetTranscript(context.Context, TranscriptRequest) (TranscriptView, error)
(*Service).Close() error
```

Internal sentinel errors support `errors.Is`; the RPC layer maps them to stable
protocol error codes.

The CLI uses one process per operation:

```text
madeleine rpc <method>
```

It reads one protocol-versioned JSON object from stdin, writes exactly one JSON
object to stdout, and sends diagnostics only to stderr.

```json
{"protocol_version":1,"ok":true,"result":{}}
{"protocol_version":1,"ok":false,"error":{"code":"...","message":"..."}}
```

RPC methods mirror the Service operations. `madeleine doctor` is human-readable;
`madeleine doctor --json` uses the same envelope.

## Pi package

The Pi adapter is a TypeScript package in the same repository:

```text
package: @aduverger/madeleine-pi
runtime: Node >= 22.19.0
root: harnesses/pi
entry: index.ts
```

Harness integrations live under `harnesses/<harness>`. Each directory owns
that harness's lifecycle, native-entry interpretation, canonical bounded
Transcript projection, tools, and presentation. SQLite and RPC expose the same
Madeleine-domain Transcript entries and Episodes regardless of their producing
harness. The versioned Madeleine CLI remains their shared boundary; no
cross-harness adapter
framework is introduced before a second implementation demonstrates shared
code. Each harness owns its packaging under its directory and does not require
other harnesses to use npm or Pi's package format.

The Pi integration uses the current `@earendil-works/pi-coding-agent`,
`@earendil-works/pi-ai`, and `typebox` peer packages. Pi loads TypeScript
directly. It invokes the Go binary through an argument-vector child process,
never through a shell.

Installation remains deliberately separate:

```text
go install github.com/aduverger/madeleine/cmd/madeleine@v0.1.0
pi install npm:@aduverger/madeleine-pi@0.1.0
```

`MADELEINE_BIN` overrides binary discovery. A missing binary disables the
adapter for that run and emits at most one non-disruptive notification.

## MVP definition

The MVP is complete when the Pi vertical slice proves all of the following:

1. A Pi run edits files and cleanly produces one Episode.
2. A later run reading one of those exact paths automatically receives recent
   L1 history.
3. The model can explicitly retrieve the Episode's L2.
4. The model can retrieve the Episode's persisted compact Transcript and page
   through its raw Transcript without reading a harness session file.
5. `/reload` retains the current Capture and does not duplicate context.
6. A crash leaves the open Capture recoverable; reopening the same Conversation
   reattaches it without attributing changes made while Pi was stopped.
7. Only successful structured mutation events become Episode paths; opaque
   shell, generated, formatted, human, and other-session changes are excluded.
8. Empty runs create no Episode.
9. Missing dependencies, SQLite contention, repository-discovery errors, and
   model failures do not interfere with Pi's normal operation or silently
   discard pending state.
10. macOS and Linux checks pass.

## Explicit MVP non-goals

- embeddings, vector databases, FTS, semantic ranking, or global memory search;
- folder records, prefix retrieval, symbol/range/function identity, or rename
  lineage;
- per-file summaries or automatic relevance filtering beyond newest-first;
- unsanitized harness-file copies or transcript parsing for old external
  histories;
- Claude, Codex, Cursor, Gemini, OpenCode, or other harness adapters;
- Agent Trace or Git AI import/export;
- real-time read presence or multi-agent steering UI;
- daemon, socket service, network API, Postgres, replication, or team sharing;
- web UI, MCP server, Git/filesystem reconciliation, commit attribution, or
  line-survival tracking;
- configurable storage backends or summarizer-provider abstraction;
- a public Go SDK or importable root package.

## Future direction

Future work is driven by dogfooding and measurements:

1. Add other harness adapters through the versioned CLI while keeping their
   lifecycle details outside the core.
2. Backfill past harness transcripts into bounded Transcript records using
   isolated importers.
3. Derive folder history by aggregating descendant exact paths.
4. Evaluate opt-in Git or filesystem mutation attribution when structured-tool
   false negatives prove more harmful than noisy associations.
5. Add rename/path lineage through Git when exact-path misses prove painful.
6. Add file ranges or symbols only when hotspot files create measurable noise.
7. Evolve context selection from newest-first using observed signals such as
   changed-line count, Episode creation, or survival; never let embeddings
   establish causal provenance.
8. Expose active Capture activity to orchestrators for same-machine agent
   coordination.
9. Add a single-writer local broker only after SQLite contention is measured.
10. Add PostgreSQL and authenticated sharing when activity spans machines.
11. Add Agent Trace import/export as an interoperability layer, not an internal
    schema.

## Rejected alternatives

### Temporal provenance graph

Episodes, code-entity incarnations, decision objects, lineage confidence, and
patch-survival graphs are much richer than needed to test the core behavior.
They were rejected in favor of `Episode <-> exact file path`.

### Function-level or folder-level canonical memory

Function identity creates parsing and refactor-lineage requirements. Stored
folder memory duplicates repository structure. Both remain derivable or
additive layers later.

### Embeddings and rankers

They recreate the global retrieval problem that Madeleine is designed to avoid.
The model already signals relevance by opening code.

### Adopting SessionWiki, Entire, OpenTraces, or Git AI wholesale

These projects are useful research and implementation references. None matches
the deliberately small local path-context kernel, and adopting one would inherit
unrelated architecture and maintenance risk. Madeleine owns its core.

This rejects an upstream project as Madeleine's foundation; it does not reject
code reuse. Entire in particular is the preferred source for proven mechanics.
Compatible implementations and tests should be adapted under their applicable
license instead of rewritten merely to keep the repository independent.

### Canonical JSON plus rebuildable SQLite

Inspectable session objects and a disposable index are attractive, but they
duplicate persistence, add projectors and recovery paths, and provide no
necessary MVP capability. SQLite alone is sufficient and exportable later.

### Per-session journal files

Private journals reduce write contention but introduce another state machine and
recovery source. Unfinished Capture rows in SQLite already provide crash
durability with tiny upserts.

### Alternative embedded databases

bbolt maps naturally to prefix keys but requires hand-built secondary indexes.
Pebble and Badger solve much larger write-volume problems. DuckDB targets
analytics and has a worse multi-process write fit. SQLite has the best balance
of local concurrency, inspectability, migrations, and contributor familiarity.

### Public Go library

No external Go consumer is known. A public package would create a second
compatibility surface beside JSON RPC and shape internal dependencies around
hypothetical embedding. An SDK can be introduced later if a concrete consumer
justifies it.

### Daemon first

A daemon adds startup, supervision, socket, versioning, and cross-platform IPC
problems before any measured need. A short-lived Go binary is the simpler
reference boundary.

## Decision ledger

| ID | Decision | Status | Consequence |
|---|---|---|---|
| D-001 | The codebase is the memory index. | Locked | Normal retrieval is exact structural lookup triggered by navigation. |
| D-002 | MVP granularity is exact file path. | Locked | No symbols, ranges, folders, or rename identity. |
| D-003 | Use one Episode-level L1/L2 plus persisted bounded Transcript evidence. | Locked | No per-file summaries and no generated L3/L4. |
| D-004 | Separate Conversation, Capture, and Episode. | Locked | Harness resumes do not overload the historical unit. |
| D-005 | Keep unfinished Capture facts separate from immutable Episode history. | Locked | Capture activity can later support presence without defining orchestration policy. |
| D-006 | One Pi work interval produces one Episode; clean shutdown or manual rollover ends the interval. | MVP policy | Episodes span prompts and may survive process restart without becoming session-long by necessity. |
| D-007 | Persist successful writes during the run. | Locked | Crashes retain useful Capture state. Reads are not persisted. |
| D-008 | SQLite WAL is the sole MVP persistence layer. | Locked | No canonical JSON, journal files, daemon, or alternate backend. |
| D-009 | Use Git CLI reconciliation as a safety net. | Superseded by D-024 | Exhaustive change detection produced noisy file-to-context associations and lifecycle edge cases. |
| D-010 | Build a harness-neutral Go library plus JSON CLI. | Superseded by D-022 | No concrete external Go consumer justified maintaining two APIs. |
| D-011 | Pi is the first complete reference adapter. | Locked | The final MVP is validated in the user's primary harness. |
| D-012 | Inject five newest L1s per exact path. | MVP policy | Context selection is deterministic and isolated for future evolution. |
| D-013 | Generate summaries with Pi's active model. | Locked | No separate model-provider configuration in MVP. |
| D-014 | Reload and process restart reattach an open Capture for the same Conversation; clean resume starts a new one. | Locked | Crashes preserve one work interval without attributing downtime filesystem changes. |
| D-015 | Retry sealed pending-summary Captures in the background, sequentially. | Locked | Recovery does not delay the current Pi work interval. |
| D-016 | All integration failures are fail-open. | Locked | Madeleine cannot break normal code-agent behavior. |
| D-017 | Treat injected memory as untrusted data. | Locked | Stored summaries cannot silently become privileged instructions. |
| D-018 | Agent Trace is future interoperability only. | Deferred | Internal persistence is not shaped around an external attribution RFC. |
| D-019 | Optimize only after concurrency benchmarks. | Locked | A broker/Postgres is a measured evolution, not MVP scaffolding. |
| D-020 | Support macOS and Linux first. | Locked | CGo-free SQLite keeps later Windows support feasible. |
| D-021 | Inspect Entire and reuse compatible code before rebuilding equivalent mechanics. | Locked | Each PR records reused provenance or why reuse did not fit; Madeleine's semantics remain authoritative. |
| D-022 | Ship Madeleine as a standalone application with private Go packages and versioned JSON RPC as its external API. | Locked | Harnesses share one protocol; internal package structure can evolve without Go API compatibility constraints. |
| D-023 | Organize and package harness integrations under `harnesses/<harness>`. | Locked | Each harness owns its integration mechanics and release format; only proven common behavior is shared through the CLI or later extraction. |
| D-024 | Attribute Episode paths only from successful structured harness mutation events in the MVP. | Locked | Git/filesystem reconciliation is deferred; retrieval precision is preferred over exhaustive change detection. |
| D-025 | Persist one sanitized cursor-bounded Transcript per non-empty Capture and expose compact/raw views by generated ID. | Locked | Episode evidence survives harness-file deletion; compact is the exact final L1/L2 evidence after any Capture-specific chunking and raw is paged structured evidence. |

## Reference documentation

- [Go module dependency management](https://go.dev/doc/modules/managing-dependencies)
- [Go toolchain selection](https://go.dev/doc/toolchain)
- [Organizing a Go module](https://go.dev/doc/modules/layout)
- [Go package names](https://go.dev/blog/package-names)
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- [Entire CLI source](https://github.com/entireio/cli)
- [Pi extension API](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/extensions.md)
- [Pi package format](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/packages.md)
