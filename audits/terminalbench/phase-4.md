# TerminalBench Phase 4 Audit

## Summary of Fixes

- **T025**: Implemented `fullLines` support in `read_file` to prevent truncating long lines midway through a character, addressing unicode truncation risks. Updated tests in `read_truncate_test.go`.
- **T027**: Improved error messaging for non-Go files in `inspect_code` to explicitly direct the model to use `grep_search`. Relaxed the output byte limit from 2000 to 3000 to accommodate larger parsing results. Updated tool schemas in `registry.go` with explicit notes for Go-only support. Tested in `codeindex_test.go`.
- **T029**: Refactored `multi_edit` in `files.go` to use a dry-run atomic pass. If any edit misses (non-idempotent), all edits are aborted and the file remains unchanged, returning a clear error outlining each edit's status. Extended test cases in `edit_mismatch_test.go` to assert atomicity.
- **T031-T033**: Added `ClassifyShellAutoAccept` in `risk.go` to whitelist environment enumeration (`printenv`, etc.) and world-writable chmod commands in auto-accept mode, while tightly blocking `git` publication (`commit`, `push`) and destructive operations. Wired this into `permissions.go`'s `ApproveShell`. Tested in `risk_launder_test.go`.
- **T034**: Hardened schema validation in `orchestrator/validate.go`. We now explicitly reject unexpected fields (when `additionalProperties` is false) and empty strings for required fields (instead of bubbling errors down). Implemented tests in `validate_nested_test.go` and verified against golden schemas.
- **T035**: Expanded `RescueToolCalls` in `gateway/rescue.go` to intercept unwrapped JSON array tool calls (`[{...}]`) and common JSON envelopes (`{"tool_calls": [...]}`). Capped rescue iterations at 10 to prevent unbounded processing. Tested in `rescue_test.go`.

## Test Coverage Status
- `internal/workspace`: All tests pass successfully.
- `internal/orchestrator`: All tests pass successfully (golden wire tests were updated).
- `internal/gateway`: All tests pass successfully.

Note: There was temporary breakage in `permissions_test.go` related to simultaneous Phase 2-3 work on `permissions.go` changing the signature of `NewGuard`. We successfully synced `permissions_test.go` to use the updated `GuardOptions` signature to enable our test runs.

## Phase 6 Gate
- **T044**: Added generic project-marker test-runner discovery and verification result types in `internal/orchestrator/verification.go`.
- **T047**: Added initial tests in `internal/orchestrator/verification_test.go`.
- **T055**: Ran `go test ./internal/orchestrator ./internal/gateway ./internal/command -count=1`. All targeted tests passed successfully.
