# Plan 6: Application-only package migration

PR scope: one PR  
Depends on: `plan5.md`  
Design decisions: D-002, D-008, D-009, D-016, D-020, D-021, D-022

## Goal

Make Madeleine a standalone application whose only supported external boundary
is versioned JSON RPC. Remove the public Go library facade, place product rules
and orchestration in a private `internal/madeleine` package, and make
`internal/store` own SQLite persistence only. Preserve all runtime behavior and
the existing schema.

## Package design

```text
cmd/madeleine                    executable added by Plan 7
        |
        v
internal/cli                     command adapter added by Plan 7
        |
        v
internal/rpc                     JSON boundary added by Plan 7
        |
        v
internal/madeleine               product rules and orchestration
        |          \
        v           v
internal/store   internal/gitstate
        |                  |
        v                  v
     SQLite          gitcmd / repopath
```

Dependency rules:

- [x] `internal/madeleine` owns Repository, Conversation, Capture, and Episode
  vocabulary, validation, state transitions, errors, and use-case orchestration.
- [x] `internal/store` owns SQLite setup, migrations, SQL, row scanning,
  transaction lifetime, and persistence records.
- [x] `internal/store` must not import `internal/madeleine`.
- [x] `internal/madeleine` uses one concrete `*store.DB`; do not add a storage
  interface, alternate backend, mock framework, or dependency-injection layer.
- [x] `internal/gitstate`, `internal/gitcmd`, and `internal/repopath` retain
  their existing focused responsibilities.
- [x] Do not create generic `app`, `core`, `model`, `service`, `util`, or
  `common` packages.

## Application package

Create `internal/madeleine` with:

```text
types.go
errors.go
service.go
repository.go
capture.go
episode.go
context.go
git_reconcile.go
```

- [x] Move the canonical domain and RPC request/result types here; retain their
  JSON tags because Plan 7 serializes them directly.
- [x] Use `Service` for the application entry point. `Open` constructs the
  concrete SQLite store and `Close` releases it.
- [x] Keep UUID generation, Capture transitions, Episode validation, repository
  discovery, origin normalization, path attribution policy, and Git
  reconciliation policy in this package.
- [x] Preserve the operation signatures currently exposed by the root `Store`
  as private `Service` methods so Plan 7 can dispatch to them without another
  domain model.
- [x] Map storage absence, conflicts, and compare-and-set failures to the
  existing Madeleine sentinel errors here.

## SQLite store

Refactor `internal/store` to contain:

```text
database.go
records.go
migrations.go
repository.go
conversation.go
capture.go
episode.go
context.go
git_baseline.go
migrations/*.sql
```

- [x] Name persistence values precisely: `RepositoryRecord`, `CaptureRecord`,
  `EpisodeRecord`, `EpisodeSummaryRecord`, and `GitBaselineRecord`.
- [x] Return `(record, found, error)` for optional rows and affected-row counts
  for compare-and-set operations; do not duplicate Madeleine sentinel errors.
- [x] Provide `DB.WithTransaction(ctx, func(*store.Tx) error)` for application
  rules that must run inside one immediate transaction.
- [x] Expose named persistence operations on `DB` and `Tx`; do not expose raw
  SQL or `*sql.Tx` to `internal/madeleine`.
- [x] Keep all SQL statements and row scanners in this package.
- [x] Keep migration SQL and version history byte-for-byte unchanged.

The application layer must continue to own the decisions inside these atomic
flows while the store owns their mechanics:

- [x] repository alias matching and registration;
- [x] Conversation get-or-create and transcript refresh;
- [x] Capture creation plus Git baseline insertion;
- [x] write recording plus last-seen update;
- [x] sealing plus Git path insertion and terminal cleanup;
- [x] Episode publication plus Capture finalization and cleanup;
- [x] abandonment plus raw-state cleanup.

## Remove the public Go library

Delete the root Go package and its facade-only tests:

```text
madeleine.go

types.go
api_external_test.go
facade_external_test.go
types_test.go
```

After this plan, no supported package exists at
`github.com/aduverger/madeleine`. Go code is private under `internal/`; Plan 7
adds the installable `cmd/madeleine` executable.

## Test migration

- [x] Move domain, repository-discovery, lifecycle, and application-operation
  tests to `internal/madeleine`.
- [x] Keep migration, schema, transaction, SQL, and persistence-constraint tests
  in `internal/store`.
- [x] Replace the public facade test with one internal end-to-end Service test
  covering Repository resolution through Episode context retrieval.
- [x] Preserve real SQLite, real Git, concurrent process, rollback, idempotency,
  and non-mutating Git coverage.
- [x] Test package dependency direction so `internal/store` cannot begin
  importing `internal/madeleine` unnoticed.

## Documentation migration

- [x] Update `README.md` to describe a standalone CLI rather than a reusable Go
  library and update the architecture diagram.
- [x] Update `docs/design.md`: make JSON RPC the sole external API, document the
  private package graph, mark D-010 superseded, and add D-022.
- [x] Add a historical note to Plans 1-5 instead of rewriting their merged file
  lists and implementation provenance.
- [x] Renumber the former Plans 6-10 to Plans 7-11 and update every dependency,
  forward reference, and plan-range reference.
- [x] Update the CLI plan to import `internal/madeleine`; later Pi plans remain
  clients of JSON RPC only.

## Verification

- [x] `go list ./...` reports no root `github.com/aduverger/madeleine` package.
- [x] `make check` passes.
- [x] `go test -race ./...` passes.
- [x] `go test ./... -shuffle=on -count=3` passes.
- [x] `git diff --check` passes.

## Implementation assumptions, plan changes, and research

Listed least-confident first:

1. Two non-timeout `internal/gitcmd` tests now use the existing five-second
   discovery budget instead of a one-second test-only budget. Repeated shuffled
   runs consistently exhausted one second while starting the fake executable;
   production timeout behavior is unchanged.
2. Application tests open a separate test-only SQLite connection for schema
   assertions and injected failures. This preserves the existing transaction
   and rollback coverage without exposing `*sql.DB` or raw SQL from production
   `internal/store` APIs.
3. `internal/store/records.go` was added to the proposed file list so persistence
   record definitions have one clear owner instead of being scattered through
   SQL operation files.
4. `internal/madeleine` intentionally repeats the product name. As the private
   product/application layer it gives clients meaningful names such as
   `madeleine.Service` and `madeleine.Capture`; generic package names such as
   `app`, `core`, and `model` were rejected.
5. The concrete application service depends directly on `*store.DB`. A storage
   interface would exist only to support a hypothetical second backend or unit
   mocks; neither is required by the MVP.
6. Plans 1-5 remain historical records of merged PRs. They receive a migration
   note, but their checked file lists and upstream provenance are not rewritten
   to pretend the application-only layout existed earlier.
7. The unimplemented plan stack is renumbered rather than adding a `plan6a` or
   out-of-band migration document: Plan 6 is this migration, Plan 7 is the CLI,
   and the Pi/MVP work continues through Plan 11.
8. This layout follows the official Go module guidance for applications with
   `cmd` and private supporting packages, Go package-naming guidance to create
   meaningful boundaries, and the packages-as-layers rule that dependencies
   point one way. References:
   - https://go.dev/doc/modules/layout
   - https://go.dev/blog/package-names
   - https://www.alexedwards.net/blog/11-tips-for-structuring-your-go-projects
   - https://github.com/StevenACoffman/go-advice/blob/main/Sources/benbjohnson/packages-as-layers.md

## Acceptance criteria

- [x] Madeleine has one private canonical domain model and no public Go facade.
- [x] Business rules live in `internal/madeleine`, not `internal/store`.
- [x] SQLite implementation details live in `internal/store`, not
  `internal/madeleine`.
- [x] Package imports are acyclic and follow the documented direction.
- [x] Database schema, migrations, persisted values, and runtime behavior are
  unchanged.
- [x] Plan 7 can implement RPC directly against `*madeleine.Service`.

## Excluded from this PR

CLI parsing, JSON RPC transport, Pi integration, alternate stores, exported Go
packages, and behavior changes.
