# Foundation Checkpoint (Phase 2)

**Verdict**: PASS. The shared Theme/Layout/Frame skeleton and the text-only grapheme-aware Composer adapter are in place; every existing input, transcript, modal, line-mode, session, prompt, and cache test remains green.

## What landed

- **Theme** (`internal/tui/theme.go`) — bundles the resolved palette, glyph table, render-floor and header breakpoints, and the transcript gutter as the single visual-system source (`newTheme`, consumed via `m.theme`).
- **LayoutSnapshot / FrameState** (`internal/tui/layout.go`) — the resolved pane geometry recorded by `layout()` each frame, and the per-frame dirty-pane + generation state advanced on every render.
- **InteractionMap** (`internal/tui/mouse.go`) — the immutable per-frame hit map (already in place from US4).
- **Composer** (`internal/tui/composer.go`) — a grapheme-aware adapter that embeds the Bubbles textarea, promoting every editing method (typing, history, grapheme navigation, focus, submit, reset, small paste) unchanged; paste blocks are managed alongside via the stash, so the composer stays text-only.
- **Shared test infrastructure** (`internal/tui/test_helpers_test.go`) — frame normalization and a deterministic event fixture, on top of the existing fake runtime (`testRuntime`).

## Commands and results (local, Windows, Go)

```
go build ./...            → exit 0
go vet ./...              → exit 0
go test ./... -count=1    → all packages ok
```

| Package | Result |
|---|---|
| benchmarks/cachebench | ok |
| benchmarks/terminalbench | ok |
| internal/command | ok |
| internal/contract | ok |
| internal/gateway | ok |
| internal/mcpclient | ok |
| internal/orchestrator | ok |
| internal/state | ok |
| internal/tui | ok |
| internal/workspace | ok |

New foundation tests: `theme_test.go` (palette/breakpoint single-source), `layout_test.go` (LayoutSnapshot geometry, FrameState dirty/generation), `composer_text_test.go` (typing/setvalue/reset/focus/grapheme-nav/history), `test_helpers_test.go` (deterministic frame + event fixture).

**Note**: `go test -race` requires a CGO/gcc-capable runner (not available locally) and is deferred to T079 on the operator's hardware.
