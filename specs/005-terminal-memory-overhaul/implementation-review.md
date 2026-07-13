# Implementation Review — Terminal Experience and Project Memory (005)

**Status**: implementation complete for every non-machine task except the SQLite scroll-back paging (T020/T023, see below). 67/83 tasks done; the remaining 16 are 13 machine-only evidence runs (§Blocked) plus T020/T023 and this final signoff (T083). All 10 packages build, vet, and test green locally.

## Built this feature (all tested)

- **Foundation (Phase 2)** — `Theme` (single-source palette/glyphs/metrics), `LayoutSnapshot`, `FrameState`/dirty-pane, `InteractionMap`, and the grapheme-aware `Composer` adapter (embeds the textarea, preserving all editing semantics). Regression checkpoint recorded in `benchmarks/checkpoints/foundation.md`.
- **US1 Instant input** — gated transcript re-render, per-item render cache (stable single-entry blocks), lifecycle-scoped tick, bounded render window (M1 trim, visible marker), and **two-stage launch** (T021): an immediate composer shell with asynchronous runtime hydration, deferring the draft/initial prompt until ready. The blocking web probe is removed from launch (T022 — persisted snapshot, config-fingerprint refresh).
- **US2 Smooth/quiet** — stream/tool coalescing, follow-output (scroll-up mid-stream sticks), completed-row byte-identity, actual-dimension resize with one atomic reflow.
- **US3 Project context/memory** — root `MUHIYA.md`, append-only ledger, boot block + one-shot tails, `/memory` TUI + CLI, secret screening.
- **US4 Mouse** — InteractionMap; click/hover for command rows, modal/MCP choices, tool/subagent chips; scoped wheel; drag text-selection + OSC 52 copy; nearest-grapheme caret placement.
- **US5 Large paste** — compact atomic blocks (stash), inspect/remove manager (bar click + `/paste`), fuzz-verified byte-exact fidelity. Checkpoint in `benchmarks/checkpoints/us5.md`.
- **US6 Visual states** — first-run/busy/tool/agent/error/too-small cues, NO_COLOR/ASCII/RTL/floor profiles, single-source Theme.
- **Stability** — 7 audited bugs fixed (crash, hang, unbounded growth, misroute, leaks, race).

## Invariant audit (T083)

| Invariant | Result |
|---|---|
| Agent loop / tool set unchanged | **Pass** — only defensive `ask_user` input validation added to the tool path. |
| Model routing / effort unchanged | **Pass**. |
| Gateway wire / marshalling unchanged | **Pass** — no edits to `internal/gateway/*` or `prefixshape.go`. |
| Deterministic prompt prefix unchanged | **Pass** — memory boot block composed once + reused verbatim on resume; the web-probe change makes the tool surface *more* stable (fixed once at session start, no mid-config flips). |
| Bounded caches, no hidden truncation | **Pass** — in-memory transcript window is bounded with a **visible** trim marker; the durable transcript is never truncated. |
| Readable legacy state | **Pass** — sessions without the project-context sidecar bootstrap in-memory; two-stage launch is opt-in so the single-stage path is byte-identical. |
| Presentation-only TUI changes | **Pass** — mouse/selection/scroll/theme/composer work is confined to `internal/tui`. |

## Deferred: SQLite scroll-back paging (T020/T023)

The `TranscriptWindow` paging machinery (T017) is built and unit-tested, but not wired as the live render source. The **observable US1 goals are met by other means**: presentation memory is bounded by the trim window (visible marker), input latency is independent of session size (gated re-render, proven by `TestTypingAndIdleTickDoNotReRenderTranscript`), and the complete transcript is always preserved in the session DB. Full scroll-back paging would require re-plumbing the `m.items`-based render loop that the mouse hit-testing, text selection, chip spans, and caret placement all compute against — a regression risk to shipped, tested UX features. It is left deferred rather than destabilize those; the trade-off is that trimmed older messages are read from the log rather than scrolled back to in the TUI.

## Blocked on operator hardware

`go test -race` (needs CGO/gcc), live cachebench/terminalbench baselines + improved runs, 60-minute endurance, and the Windows/macOS/Linux terminal matrix (T003/T004/T015/T024/T025/T035/T036/T061/T076/T079–T082). Commands are recorded in tasks.md; they cannot run in this environment.

## Local quality gates

`go build ./...`, `go vet ./...`, `go test ./... -count=1` — all green across 10 packages. New files gofmt-clean.
