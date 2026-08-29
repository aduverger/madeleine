<p align="center">
  <img src="./assets/madeleine-logo.png" alt="Madeleine" width="760">
</p>

<p align="center">
  <strong>Historical context for coding agents, attached to the files they explore.</strong><br>
</p>

Madeleine is a local-first memory layer for coding agents. When an agent reads
or edits a file, it surfaces the past agent sessions that changed that exact
file and the decisions that shaped it.

The agent already selects relevant context by navigating the repository.
Madeleine adds history to what it is looking at instead of turning memory into
another search problem.

> [!NOTE]
> Madeleine is currently in the design and implementation phase. The Pi MVP is
> specified but not yet available as a release.

## The idea

Most agent-memory systems begin with a prompt, search a large memory collection,
rank the results, and inject what appears relevant.

Madeleine inverts that flow:

```text
agent reads src/payments/refund.go
                ↓
       exact repository path
                ↓
 Episodes that previously changed the file
                ↓
 compact historical context beside the code
```

The file path is the retrieval key. There is no embedding search, query
rewriting, reranker, or separate memory agent in the normal path.

This is closer to extending code navigation than building a general-purpose
memory database.

## Simple primitives

Madeleine uses four product-domain objects:

- **Conversation** — a harness-owned thread, such as a Pi session.
- **Capture** — durable operational state for one unfinished interval of work.
- **Episode** — an immutable historical record produced from a finalized
  Capture.
- **Transcript** — immutable sanitized evidence for one non-empty sealed
  Capture, addressed by a generated ID.

Their responsibilities remain separate:

| Unfinished Capture | Published Episode |
|---|---|
| Records what is happening now | Explains what happened and why |
| Stores modified paths and activity timestamps | Stores exact paths, L1/L2, and a Transcript ID |
| Mutable while work is in progress | Immutable after publication |

`live` and `history` are descriptions of lifecycle state, not additional domain
objects or storage layers.

For the Pi MVP, one Capture spans work in a Pi Conversation until a clean
session transition, process exit, or explicit `/madeleine rollover`. Hot reload
and restart after an abrupt process exit reattach the same open Capture. This
keeps Episodes deliberately coarse while allowing the user to create a boundary
inside a long-running session.

## Progressive context

Episode context is disclosed in layers:

1. **L1** — one or two sentences, attached automatically when a file is read.
2. **L2** — a longer brief containing goals, decisions, rationale, actions,
   tests, and caveats; requested only when an Episode looks useful.
3. **Compact Transcript** — the exact final evidence used to generate L1/L2,
   after Capture-specific chunking when a session exceeds the active model's
   context window.
4. **Raw Transcript** — the fuller sanitized, cursor-bounded semantic entries,
   available in pages.

Madeleine persists both Transcript views by generated ID. Their entry format is
owned by Madeleine rather than Pi, Claude Code, Codex, or another harness. Each
adapter translates its native transcript into the same canonical representation,
so Episode summaries and Transcript evidence can be consumed across harnesses.
Madeleine does not depend on or expose the original harness transcript-file path.

The default lookup is intentionally deterministic: the five newest Episodes for
the exact path, newest first. The model decides which history matters.
More elaborated ranking mechanisms will be explored in the future.

## How an Episode is created

```text
agent run starts or resumes
    ↓
Capture stores successful structured file mutations
    ↓
Capture is sealed on exit, session transition, or manual rollover
    ↓
sanitized semantic Transcript entries are persisted
    ↓
Capture evidence is chunked only when it exceeds the active model context
    ↓
L1 and L2 are generated from final compact evidence
    ↓
compact evidence and immutable Episode are published atomically
```

Persisted Capture paths make unfinished work recoverable after a crash. Reopening
the same Conversation reattaches its open Capture. Sealed Captures whose summary
or publication failed remain pending for retry.

## MVP

The first complete integration targets [Pi](https://github.com/earendil-works/pi).
It will provide:

- a standalone Go CLI with a versioned JSON protocol;
- local SQLite storage in WAL mode;
- a thin Pi TypeScript extension;
- automatic L1 context after successful file reads;
- explicit L2 and compact/raw Transcript retrieval through Pi tools;
- semantic Transcript evidence without read output or edit/write file bodies;
- immediate recording of successful `edit` and `write` operations;
- deliberate Capture rollover within a long-running Pi session;
- clean shutdown, hot-reload, and crash-reattachment semantics;
- fail-open behavior so Madeleine never breaks normal agent tools;
- macOS and Linux support.

The Go binary and Pi package will be installed separately. No daemon or network
service is required.

## Reuse before rebuilding

Madeleine is deliberately small, but it is not built in isolation. The
[Entire CLI](https://github.com/entireio/cli) is the primary implementation
reference for mechanics it has already solved, including agent lifecycles, Git
integration, session capture, recovery, and cross-platform behavior.

When Entire code or tests fit Madeleine's semantics, we prefer adapting them over
writing another implementation. Madeleine still owns its smaller domain model
and avoids importing unrelated architecture. Reused work keeps its required
attribution and provenance.

## Deliberate non-goals

The MVP does **not** include:

- embeddings, vector databases, semantic ranking, or global memory search;
- function, symbol, range, folder, or rename-level identity;
- unsanitized harness-file copies or bulk import of old agent histories;
- multiplayer coordination or shared remote storage;
- a daemon, web service, or user interface;
- Agent Trace or Git AI interoperability;
- exhaustive filesystem or Git change attribution for shell commands,
  generators, formatters, or human edits.

These are not rejected forever. We want to focus first on validating file-level history.

## Architecture

```text
Pi extension
    ↓ versioned JSON over stdin/stdout
Madeleine CLI
    ├── private Go application and SQLite Store
    └── system Git: repository identity
```

SQLite is the sole source of truth for the MVP. Its write pattern is small,
append-oriented, and friendly to concurrent readers. If future same-machine
contention is measured, a single-writer broker can be added. Shared multi-machine
operation would use a server store such as PostgreSQL.

## Direction after the MVP

Likely next steps are:

1. Add more harness adapters without changing the core model.
2. Import historical sessions into bounded Transcript records.
3. Evaluate opt-in Git or filesystem attribution if structured mutation events
   miss context that agents demonstrably need.
4. Derive folder history and follow Git renames when exact paths prove limiting.
5. Add range or symbol context only for repositories where large hotspot files
   create measurable noise.
6. Expose active Capture activity to orchestrators for multi-agent awareness.
7. Add Agent Trace import/export as an interoperability layer, not an internal
   storage model.

## Project documents

- [`design.md`](./docs/design.md) contains the philosophy, accepted decisions,
  lifecycle, interfaces, MVP boundaries, and rejected alternatives.
- [`plan1.md`](./docs/plan1.md) through [`plan12.md`](./docs/plan12.md) form the
  stacked implementation plan, with one reviewable pull request per file.

## License

Madeleine is licensed under the [Apache License 2.0](./LICENSE).
