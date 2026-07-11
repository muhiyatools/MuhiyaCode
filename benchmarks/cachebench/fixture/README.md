# Cachebench Taskboard Fixture

This intentionally small Go project is copied to a temporary workspace for each cachebench
run. It contains source, tests, configuration, data, and documentation so a scripted coding
session can exercise realistic reads, searches, edits, tests, and follow-up verification
without touching the MuhiyaCode repository itself.

The project is a JSON-backed task board. Its deliberately modest surface keeps benchmark runs
repeatable while still providing enough cross-file context for the 24-turn workload in
`workload.md`.

## Commands

```sh
go test ./...
go run ./cmd/taskboard list
go run ./cmd/taskboard stats
```

## Invariants

- Task IDs are positive and unique.
- Status is one of `todo`, `doing`, or `done`.
- Data-file order is preserved when loading and saving.
- Unknown CLI commands return an error and a non-zero exit status.
