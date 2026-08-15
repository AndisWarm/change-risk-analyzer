# ADR-004: Separate the Go analyzer module from the GitHub Action client

- Status: Accepted
- Date: 2026-08-15
- Scope: Repository module layout

## Context

The repository started with the Go module and its `internal/` packages at the repository root. The product now needs a clear boundary between the reusable analyzer implementation and the future GitHub Action packaging layer, without changing the MVP into a web service or GitHub App.

## Decision

- `server/` is the single Go module and owns the analyzer source, tests, and future `cmd/go-risk-analyzer` CLI.
- The Go module path remains `change-risk-analyzer`; existing internal import paths do not change.
- `client/` owns the future GitHub Action packaging layer. It does not import `server/internal` packages and does not provide a browser UI.
- The repository root contains documentation, specifications, and `go.work`, which uses `./server` for local development.
- The future Action downloads a versioned analyzer binary only after the release workflow and checksum verification exist.

## Consequences

- Go development commands run against `./server/...` from the root, or from within `server/`.
- Existing analyzer behavior, report schema, Action permissions, and public interfaces do not change in this migration.
- Root-level `internal/` paths in historical status records describe their original locations; new implementation records use `server/internal/`.
- This directory change is reversible by moving the module back; it does not require a report-schema migration or user migration.
