# Implementation Plan: Terminal Experience and Project Memory

**Branch**: `005-terminal-memory-overhaul` | **Date**: 2026-07-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/005-terminal-memory-overhaul/spec.md`

## Summary

Make the terminal's work per event independent of total history by retaining the existing Bubble Tea stack while introducing a keyset-paged `TranscriptWindow`, bounded rendered-block caches, stable scroll anchors, pane-level dirty flags, lifecycle-scoped timers, and a two-stage interactive startup. Add `MouseModeCellMotion` with frame-owned hit regions and in-app transcript selection/copy; replace only the main textarea with an ordered text/paste composer that preserves raw large-paste content. Complete feature 003's visual system by centralizing layout metrics and explicit application states.

Add workspace context without altering the agent loop, tools, model selection, or routing: load a contained optional `MUHIYA.md`; persist project decisions/findings in an additive workspace-scoped SQLite ledger; capture bounded memory candidates from a hidden structured trailer on the existing final response; compile instructions/current memory into an exact per-session boot snapshot; and place later changes once on the newest user-message tail. This follows the repository's documented Reasonix memory pattern and preserves byte-stable prefixes and append-only settled history after a single documented upgrade boundary ([research D9-D13](research.md)).

## Technical Context

**Language/Version**: Go 1.25.0 (`go.mod`; current development host uses a newer compatible Go toolchain)

**Primary Dependencies**: `charm.land/bubbletea/v2 v2.0.8`, `charm.land/bubbles/v2 v2.1.1`, `charm.land/lipgloss/v2 v2.0.5`, `github.com/charmbracelet/x/ansi`, `github.com/mattn/go-runewidth` (terminal); `modernc.org/sqlite v1.53.0` (durable state); Cobra v1.10.1 (CLI). No new runtime dependency.

**Storage**: Existing `~/.muhiya` / `MUHIYA_HOME` layout. Additive SQLite `project_memory_events` table and event-paging queries; typed per-session `project_context.json` sidecar; root-local `MUHIYA.md` is read-only. Existing histories, transcripts, session bundles, trust rows, and sidecars remain readable.

**Testing**: `gofmt`, `go vet`, `go test ./... -count=1`, and `go test -race ./... -count=1` on a CGO-capable runner; direct TUI update/frame tests; state/prompt/session integration tests; deterministic `benchmarks/terminalbench`; existing live `benchmarks/cachebench`; real-terminal frame/interaction matrix. Current targeted baseline (`internal/tui`, `internal/state`, `internal/orchestrator`) is green before design work.

**Target Platform**: Local terminal application; Windows Terminal primary, with supported macOS and Linux terminals. Full-screen TTY plus `--simple`, non-TTY, and one-shot modes.

**Project Type**: Single Go CLI/TUI application with local durable state and an existing OpenAI-compatible provider port.

**Performance Goals**: Input-to-frame P95 <=16 ms at 10 and 5,000 transcript events with <=2 ms P95 gap; stream chunk P95 large-history gap <=5 ms; resume input-ready P95 <=2 s over 30 trials; 60-minute/5,000+ workload <=300 MiB WorkingSet/RSS with <=10% final-15-minute growth; unchanged idle <=1% of one core and zero recurring full-frame refreshes.

**Constraints**: Preserve correctness, agent loop, tool surface, model selection/routing, provider compatibility, security enforcement, simple-line behavior, and stable-prefix/append-only-history guarantees. Local-only memory; no semantic code index, diff review, `@` mentions, or fast-apply. Root instructions <=32 KiB; current project-memory projection <=64 items and <=32 KiB; no detected secrets persisted. TUI window/render caches remain bounded by entry and byte budgets.

**Scale/Scope**: 5,000+ mixed transcript events, multi-hour sessions, large individual messages/tool output, multiple paste blocks, up to 64 current durable memory items, one optional root instructions file, eight explicit UI states, six mouse target classes, three OS families. Primary implementation touches `internal/tui`, `internal/state`, `internal/workspace`, `internal/orchestrator`, `internal/command`, tests/benchmarks, and related docs.

## Constitution Check

*GATE: evaluated before Phase 0 and re-checked after Phase 1 design — PASS. No violation requires Complexity Tracking.*

| # | Principle | Pre-research gate | Post-design evidence |
|---|---|---|---|
| I | Correctness before optimization | PASS | Transcript virtualization changes only presentation retention; SQLite remains authoritative. Stable IDs, generation checks, atomic draft assembly, and recovery tests prevent missing/duplicated content. Performance work cannot ship if acceptance behavior regresses ([terminal-performance](contracts/terminal-performance.md)). |
| II | Cache efficiency without quality loss | PASS | Project context adds relevant information and removes none. Current memory/instructions are complete within explicit bounds; overflow is rejected visibly, never silently truncated. Cachebench reports hit rate, tokens, cost, and quality before/after. |
| III | Deterministic stable prefix | PASS (guarded) | One fixed output-contract paragraph and exact boot context create one upgrade-boundary reset. Thereafter a session restores `project_context.json` bytes exactly; canonical rendering omits timestamps and sorts skills/memory. Mid-session changes append at the tail. Stability/restart/prefix-shape tests are mandatory ([project-context](contracts/project-context.md)). |
| IV | Separation of dynamic and cached content | PASS | Boot instructions/memory/skills are session-stable and belong in the cached prefix. File/memory changes after boot use one-shot `<project-instructions-update>` / `<memory-update>` blocks on the newest user tail; they never mutate the prefix. |
| V | No redundant retransmission | PASS | Boot context is cached once. Each update sequence is appended exactly once and its cursor is persisted atomically. Transcript paging affects only local presentation and does not retransmit provider history. |
| VI | Honest measurement | PASS | Provider token/cache/cost claims continue using provider usage records. Terminal latency uses event/frame timestamps; CPU and WorkingSet/RSS come from OS process metrics, with Go heap reported separately. All evidence labels environment, fixture, build, and unavailable fields ([terminal-performance](contracts/terminal-performance.md)). |
| VII | Reference architecture: Reasonix | PASS | Research D12 records the direct Reasonix pattern: memory frozen into the boot prefix, changes on a tail `<memory-update>`, resume from persisted bytes. The design adopts it and updates the existing closed tail contract. |
| VIII | Improve, don't rewrite | PASS | Bubble Tea, viewport, renderer, storage, engine loop, and simple-line path stay. A bounded transcript layer and frame cache wrap existing rendering. Only the main textarea is replaced because its flat sanitized string cannot represent atomic raw paste parts; research D7 documents rejected adapters. |
| IX | Clean, maintainable, secure, provider-compatible | PASS (security review required) | No new runtime modules. `MUHIYA.md` uses canonical root containment, UTF-8/NUL/size/secret validation. Memory candidates reject any detected provider/MCP secret. Additive schema/sidecars preserve compatibility. No provider-specific API is added; tool/model/routing behavior remains unchanged. Review against `docs/security.md` is mandatory. |
| X | Verified improvements | PASS (planned) | Fixed-seed terminalbench plus clean-worktree before/after runs, 60-minute evidence, cross-platform recordings, existing cachebench, and quality suites are specified in [quickstart.md](quickstart.md). Same fixture/model/gateway/effort/machine conditions are held constant where applicable. |

**Workflow gates**:

- Prefix stability: extend `prompt_stability_test.go`, `restart_determinism_test.go`, `request_assembly_test.go`, `cachehit_guard_test.go`, and gateway marshal/retry tests. Update feature 002's closed tail allow-list for the two project-context update tags.
- Honest evidence: record raw terminalbench and cachebench baseline/improved outputs under `specs/005-terminal-memory-overhaul/benchmarks/`; do not use the current dirty working tree as a baseline.
- Security review: required for root file containment, project-memory secret rejection, SQLite/sidecar permissions, clipboard behavior, and clear/forget operations.
- Documentation sync: update `README.md`, `docs/agent-design.md`, `docs/architecture.md`, `docs/security.md`, and `docs/prompt-caching.md` in the implementation change.
- Quality: `gofmt`, `go vet`, full tests, cross-platform CI, and race suite must pass before merge.

## Project Structure

### Documentation (this feature)

```text
specs/005-terminal-memory-overhaul/
├── plan.md
├── spec.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── terminal-performance.md
│   ├── mouse-interaction.md
│   ├── input-draft.md
│   ├── project-context.md
│   └── visual-state-system.md
├── checklists/
│   └── requirements.md
├── benchmarks/                 # implementation-time baseline/improved evidence
└── tasks.md                    # Phase 2 output; not created by /speckit-plan
```

### Source Code (repository root)

```text
cmd/muhiyacode/
└── main.go                     # unchanged thin entry point

internal/contract/
└── types.go                    # additive project-memory/context values and ports if shared

internal/state/
├── db.go                       # additive migration; keyset event paging
├── project_memory.go           # project-memory ledger/current projection/clear operations
├── session.go                  # typed project_context.json read/write
└── *_test.go                   # migration, paging, isolation, atomic recovery, secret cases

internal/workspace/
├── project_context.go          # contained MUHIYA.md load/canonicalize/hash/diagnostics
├── paths.go                    # existing canonical containment reused
└── *_test.go                   # symlink/junction, UTF-8, size, secret cases

internal/orchestrator/
├── prompt.go                   # fixed memory trailer contract + boot context block
├── project_context.go          # deterministic snapshot/update rendering; trailer observer
├── engine.go                   # completion-boundary observation and one-shot user-tail updates
├── history.go                  # append-only integration only; no new rewrite path
└── *_test.go                   # stability, restart, tail allow-list, malformed trailer tests

internal/command/
├── application.go              # two-stage runtime hydration; pager/context/memory composition
├── root.go                     # `memory` CLI and early TUI shell composition
└── *_test.go                   # resume, one-shot, simple/non-TTY, memory command integration

internal/tui/
├── model.go                    # dirty-state/event routing; lifecycle timers; generation handling
├── view.go                     # bounded cached frame composition
├── transcript.go               # paged window, stable anchor, block cache, live reconciliation
├── composer.go                 # TextPart/PastePart draft and exact assembly
├── mouse.go                    # interaction map, selection/copy, semantic action routing
├── theme.go                    # palette/glyphs plus spacing/breakpoints/pane metrics
├── actions.go                  # `/memory` modal/actions and existing keyboard equivalents
├── bridge.go                   # frame-scoped assistant/tool stream coalescing
├── run.go                      # line-mode context integration; no full-screen dependency
└── *_test.go                   # frame invalidation, paging, mouse, paste, state, line mode

benchmarks/terminalbench/
├── main.go                     # fixed-seed fixture generation and scenario runner
├── fixture.go                  # real SQLite + session-sidecar population
└── metrics_*.go                # OS-specific CPU/WorkingSet/RSS sampling

benchmarks/cachebench/          # existing live cache/quality harness; reused unchanged where possible

docs/
├── architecture.md
├── agent-design.md
├── prompt-caching.md
└── security.md
```

**Structure Decision**: Keep the single-project layered Go architecture. Presentation-only paging/composition/mouse work stays in `internal/tui`; SQLite and typed sidecars stay in `internal/state`; root-file policy stays in `internal/workspace`; only deterministic context assembly and final-trailer observation touch `internal/orchestrator`; `internal/command` composes these pieces and exposes user controls. No gateway or external repository change is required.

## Complexity Tracking

No constitution violations. The scoped main-input component replacement is justified by research D7 and does not constitute a TUI subsystem rewrite; all other established layers and compatibility boundaries remain in place.
