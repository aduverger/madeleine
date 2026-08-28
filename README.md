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

Madeleine uses three domain objects:

- **Conversation** — a harness-owned thread, such as a Pi session.
- **Capture** — durable operational state for one unfinished interval of work.
- **Episode** — an immutable historical record produced from a finalized
  Capture.

Their responsibilities remain separate:

| Unfinished Capture | Published Episode |
|---|---|
| Records what is happening now | Explains what happened and why |
| Stores modified paths and activity timestamps | Stores exact paths and L1/L2 summaries |
| Mutable while work is in progress | Immutable after publication |

`live` and `history` are descriptions of lifecycle state, not additional domain
objects or storage layers.

For the Pi MVP, one Pi runtime produces one Episode. Resuming the same
Conversation starts a new Capture rather than extending old history. This keeps
completed work immutable while allowing future checkpoints or longer-running
workflows.

## Progressive context

Episode context is disclosed in layers:

1. **L1** — one or two sentences, attached automatically when a file is read.
2. **L2** — a longer brief containing goals, decisions, rationale, actions,
   tests, and caveats; requested only when an Episode looks useful.
3. **Raw transcript** — the original harness-owned conversation remains the
   evidence source. Madeleine stores a reference rather than copying it.

The default lookup is intentionally deterministic: the five newest Episodes for
the exact path, newest first. The model decides which history matters.
More elaborated ranking mechanisms will be explored in the future.

## How an Episode is created

```text
agent run starts
    ↓
Capture stores successful file writes
    ↓
Git reconciliation catches shell tools, generators, and commits
    ↓
Capture is sealed
    ↓
L1 and L2 are generated from the bounded conversation interval
    ↓
immutable Episode is published
```

Persisted Capture paths make unfinished work recoverable after a crash. A resumed run gets a
new Capture immediately, while older pending Captures are finalized in the
background.

## MVP

The first complete integration targets [Pi](https://github.com/earendil-works/pi).
It will provide:

- a standalone Go CLI with a versioned JSON protocol;
- local SQLite storage in WAL mode;
- a thin Pi TypeScript extension;
- automatic L1 context after successful file reads;
- explicit L2 retrieval through a Pi tool;
- immediate recording of successful `edit` and `write` operations;
- non-mutating Git reconciliation at finalization;
- clean shutdown, hot-reload, and crash-recovery semantics;
- fail-open behavior so Madeleine never breaks normal agent tools;
- macOS and Linux support.

The Go binary and Pi package will be installed separately. No daemon or network
service is required.

## Reuse before rebuilding

Madeleine is deliberately small, but it is not built in isolation. The
[Entire CLI](https://github.com/entireio/cli) is the primary implementation
reference for mechanics it has already solved, including agent lifecycles, Git
integration, session capture, recovery, and cross-platform behavior.

When Entire code or tests fit Madeleine's semantics, we prefer adapting them to
writing another implementation. Madeleine still owns its smaller domain model
and avoids importing unrelated architecture. Reused work keeps its required
attribution and provenance.

## Deliberate non-goals

The MVP does **not** include:

- embeddings, vector databases, semantic ranking, or global memory search;
- function, symbol, range, folder, or rename-level identity;
- copied transcript storage or bulk import of old agent histories;
- multiplayer coordination or shared remote storage;
- a daemon, web service, or user interface;
- Agent Trace or Git AI interoperability.

These are not rejected forever. We want to focus first on validating file-level history.

## Architecture

```text
Pi extension
    ↓ versioned JSON over stdin/stdout
Madeleine CLI
    ├── private Go application and SQLite Store
    └── system Git: repository identity and final reconciliation
```

SQLite is the sole source of truth for the MVP. Its write pattern is small,
append-oriented, and friendly to concurrent readers. If future same-machine
contention is measured, a single-writer broker can be added. Shared multi-machine
operation would use a server store such as PostgreSQL.

## Direction after the MVP

Likely next steps are:

1. Add more harness adapters without changing the core model.
2. Import historical sessions from existing harness transcripts.
3. Derive folder history and follow Git renames when exact paths prove limiting.
4. Add range or symbol context only for repositories where large hotspot files
   create measurable noise.
5. Expose active Capture activity to orchestrators for multi-agent awareness.
6. Add Agent Trace import/export as an interoperability layer, not an internal
   storage model.

## Project documents

- [`design.md`](./docs/design.md) contains the philosophy, accepted decisions,
  lifecycle, interfaces, MVP boundaries, and rejected alternatives.
- [`plan1.md`](./docs/plan1.md) through [`plan11.md`](./docs/plan11.md) form the
  stacked implementation plan, with one reviewable pull request per file.

## License

Madeleine is licensed under the [Apache License 2.0](./LICENSE).
