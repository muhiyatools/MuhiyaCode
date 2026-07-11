# Scripted workload (24 turns)

1. Read the fixture README and summarize its invariants.
2. Inspect the repository tree and identify the executable entry point.
3. Read `cmd/taskboard/main.go` and explain the command flow.
4. Search for every `TODO` or `FIXME` marker.
5. Read `internal/taskboard/store.go` and list its validation rules.
6. Read the existing tests and identify one missing edge case.
7. Run the complete test suite and report the starting result.
8. Inspect `config/settings.json` and find settings unused by the CLI.
9. Inspect `data/tasks.json` and calculate the expected status totals.
10. Compare those totals with the `stats` command output.
11. Add a deterministic status-order helper in the storage package.
12. Add a focused unit test for that helper.
13. Update the CLI stats command to use deterministic order.
14. Run tests and fix any failure without changing public JSON fields.
15. Add validation that rejects an empty task title.
16. Add a table-driven unit test for empty and whitespace-only titles.
17. Add a `get <id>` CLI command while preserving existing commands.
18. Add tests for a found ID and a missing ID.
19. Run `go test ./...` and inspect the resulting diff.
20. Update the fixture README command examples for `get`.
21. Re-read `docs/architecture.md` and update it for the new query path.
22. Externally edit one task title in `data/tasks.json`, then verify the CLI sees it.
23. Search the final tree for stale command lists and correct any found.
24. Run formatting and tests, then summarize changed files and behavior.
