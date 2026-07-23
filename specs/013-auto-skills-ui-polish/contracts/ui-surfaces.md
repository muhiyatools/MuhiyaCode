# Contract: UI Surfaces — Keys, Footer, To-dos, Commands

**Feature**: `013-auto-skills-ui-polish` | Covers FR-012..FR-018, FR-025..FR-030 | Amends feature 003/g-series TUI contracts (keys, mode line, todos)

## 1. Keyboard Map (final state)

| Key | Behavior | Change |
|-----|----------|--------|
| Shift+Tab | Cycle permission mode (`normal ⇄ auto-accept`) | unchanged (now the ONLY mode switch) |
| Tab | Command autocomplete when input starts with `/`; otherwise nothing | agent-cycle branch **removed** |
| ← / → | Empty composer & not in `/` palette: previous / next agent view; any text present: caret movement (falls through to composer) | **new** |
| Alt+1..9 | Direct jump to agent N | unchanged |
| Esc | Selection → agent view → cancel task → clear input (existing precedence) | unchanged |
| Ctrl+T | — | **removed** (no replacement) |
| Up/Down, PgUp/PgDn, Ctrl+C/D/P/S | unchanged | — |

- C1.1 Agent ring order: `main → agent[0] → … → agent[last] → main` for →; exact inverse for ←. Single/no agents: keys are harmless no-ops on an empty composer.
- C1.2 Composer editing regression bar: with any composer text, ←/→ MUST reach the textarea unmodified (Clarification Q1; SC-010).

## 2. Footer / Mode Line

- C2.1 Left hints become exactly: `Esc stop · ←/→ agents · / commands` (Ctrl+T and Tab-agents hints deleted, FR-029).
- C2.2 Right cluster order: `[N skills] · [permission chip] · [effort chip]` — the permission chip renders in EVERY mode: `normal` in muted style, `auto-accept` in warning style (Clarification Q4). The effort chip stays flush right (unchanged).
- C2.3 A second footer line renders `Shift + Tab to cycle` in faint style, right-aligned beneath the permission/effort cluster (FR-015). Narrow terminals: this hint line truncates/drops before the mode line does; it never corrupts layout (spec edge case).
- C2.4 The hint text is the literal string `Shift + Tab to cycle`.

## 3. To-do Panel

- C3.1 Visible iff a task is active AND the checklist has items; no user toggle exists (FR-025/FR-026).
- C3.2 Retire rules unchanged: empty list / fully-retired list renders nothing; idle sessions render nothing.
- C3.3 The `todoVisible` state and its golden cases are deleted; new goldens pin always-visible-while-busy.

## 4. Command Surface

| Command | Change | Post-change behavior |
|---------|--------|----------------------|
| `/logout` | description renamed | Palette/autocomplete shows "Log Out of Account"; behavior unchanged |
| `/errors` | **removed** (row + dispatch + panel) | Typing it yields the standard unknown-command notice (FR-018) |
| `/permissions`, `/mode` | **removed** (row + dispatch + choice modal) | Standard unknown-command notice; Shift+Tab is the only mode switch |
| `/skills` | kept | Manual queueing flow unchanged (FR-008 precedence) |
| `/context` | kept | Renders the simplified card ([display-formats.md](display-formats.md) §4) |
| `/reasoning`, `/effort` | kept | Chooser subtitle: "How hard the model thinks."; level descriptions carry no "DeepSeek maps…" text (FR-016) |

- C4.1 Busy-allowed command list drops `/errors` (currently allowed while busy) — the remaining busy-allowed set is `/reasoning`, `/effort`, `/context`.
- C4.2 Harness-friction UI is fully removed: `formatHarnessEvents` panel, the `⚠ N harness` summary marker, and every user-visible "harness friction" string. Engine recording (`recordHarnessEvent`, `HarnessEvents()`, `TaskStats.HarnessEvents`) is retained as telemetry (research R8) — it must remain invisible to the UI.
- C4.3 Orphan sweep (FR-017/FR-030, SC-007): after implementation, a case-insensitive repo sweep for `harness friction`, `/errors`, `/permissions`, `/mode`, `Clear API key`, `DeepSeek maps`, `Ctrl+T`, `Tab agents` over user-visible strings (TUI renders, notices, command descriptions, docs) must return zero hits; engine-internal identifiers and telemetry comments are exempt.

## 5. Verification

- C5.1 Key tests: Shift+Tab cycle pinned (existing); new tests for ←/→ (empty vs non-empty composer, ring order both directions, no-agent no-op); Tab-with-text-and-no-slash does nothing; Ctrl+T unhandled.
- C5.2 Render goldens: mode line + hint sub-line in both permission modes; todos always-visible; task summary without friction marker; palette without removed rows; renamed `/logout` row.
- C5.3 Unknown-command behavior test for `/errors`, `/permissions`, `/mode`.
- C5.4 Docs: `README.md`, `docs/agent-design.md`, `docs/architecture.md` updated where they describe removed commands, Ctrl+T, Tab agent switching, or abbreviated token displays (constitution workflow gate).
