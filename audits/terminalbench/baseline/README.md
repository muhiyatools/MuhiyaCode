# Terminal-Bench Readiness Baseline

**Captured**: 2026-07-24  
**HEAD**: `662cbd4ce536b844f22666e3cdafc79df7773a28`  
**Toolchain**: `go version go1.26.4 windows/amd64`

## Preserved working tree

The implementation began with unrelated, uncommitted TUI changes. They are intentionally retained and are not part of this feature:

- `internal/tui/footer_test.go`
- `internal/tui/layout.go`
- `internal/tui/layout_test.go`
- `internal/tui/render_header.go`
- `internal/tui/render_transcript.go`
- `internal/tui/tui_test.go`
- `internal/tui/view.go`
- `internal/tui/visibility_test.go`

The Terminal-Bench audit document and `specs/014-terminal-bench-readiness/` are also uncommitted feature artifacts.

## Focused baseline gate

Command:

`go test ./internal/command ./internal/orchestrator ./internal/workspace ./internal/gateway -count=1`

Result: PASS

- `internal/command`: 2.056s
- `internal/orchestrator`: 1.418s
- `internal/workspace`: 5.645s
- `internal/gateway`: 12.656s

No files were reset, stashed, or discarded to obtain this result.
