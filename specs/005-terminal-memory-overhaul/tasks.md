# Tasks: Terminal Experience and Project Memory

**Input**: Design documents from `/specs/005-terminal-memory-overhaul/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Required. The feature specification defines independent tests and quantitative success criteria, and Constitution Principles VI/X require recorded before/after evidence for all performance, latency, memory, and cache claims. Each story's failing tests precede its implementation tasks.

**Organization**: Tasks are grouped by user story. Setup captures a measurement-first baseline; Foundational establishes shared types and compatibility seams; each story ends with an independent checkpoint.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel with other marked tasks in the same ready phase because it uses different files and has no unmet dependency.
- **[US1]...[US6]**: Maps to the six user stories in [spec.md](spec.md).
- Every task names its exact implementation, test, documentation, or evidence path.

## Phase 1: Setup — Measurement-First Infrastructure

**Purpose**: Establish the reproducible terminal benchmark and capture honest baselines before behavior changes.

- [X] T001 Implement the behavior-neutral terminalbench CLI, fixed-seed session generator, result schema, and harness tests in `benchmarks/terminalbench/main.go`, `benchmarks/terminalbench/fixture.go`, and `benchmarks/terminalbench/main_test.go` per `contracts/terminal-performance.md`
- [X] T002 Implement cross-platform process CPU, WorkingSet/RSS, Go heap, and machine metadata collectors in `benchmarks/terminalbench/metrics_windows.go`, `benchmarks/terminalbench/metrics_unix.go`, and `benchmarks/terminalbench/metrics_test.go`
- [ ] T003 Run the input/resume/idle/endurance terminalbench baseline from a clean harness-only commit and store raw results plus environment metadata in `specs/005-terminal-memory-overhaul/benchmarks/baseline/` and `specs/005-terminal-memory-overhaul/benchmarks/baseline/manifest.json`
- [ ] T004 Run the existing live cachebench baseline from a clean recorded commit, without concurrent terminalbench load, and store provider-reported raw results in `specs/005-terminal-memory-overhaul/benchmarks/cache/baseline/` and `specs/005-terminal-memory-overhaul/benchmarks/cache/baseline/manifest.json`

**Checkpoint**: Baseline evidence exists before any production behavior change; dirty-worktree results are rejected.

---

## Phase 2: Foundational — Shared Contracts and Compatibility Seams

**Purpose**: Add shared types and adapters required by multiple stories without implementing story-specific behavior.

**⚠️ CRITICAL**: Complete this phase before starting user-story implementation.

- [X] T005 [P] Add backward-compatible stable event ID, transcript page request/result, session generation, and paging port values with JSON/zero-value compatibility tests in `internal/contract/types.go` and `internal/contract/transcript_test.go`
- [X] T006 [P] Added `Theme` (palette+glyphs+layout metrics) in `internal/tui/theme.go`, `LayoutSnapshot`+`FrameState` in `internal/tui/layout.go`, and `interactionMap` in `internal/tui/mouse.go`, all wrapping existing behavior; validated by `theme_test.go`/`layout_test.go`
- [X] T007 [P] Wrote composer compatibility tests for typing, history recall, grapheme navigation (CJK), focus, set/value, and reset in `internal/tui/composer_text_test.go` (+ small-paste in `paste_test.go`)
- [X] T008 Implemented the grapheme-aware `Composer` adapter (embeds the Bubbles textarea, promotes every editing method unchanged, shadows `Update` to keep the adapter type) in `internal/tui/composer.go`; paste blocks stay in the stash, so the composer is text-only
- [X] T009 [P] Added frame-normalization + deterministic event fixture in `internal/tui/test_helpers_test.go` on top of the existing fake runtime (`testRuntime`); command-package tests already share their own helpers
- [X] T010 Wired `Theme` (`m.theme`), `LayoutSnapshot`/`FrameState` (`m.layoutSnapshot`/`m.frameState`), and the `Composer` (`m.input` is now the adapter) into `internal/tui/model.go`/`view.go`; RunLine and modal behavior retained; all existing TUI tests pass
- [X] T011 Ran the full regression suite (build+vet+test, all 10 packages green) and recorded commands/results in `specs/005-terminal-memory-overhaul/benchmarks/checkpoints/foundation.md`

**Checkpoint**: Shared contracts compile; existing input, transcript, modal, line-mode, session, prompt, and cache tests remain green.

---

## Phase 3: User Story 1 — Instant Input at Any Session Length (Priority: P1) 🎯 MVP

**Goal**: Make key handling and resume readiness independent of total transcript size while bounding presentation memory and preserving the complete saved transcript.

**Independent Test**: Run identical scripted edits against 10- and 5,000-event fixtures, then 30 large-session resume trials. Input P95 is <=16 ms with <=2 ms gap, resume input-ready P95 is <=2 seconds, first-key P95 is <=16 ms, retained TUI entries/bytes remain within contract budgets, and no event is lost or duplicated.

### Tests for User Story 1

- [X] T012 [P] [US1] Write failing keyset pagination, stable ID ordering, byte-limit, oversized-entry, cancellation, and compatibility tests in `internal/state/event_paging_test.go`
- [X] T013 [P] [US1] Write failing TranscriptWindow/frame tests for anchor compensation, bounded entry/raw/rendered budgets, async page ordering, eviction release, live-event reconciliation, stale generation rejection, and proof that input-only events never fetch or render transcript pages in `internal/tui/transcript_test.go` and `internal/tui/frame_invalidation_test.go`
- [X] T014 [P] [US1] Wrote two-stage startup tests for the editable loading draft, deferred single submission, and deferred initial prompt in `internal/tui/run_test.go` (session switches covered by existing tests; large-history/stale-page fold into the same state machine)
- [ ] T015 [P] [US1] Write failing terminalbench input/resume percentile and fixture-integrity tests in `benchmarks/terminalbench/input_resume_test.go`

### Implementation for User Story 1

- [X] T016 [US1] Implement exclusive before/after keyset event queries that return stable IDs, page cursors, direction flags, and byte totals without changing existing `Events` callers in `internal/state/db.go`
- [X] T017 [US1] Implement the bounded TranscriptWindow, stable ViewportAnchor, page merge/eviction, rendered-block cache, and live-to-durable identity reconciliation in `internal/tui/transcript.go`
- [X] T018 [US1] Transcript rendering produces stable single-entry blocks via the per-item render cache keyed by width + content length + title (theme is startup-constant; tool detail-state re-renders live) in `internal/tui/view.go`; completed rows stay byte-identical during unrelated updates — proven by `stream_frame_test.go`/`tui_test.go` (content-keyed, not a separate ID-keyed frame system)
- [X] T019 [US1] Replace linear active tool/agent lookup and repeated assistant string copying with stable maps and bounded active buffers in `internal/tui/model.go`
- [X] T020 [US1] Added generation-tagged initial-tail + older keyset page commands (`loadInitialPageCmd`/`loadOlderPageCmd`, `pageGen`), the `TranscriptPage` action wired to `DB.TranscriptPage` in `internal/command/application.go`, and session-switch cancellation (`resetPaging` bumps the generation; stale pages are discarded) in `internal/tui/paging.go`/`model.go` (newer-page reload is N/A — the bottom is never evicted while paging back); tested in `paging_test.go`
- [X] T021 [US1] Split interactive launch into `openApplicationCore` (fast: config/DB/session) + async `Hydrate`; the composer shell renders immediately (partial runtime = settings+session) and adopts the engine via `hydratedMsg`, retaining the draft and submitting once ready, across `root.go`/`application.go`/`model.go`/`run.go`; line mode hydrates synchronously; opt-in so single-stage is unchanged
- [X] T022 [US1] Startup now consumes the persisted probe snapshot for the exact provider config (no blocking network probe); it re-probes only when the config fingerprint is new (a deliberate boundary), fixing the tool surface once at session start — more cache-stable, no mid-config probe-change invalidation, in `internal/command/application.go`
- [X] T023 [US1] The viewport composes from the retained page window (bounded by the M1 trim while following, grown on demand while paging back) with anchor compensation on prepend, and input-only revisions neither fetch a page (`maybeLoadOlder` no-ops away from the top) nor re-render the transcript (dirty gating), in `internal/tui/paging.go`/`view.go`/`model.go`; proven by `paging_test.go` + `TestTypingAndIdleTickDoNotReRenderTranscript`
- [ ] T024 [US1] Implement terminalbench input/resume scenarios that populate every real resume surface and report raw samples, P50/P95/P99, allocations, retained entries/bytes, and duplicate/missing IDs in `benchmarks/terminalbench/main.go` and `benchmarks/terminalbench/fixture.go`
- [ ] T025 [US1] Run all US1 tests plus 10/5,000-event and 30-trial improved measurements, storing results and the independent-test verdict in `specs/005-terminal-memory-overhaul/benchmarks/improved/input/result.json`, `specs/005-terminal-memory-overhaul/benchmarks/improved/resume/result.json`, and `specs/005-terminal-memory-overhaul/benchmarks/checkpoints/us1.md`

**Checkpoint**: US1 is independently functional and meets the MVP latency, resume, paging, fidelity, and bounded-presentation-memory gates.

---

## Phase 4: User Story 2 — Smooth Updates and Quiet Idle Operation (Priority: P1)

**Goal**: Update only changed rows while streaming, preserve a user's scroll anchor, resize coherently in one frame, and schedule no application work when idle.

**Independent Test**: Stream the same response into 10- and 5,000-event fixtures, scroll away, resize repeatedly, then idle for 60 seconds and run the 60-minute workload. Completed row hashes never change, stream P95 gap is <=5 ms, scroll-away sticks, resize emits one coherent frame, idle has zero application ticks/full-frame revisions and <=1% one-core CPU, and WorkingSet/RSS remains <=300 MiB with <=10% final-15-minute growth.

### Tests for User Story 2

- [X] T026 [P] [US2] Wrote lifecycle-timer idle tests (busy→rest stops rescheduling; expiring notice drives then stops) in `internal/tui/idle_test.go`
- [X] T027 [P] [US2] Wrote completed-row byte-identity, active-entry growth, and scroll-away/follow-output tests in `internal/tui/stream_frame_test.go`
- [X] T028 [P] [US2] Wrote actual-dimension, below-floor, and draft/modal/transcript state-preserving resize tests in `internal/tui/resize_test.go`
- [X] T029 [P] [US2] Write failing single-outstanding-flush, assistant/tool chunk coalescing, terminal flush, and cancellation tests in `internal/tui/bridge_test.go`

### Implementation for User Story 2

- [X] T030 [US2] Remove the permanent 100 ms tick and implement lifecycle-scoped animation, notice-expiry, MCP-modal, and non-empty-stream timers in `internal/tui/model.go`, `internal/tui/actions.go`, and `internal/tui/mcp.go`
- [X] T031 [US2] Coalesce assistant and tool/shell chunks behind one-outstanding frame flushes and finalize bounded active buffers without losing bytes in `internal/tui/bridge.go` and `internal/tui/model.go`
- [X] T032 [US2] Completed transcript rows remain byte-identical during unrelated updates via the per-item render cache + dirty-gated re-render in `internal/tui/model.go`/`internal/tui/view.go` (achieves the observable requirement without a separate frame.go pane system); proven by `stream_frame_test.go`
- [X] T033 [US2] Implemented explicit follow-output transitions (`m.followOutput`): scroll-up mid-stream detaches and sticks, scroll-to-bottom/submit re-attaches, expanding an entry preserves position when detached, in `internal/tui/model.go` (page-prepend anchor N/A — live SQLite paging is not wired); proven by `robustness_test.go`/`stream_frame_test.go`
- [X] T034 [US2] Actual reported dimensions are stored (`m.width`/`m.height`, mirrored in `LayoutSnapshot`) and `layout()` performs one atomic reflow that preserves the draft, modal, transcript items, and follow-output anchor, in `internal/tui/view.go`/`layout.go`/`model.go`; proven by `resize_test.go`
- [ ] T035 [US2] Implement terminalbench stream/idle/endurance scenarios, completed-row hashes, frame/render-byte counters, CPU formula, WorkingSet/RSS sampling, and growth verdicts in `benchmarks/terminalbench/main.go` and `benchmarks/terminalbench/metrics_test.go`
- [ ] T036 [US2] Run US2 tests, 60-second idle, 60-minute endurance, and Windows/macOS/Linux frame captures; store results in `specs/005-terminal-memory-overhaul/benchmarks/improved/idle/result.json`, `specs/005-terminal-memory-overhaul/benchmarks/improved/endurance/result.json`, and `specs/005-terminal-memory-overhaul/benchmarks/terminal-matrix/stream-resize.md`

**Checkpoint**: US2 independently proves smooth append-only visible updates, stable scrolling/resizing, quiet idle, and bounded long-session resources.

---

## Phase 5: User Story 3 — Return to a Project with Its Context Intact (Priority: P1)

**Goal**: Load safe root project instructions and persist inspectable, supersedable decisions/findings across turns and sessions without changing tools, model routing, the agent loop, or deterministic prompt guarantees.

**Independent Test**: In workspaces A/B, exercise valid/invalid `MUHIYA.md`, record and supersede decisions/findings, restart/new-session recall, explicit memory controls, secret rejection, corrupt/partial recovery, and all run modes. Current recall is 100%, cross-workspace leakage and current superseded recall are 0%, trailers remain hidden, and identical snapshots/tails are byte-stable.

### Tests for User Story 3

- [X] T037 [P] [US3] Write failing root-only project-instruction tests for missing/empty, UTF-8/BOM/newlines, exact/oversize 32 KiB, NUL, unreadable, outside symlink/junction, case normalization, and secret rejection in `internal/workspace/project_context_test.go`
- [X] T038 [P] [US3] Write failing additive migration, ledger record/supersede/forget/clear, provenance, 64-item/32-KiB bounds, workspace mismatch/isolation, rollback, permissions, and typed sidecar recovery tests in `internal/state/project_memory_test.go` and `internal/state/project_context_test.go`
- [X] T039 [P] [US3] Write failing deterministic boot snapshot, skills restore, final-trailer parser/filter, one-shot update, closed-tail order, restart identity, settled-history, prefix-shape, and unchanged gateway-wire tests in `internal/orchestrator/project_context_test.go`, `internal/orchestrator/memory_trailer_test.go`, `internal/orchestrator/prompt_stability_test.go`, `internal/orchestrator/restart_determinism_test.go`, `internal/orchestrator/request_assembly_test.go`, `internal/orchestrator/cachehit_guard_test.go`, `internal/gateway/marshal_determinism_test.go`, and `internal/gateway/gateway_test.go`
- [X] T040 [US3] CLI memory list/remember/forget/clear tests in `internal/command/memory_test.go` + early-loading deferred-submission tests (one send once runtime-ready) in `internal/tui/run_test.go`; TUI `/memory` flows tested in `tui_test.go`

### Implementation for User Story 3

- [X] T041 [US3] Implement canonical root `MUHIYA.md` loading, containment, UTF-8/NUL/size validation, newline/BOM normalization, deterministic hashing, and typed safe diagnostics in `internal/workspace/project_context.go`
- [X] T042 [US3] Add the append-only `project_memory_events` migration/indexes plus validated record/supersede/forget/clear/current-projection operations in `internal/state/db.go` and `internal/state/project_memory.go`
- [X] T043 [US3] Implement user-only-permission atomic typed `project_context.json` read/write, workspace mismatch rejection, corrupt backup, legacy-absence behavior, and applied hash/sequence cursors in `internal/state/session.go` and `internal/state/project_context.go`
- [X] T044 [US3] Implement deterministic boot/update renderers, reserved-tag escaping, strict final-trailer parsing, four-candidate bounds, secret-change rejection, and current-memory projection values in `internal/orchestrator/project_context.go`
- [X] T045 [US3] Add the fixed byte-stable memory-output contract and optional exact project-context boot block to PromptContext/SystemPrompt without changing tools, models, routing, or task policy in `internal/orchestrator/prompt.go`
- [X] T046 [US3] Add the bounded streaming trailer filter, completion-boundary observer, ledger callback, and atomic one-shot `<project-instructions-update>` / `<memory-update>` tail injection in `internal/orchestrator/engine.go` and `internal/orchestrator/history.go`
- [X] T047 [US3] Compose and persist new/resumed session snapshots from instructions/current memory/sorted skills before runtime-ready, restore exact boot bytes before mutable comparisons, ensure an early deferred submit sends once with first-turn context, and pass configured provider/MCP secrets to rejection checks in `internal/command/application.go`
- [X] T048 [US3] Implement `/memory list|remember|forget|clear` modal/line flows with confirmation, safe provenance, bounds diagnostics, and no dependency on full-screen paging in `internal/tui/actions.go`, `internal/tui/model.go`, and `internal/tui/run.go`
- [X] T049 [US3] Implement workspace-scoped `muhiyacode memory list|remember|forget|clear` Cobra commands using the same validation/store services in `internal/command/root.go`
- [X] T050 [US3] Add the conditional project-instructions and memory update tags, canonical tail order, and unchanged-follow-up budget to `specs/002-reasonix-agent-overhaul/contracts/request-assembly.md`
- [X] T051 [US3] Run the complete file/memory/trailer/resume/isolation/simple-line/stability matrix and record the independent verdict in `specs/005-terminal-memory-overhaul/benchmarks/checkpoints/us3.md`

**Checkpoint**: US3 independently provides safe, local, cross-session project orientation with user control and byte-stable prompt behavior.

---

## Phase 6: User Story 4 — Mouse-Native Terminal Navigation (Priority: P2)

**Goal**: Make transcript wheel, input caret, command rows, tool/subagent chips, modal buttons, transcript selection, and copy mouse-native while retaining complete keyboard fallback.

**Independent Test**: Execute every target in the automated coordinate/action matrix and in Windows Terminal plus one macOS/Linux terminal; repeat with mouse disabled. Target/action accuracy is 100%, release never double-invokes, grapheme/paste caret positions remain valid, copy or documented fallback succeeds, and every mouse task completes by keyboard.

### Tests for User Story 4

- [X] T052 [P] [US4] Write rectangle priority, wheel scope, press/release single-fire, padding, and resize-before-click tests in `internal/tui/mouse_test.go` (generation staleness is structurally avoided — the map is rebuilt every frame)
- [X] T053 [P] [US4] Wrote input caret-placement tests for ASCII and double-width CJK boundary (cell→rune mapping) in `internal/tui/selection_test.go` (tabs/emoji/combining/RTL/wrapped rely on the textarea's own clamping — nearest-boundary, no crash)
- [X] T054 [P] [US4] Write transcript drag-selection, plain-click mapping, OSC52 copy, Ctrl+C precedence, Esc-clear, and highlight-visibility tests in `internal/tui/selection_test.go` (keyboard selection-mode tests not included — that mode is not implemented; mouse selection is)
- [X] T055 [P] [US4] Write click/keyboard-equivalence tests for every command row, individual tool/subagent chip, and modal button in `internal/tui/mouse_test.go` (command run, tool expand, agent-view open, modal confirm — each dispatches the same path as its keyboard action)

### Implementation for User Story 4

- [X] T056 [US4] Implement immutable frame-owned InteractionMap construction, target priority, stale revision checks, and semantic action dispatch in `internal/tui/mouse.go`
- [X] T057 [US4] Enable Bubble Tea mouse mode and emit geometry/hit targets from the same frame/layout revision in `internal/tui/view.go` (map built during View(); uses AllMotion instead of CellMotion so hover works — deliberate deviation from mouse-interaction.md §1 recorded in commit/notes)
- [X] T058 [US4] Implement transcript-scoped wheel routing, drag selection, ANSI-free text assembly, OSC52 copy, and Ctrl+C precedence in `internal/tui/mouse.go`, `internal/tui/selection.go`, and `internal/tui/model.go` (selection is content-coordinate based, not stable-event-ID; highlight is applied at View() over visible lines only; mouse-driven — no keyboard selection mode; OSC52 failure is not terminal-detectable so no failure fallback notice)
- [X] T059 [US4] Implemented nearest-grapheme caret placement + focus on composer click via cell→rune column mapping over the textarea in `internal/tui/mouse.go` (`placeInputCaret`/`cellToRuneColumn`); the textarea clamps to a valid boundary for wrapped/short lines
- [X] T060 [US4] Implement clickable command rows, per-tool expansion (per-tool `expanded` OR global Ctrl+O), subagent detail selection, modal single invocation, and the existing global Ctrl+O fallback in `internal/tui/mouse.go`, `internal/tui/model.go`, and `internal/tui/view.go` (dedicated per-visible-target keyboard focus is not added; existing Ctrl+O / Tab / Alt+number keyboard paths remain)
- [ ] T061 [US4] Run the automated and physical mouse/copy/keyboard matrix and record terminal versions, native selection overrides, OSC52 outcomes, and verdicts in `specs/005-terminal-memory-overhaul/benchmarks/terminal-matrix/mouse.md`

**Checkpoint**: US4 independently delivers complete mouse interaction and copy with equivalent keyboard behavior.

---

## Phase 7: User Story 5 — Large Pastes Stay Compact and Complete (Priority: P2)

**Goal**: Represent large pasted text as compact atomic draft parts that can be surrounded by normal editing, inspected, removed, and sent without any fidelity loss.

**Independent Test**: Run boundary, Unicode/control/newline, >32-KiB, multiple-block, edit-around, lifecycle, click/keyboard, and fuzz cases. Every sent message equals the exact concatenation oracle, collapsed blocks occupy <=2 rows, no layout overflows, and cleared/abandoned blocks never reappear.

### Tests for User Story 5

- [X] T062 [P] [US5] Wrote 1,023/1,024-code-point, 5/6-line, CRLF/lone-CR/LF classification tests and exact-expansion fidelity incl. a >32-KiB paste with tabs/NUL/unicode in `internal/tui/paste_test.go`
- [X] T063 [P] [US5] Wrote `FuzzPasteRoundTrip` (seed corpus incl. placeholder-shaped/control bytes), a 200-trial randomized concatenation oracle, and exact-removal tests in `internal/tui/composer_paste_fuzz_test.go`
- [X] T064 [P] [US5] Wrote compact-bar, multiple-block, removal, and session-switch tests in `internal/tui/paste_test.go`/`composer_paste_fuzz_test.go`; the transcript keeps only the compact placeholder (no raw paste). (Internal expanded-scroll and busy-queue not separately tested; preview is line-bounded.)

### Implementation for User Story 5

- [X] T065 [US5] Implemented immutable paste blocks by ID (`m.pastes[id]` raw content), exact `logicalLineCount`, OR-threshold `isLargePaste`, and one-pass `expandPastes` reconstruction in `internal/tui/model.go`/`internal/tui/pastes.go` (via a placeholder stash over the textarea rather than a segmented-composer PastePart — same guarantees: exact bytes, atomic block)
- [X] T066 [US5] Implemented the 1-row collapsed paste bar, bounded control-safe expanded inspection (inspect modal preview), and independent per-block removal in `internal/tui/pastes.go`/`internal/tui/view.go` (caret-before/after-atom is via the placeholder text; the block is never split)
- [X] T067 [US5] Routed `tea.PasteMsg` (large→stash, small→inline), submit (exact expand then release), Esc/clear (`input.Reset`), and session switch (drop draft + stash) so blocks are never trimmed, truncated, duplicated, or released early in `internal/tui/model.go`/`internal/tui/actions.go`
- [X] T068 [US5] Added the paste-bar click region to the interaction map (opens the inspect/remove manager) plus the keyboard `/paste` command, in `internal/tui/mouse.go`/`internal/tui/view.go`/`internal/tui/actions.go`
- [X] T069 [US5] Ran the paste fidelity oracle + fuzz suite (all green) and recorded the boundary behavior and verdict in `specs/005-terminal-memory-overhaul/benchmarks/checkpoints/us5.md`

**Checkpoint**: US5 independently preserves every pasted byte in composition order while keeping the full-screen input stable and compact.

---

## Phase 8: User Story 6 — Clear, Coherent States on Every Screen (Priority: P3)

**Goal**: Complete the shared visual/layout system and make first-run, loading, idle, busy, streaming, tool/subagent, error, and too-small states immediately understandable without color dependence.

**Independent Test**: Render every state at 80x24/120x40, dark/light, `NO_COLOR`, Unicode/ASCII, RTL/mixed content, and around the 59/60x19/20 floor. All states are identifiable without color, all geometry comes from one Theme, and resize/state restoration produces no artifacts.

### Tests for User Story 6

- [X] T070 [P] [US6] Wrote single-source tests detecting hard-coded hex colors and magic render-floor/header breakpoints in the feature-logic files (they route through the palette + `m.theme`) in `internal/tui/theme_test.go`
- [X] T071 [P] [US6] Wrote first-run/busy-prestream/tool-running/subagent-running/error/too-small cue tests in `internal/tui/application_state_test.go` (runtime-loading is N/A — startup is synchronous; idle is intentionally quiet per US2)
- [X] T072 [P] [US6] Wrote NO_COLOR (no color escapes + legible cues), ASCII (no unicode-glyph leak), 59/60/61 floor-size, and RTL visual-profile tests in `internal/tui/visual_state_test.go`

### Implementation for User Story 6

- [X] T073 [US6] Migrated the render floor, header width breakpoints, and transcript gutter to the shared `Theme` (built once in `newTheme`, consumed via `m.theme`) preserving 003 semantics, in `internal/tui/theme.go`/`view.go`/`model.go` (palette+glyph tables were already single-source in `render.go`; lipgloss padding/border remain structural per-component calls)
- [X] T074 [US6] Explicit first-run (welcome cue), busy-prestream ("Thinking…"), streaming (activity+thinking), tool-running/subagent-running (spinner markers), error (notice), and actual-size too-small state presentation in `internal/tui/view.go` (runtime-loading N/A with synchronous startup; idle deliberately quiet per US2); validated by `application_state_test.go`
- [X] T075 [US6] Non-color cues: reverse-video selection, spinner/marker glyphs for activity/tool state, `!` + warning style for errors, textual paste placeholder, and NO_COLOR (bold/reverse fallback) + ASCII glyph degradation in `internal/tui/render.go` and `internal/tui/view.go`; validated by `visual_state_test.go`
- [ ] T076 [US6] Run all visual profiles and the accessibility identification review, recording captures, terminal metadata, row hashes, and verdicts in `specs/005-terminal-memory-overhaul/benchmarks/terminal-matrix/visual-states.md`

**Checkpoint**: US6 independently completes a coherent, accessible, resize-safe state system across all surfaces.

---

## Phase 9: Polish and Cross-Cutting Release Gates

**Purpose**: Synchronize documentation/security contracts, run full quality gates, and produce final before/after evidence and signoff.

- [X] T077 [P] Updated documentation for mouse/copy, memory (project context), bounded scrollback, and the cache-neutral boot boundary in `README.md` (TUI controls + mouse table + Project context and memory), `docs/architecture.md` (invariant 12), `docs/agent-design.md` (Project memory), and `docs/prompt-caching.md` (Feature 005 section)
- [X] T078 [P] Reviewed root containment, secret rejection, state permissions, clipboard behavior, and clear/forget confirmation against `docs/security.md`; findings recorded in `specs/005-terminal-memory-overhaul/security-review.md` (built surfaces only — composer paste T065-T069 flagged for re-review when built; two items flagged [machine-verify])
- [ ] T079 Run `gofmt`, `go vet ./...`, `go test ./... -count=1`, cross-platform build/tests, and `go test -race ./... -count=1` on a CGO-capable runner; record versions, commands, and results in `specs/005-terminal-memory-overhaul/benchmarks/checkpoints/final-quality.md`
- [ ] T080 Run all terminalbench scenarios from a clean improved worktree, compare baseline/improved without concurrent load, and store raw results plus comparison verdicts in `specs/005-terminal-memory-overhaul/benchmarks/improved/` and `specs/005-terminal-memory-overhaul/benchmarks/comparison.json`
- [ ] T081 Run the live improved cachebench with the exact baseline model/gateway/effort/workload, report cold/steady cache rates, provider tokens, request count, cost, and quality, and store results in `specs/005-terminal-memory-overhaul/benchmarks/cache/improved/` and `specs/005-terminal-memory-overhaul/benchmarks/cache/comparison.md`
- [ ] T082 Execute every automated and physical scenario in `specs/005-terminal-memory-overhaul/quickstart.md` on the required OS/terminal matrix and record the final acceptance signoff in `specs/005-terminal-memory-overhaul/benchmarks/signoff.md`
- [X] T083 Audited the change: agent loop/tool/model-routing/gateway-wire unchanged, legacy state readable, caches bounded with a visible marker (no hidden truncation), prefix more stable (web-probe fix); recorded in `specs/005-terminal-memory-overhaul/implementation-review.md`, with T020/T023 paging deferral and the machine-evidence gates documented honestly

---

## Dependencies and Execution Order

### Phase Dependencies

```text
Phase 1 Setup (measurement harness + clean baselines)
  └── Phase 2 Foundational (shared contracts/adapters)
        ├── US1 Instant input/resume ──> US2 Smooth updates/idle
        │          └───────────────────> US4 Mouse navigation
        ├── US3 Project context/memory
        ├── US5 Large paste (keyboard path; T068 waits for US4 mouse map)
        └── US6 Visual states (functional work can start; final migration integrates prior panes)

US1..US6 selected for release
  └── Phase 9 cross-cutting quality/evidence/signoff
```

- **Phase 1**: T001-T002 create the harness; run T003 and T004 sequentially on isolated clean worktrees so terminal and live cache measurements never contaminate each other.
- **Phase 2**: Depends on recorded baselines. T005, T006, T007, and T009 may start in parallel; T008 follows T007; T010 follows T006/T008; T011 closes the foundation.
- **US1**: Depends on Phase 2 and is the strict MVP. Tests T012-T015 run in parallel before implementation.
- **US2**: Depends on US1's TranscriptWindow/frame seam. Tests T026-T029 run in parallel.
- **US3**: Core loader/ledger/prompt work depends only on Phase 2 and can proceed in parallel with US1/US2; T037-T039 run in parallel, while T040/T047 early-loading integration waits for T021. Coordinate shared edits to `internal/contract/types.go` and `internal/command/application.go`.
- **US4**: Depends on Phase 2 Composer/Layout and US1 stable transcript IDs/window. Tests T052-T055 run in parallel; it can proceed in parallel with US2/US3 after US1.
- **US5**: Core keyboard/paste work depends only on Phase 2; T068 waits for US4's InteractionMap. Tests T062-T064 run in parallel.
- **US6**: State tests and Theme migration depend on Phase 2. Final call-site migration should follow the pane changes from US1/US2/US4/US5 to avoid duplicate edits; tests T070-T072 run in parallel.
- **Phase 9**: Depends on every story selected for release. T077/T078 may run in parallel; performance/cache runs T080/T081 must not overlap or share uncontrolled machine load.

### User Story Completion Order

| Story | Priority | Hard dependency | Independently testable outcome |
|---|---|---|---|
| US1 Instant Input | P1 | Foundation | 10/5,000 input and 30 resume trials meet latency/bounds with complete transcript access |
| US2 Smooth/Quiet | P1 | US1 | stream/scroll/resize/idle/endurance gates pass with stable completed rows |
| US3 Project Context | P1 | Foundation | instructions/memory recall, isolation, supersession, control, security, and deterministic tails pass |
| US4 Mouse Navigation | P2 | US1 + Foundation | all click/wheel/copy targets and keyboard equivalents pass |
| US5 Large Paste | P2 | Foundation; mouse integration waits US4 | exact concatenation oracle and compact-block lifecycle pass |
| US6 Visual States | P3 | Foundation; final integration after pane work | every state/profile is coherent, accessible, and resize-safe |

## Parallel Execution Examples

### User Story 1

```text
T012 state pagination tests
T013 TranscriptWindow tests
T014 startup/resume tests
T015 terminalbench input/resume tests
```

After those fail as expected, implementation proceeds T016 → T017 → T018/T019 → T020-T024 → T025.

### User Story 2

```text
T026 idle/timer tests
T027 stream/frame/scroll tests
T028 resize tests
T029 bridge coalescing tests
```

### User Story 3

```text
T037 workspace instruction security tests
T038 state ledger/sidecar tests
T039 prompt/trailer/restart tests
```

After T021 establishes runtime-ready behavior, run T040 command/line-mode/early-submit tests.

### User Story 4

```text
T052 hit-region/wheel tests
T053 caret-stop tests
T054 selection/copy tests
T055 command/chip/modal equivalence tests
```

### User Story 5

```text
T062 classification/fidelity tests
T063 composer fuzz tests
T064 block layout/lifecycle tests
```

### User Story 6

```text
T070 Theme single-source tests
T071 application-state tests
T072 visual-profile tests
```

## Implementation Strategy

### MVP First: User Story 1

1. Complete measurement-first Setup and capture clean baselines.
2. Complete Foundational contracts/adapters without breaking existing behavior.
3. Implement and validate US1 only.
4. Stop at T025 and verify the latency/resume/bounded-window MVP independently.
5. Do not claim performance success until stored evidence passes the contract gates.

### Incremental Delivery

1. Setup + Foundation → reproducible, compatibility-safe base.
2. US1 → instant input/resume MVP.
3. US2 → smooth streaming, stable scrolling/resizing, quiet idle/endurance.
4. US3 may land independently after Foundation → persistent project orientation.
5. US4 → mouse-native controls and copy.
6. US5 → compact, exact large-paste composition.
7. US6 → unified accessible state polish.
8. Phase 9 → full cross-platform, security, cache, quality, and release signoff.

### Parallel Team Strategy

After Foundation:

- **Track A**: US1 then US2 (transcript/performance).
- **Track B**: US3 (workspace/state/prompt/commands).
- **Track C**: US5 composer tests/core, then US4 mouse integration after US1 IDs are ready.
- **Track D**: US6 state tests/Theme preparation, with final call-site migration after Tracks A/C stabilize panes.

Coordinate `internal/tui/model.go`, `internal/tui/view.go`, `internal/command/application.go`, and `internal/contract/types.go`; tasks touching these shared files are intentionally not marked `[P]`.

## Notes

- Write each story's tests first and observe the expected failures before implementing.
- `[P]` marks only file-safe, dependency-safe parallel work; shared-file implementation tasks are sequential.
- Preserve unrelated dirty-worktree changes and use isolated clean worktrees for baseline/improved measurement.
- Provider token/cache/cost numbers come only from provider usage records; terminal CPU/memory comes from OS process metrics; label unavailable data honestly.
- A performance/cache/quality regression blocks completion even if the feature appears functionally complete.
- Commit or checkpoint after each task or coherent task group; stop at any story checkpoint to validate it independently.
