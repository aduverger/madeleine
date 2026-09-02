<p align="center">
  <img src="./assets/madeleine-logo.png" alt="Madeleine" width="760">
</p>

<p align="center">
  <strong>Historical context for coding agents, attached to the files they explore.</strong>
</p>

Madeleine is a local-first memory layer for coding agents. When an agent reads a
file, Madeleine retrieves Episodes from earlier agent work that intentionally
changed that exact repository path. Navigation remains the relevance signal:
there is no embedding search, reranker, or separate memory agent in the normal
path.

The first integration targets [Pi](https://github.com/earendil-works/pi). It
runs a local Go application and stores all Madeleine data in SQLite; no daemon or
network service is required.

## Install

Development builds track the current main branch:

```sh
go install github.com/aduverger/madeleine/cmd/madeleine@main
pi install npm:@aduverger/madeleine-pi@0.1.0
```

After the repository is merged and tagged, install the matching release:

```sh
go install github.com/aduverger/madeleine/cmd/madeleine@v0.1.0
pi install npm:@aduverger/madeleine-pi@0.1.0
```

The `v0.1.0` Go tag is created after the MVP PR merges; this PR does not create
the tag.

Run Pi inside a Git repository, then verify the installation from either the
shell or Pi:

```text
madeleine doctor
/madeleine doctor
```

The extension requires Node.js 22.19 or newer and a configured Pi model. The Go
binary must be available as `madeleine` on `PATH`, unless `MADELEINE_BIN` points
to it explicitly.

## How it works

Madeleine has four product objects:

- **Conversation** — a harness-owned thread, identified in Pi by its session
  UUID.
- **Capture** — recoverable state for the current interval of work.
- **Episode** — immutable path history and L1/L2 summaries published from a
  Capture.
- **Transcript** — immutable sanitized evidence for one non-empty sealed
  Capture.

A normal interval is:

```text
Pi session starts or resumes
    -> attach one open Capture, or create one
successful edit/write
    -> persist the normalized repository path immediately
clean shutdown, /tree, or /madeleine capture
    -> persist the cursor-bounded semantic Transcript and seal the Capture
    -> generate L1/L2 with Pi's active authenticated model
    -> atomically publish compact evidence and the Episode
later read of the same path
    -> inject the five newest L1 summaries
```

Every non-no-op `/tree` navigation creates a Capture boundary so abandoned
branch intent remains paired with its paths. A temporary source Capture remains
active while Pi creates an optional branch summary; failed or cancelled
navigation therefore does not strand write capture.

## Progressive context

Madeleine exposes history in four levels:

1. **L1** — one or two sentences appended automatically to a successful exact
   path read.
2. **L2** — a longer implementation brief retrieved with
   `madeleine_episode`.
3. **Compact Transcript** — the exact final evidence supplied to the successful
   L1/L2 model call.
4. **Raw Transcript** — fuller sanitized semantic entries retrieved in stable
   pages with `madeleine_transcript`.

The retrieval tools are repository-scoped. An Episode or Transcript from a
different repository is not returned.

## Commands

Run these inside Pi:

```text
/madeleine status
/madeleine capture
/madeleine abandon <capture-id>
/madeleine doctor
```

- `status` lists open and pending Captures in the repository and marks the
  current Capture.
- `capture` publishes the current work as an Episode and starts another Capture
  interval in the same Conversation.
- `abandon` permanently removes unfinished Capture paths and unpublished
  Transcript evidence after confirmation.
- `doctor` checks the binary, data directory, schema, Git executable, and
  current repository.

Pi tools:

```text
madeleine_episode { episode_id }
madeleine_transcript { transcript_id, view: "compact" | "raw", offset? }
```

## Recovery

An abrupt process exit leaves the current Capture open. Reopening the same
persisted Pi Conversation reattaches that Capture with its original ID,
Transcript boundary, recorded paths, and injected-path deduplication state.
Madeleine does not inspect Git or infer filesystem changes made while Pi was
stopped.

A summary, model, or publication failure leaves a sealed Capture in
`pending_summary`. After the current open Capture is attached or started, one
background worker snapshots older pending Captures and retries them oldest-first.
It attempts each queued Capture once per extension runtime, runs one recovery
summary call at a time, does not block current path recording, and is cancelled
cleanly during session shutdown. A failed attempt remains pending for the next
Pi runtime; `/madeleine abandon` deletes unfinished data when explicitly
requested.

## Storage and configuration

`MADELEINE_HOME` overrides Madeleine's data directory. The SQLite database is:

```text
macOS: ~/Library/Application Support/madeleine/madeleine.db
Linux: $XDG_DATA_HOME/madeleine/madeleine.db
       or ~/.local/share/madeleine/madeleine.db
```

`MADELEINE_BIN` overrides Go binary discovery, for example:

```sh
MADELEINE_BIN=/absolute/path/to/madeleine pi
```

SQLite runs in WAL mode with foreign keys, a five-second busy timeout, and four
connections. Capture and Episode paths are repository-relative, case-preserving,
and slash-separated.

## Trust and privacy

Madeleine stores locally:

- paths reported by successful typed `edit` and `write` results;
- Capture, Episode, Conversation, and Repository IDs and timestamps;
- generated L1/L2 summaries;
- sanitized cursor-bounded `user`, `assistant`, `branch_summary`, and `mutation`
  Transcript entries;
- the exact compact evidence used to publish each Episode.

It does **not** copy original Pi transcript files, read calls or results,
edit/write file bodies, image or audio bodies, thinking blocks, successful tool
result prose, or recursively injected Madeleine context. Historical content is
rendered as untrusted reference data rather than instructions.

Opaque shell commands, generators, formatters, external tools, human edits, and
changes from other sessions are not attributed in v0.1 unless the current Pi
session also reports a successful structured edit or write for that path. This
favors precise causal history over exhaustive filesystem detection.

## Failure behavior

The Pi integration fails open. A missing binary, unavailable repository, busy or
invalid database, malformed protocol response, missing model, invalid summary,
timeout, or cancelled child process must not break normal Pi reads and writes.
Recoverable Capture data remains open or pending whenever publication cannot
complete.

## Disable or remove

Use `pi config` to disable the installed extension while retaining the package.
Remove it entirely with:

```sh
pi remove npm:@aduverger/madeleine-pi
```

Removing the Pi package stops future collection but does not delete the local
SQLite database. Delete the configured `MADELEINE_HOME` directory separately if
you intentionally want to remove all Madeleine data.

## Development

```sh
make check
cd harnesses/pi
npm ci
npm run check
cd ../..
make pack-check
```

CI runs Go and Pi checks on macOS and Linux. The end-to-end suite builds the real
`madeleine` binary, uses temporary Git repositories and SQLite homes, and drives
a fake deterministic Pi runtime through lifecycle, recovery, retrieval, and
failure scenarios.

## Release

Release the Go application and npm adapter at the same version from a clean,
synchronized `main` branch:

```sh
make release VERSION=0.1.0
```

For a prerelease, use a non-`latest` npm tag:

```sh
make release VERSION=0.2.0-rc.1 NPM_TAG=next
```

The release target validates Git and npm state, updates the package version when
needed, runs all quality gates, publishes `@aduverger/madeleine-pi`, verifies the
registry, and pushes the matching Git tag. That tag also publishes the Go module
version.

`.github/workflows/publish.yml` supports manually dispatched npm trusted
publishing with provenance. It defaults to a dry run. Configure the npm trusted
publisher for repository `aduverger/madeleine`, workflow `publish.yml`, and
GitHub environment `npm` before enabling publication. That workflow publishes
only npm; push the matching root Git tag separately to release the Go version.

## Scope

The v0.1 MVP intentionally excludes semantic ranking, embeddings, folder or
symbol identity, rename tracking, Git/filesystem reconciliation, other harness
adapters, a daemon, network sync, and multiplayer coordination. These remain
future work rather than current product promises.

See [`docs/design.md`](./docs/design.md) for accepted architecture and
[`docs/plan1.md`](./docs/plan1.md) through
[`docs/plan12.md`](./docs/plan12.md) for the implementation plans.

## License

Madeleine is licensed under the [Apache License 2.0](./LICENSE).
