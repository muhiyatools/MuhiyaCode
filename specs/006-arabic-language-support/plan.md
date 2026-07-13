# Implementation Plan: Arabic Language Support (RTL & Bidirectional)

**Branch**: `006-arabic-language-support` | **Date**: 2026-07-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/006-arabic-language-support/spec.md`

## Summary

Make Arabic a first-class language across the entire MuhiyaCode terminal experience — reading, writing, mixed Arabic/English content, and clean interchange with the model — by **extending the existing display-time RTL pipeline** (`internal/tui/rtl.go`: contextual shaping + `golang.org/x/text/unicode/bidi` reordering) rather than replacing it.

The work is presentation-only and cache-neutral: the model always receives the original **logical** Unicode on the user message; the deterministic stable prompt prefix, tool schemas, model routing, and security model are untouched. The plan centralizes one RTL display pass and routes **every** user-visible surface through it, adds the currently-missing right-alignment, makes the **composer** render Arabic shaped/RTL/right-aligned with a correct cursor (the reported screenshot defect), and fixes selection/copy so it yields **logical** text (today it copies the visually-shaped/reversed transcript — corrupt). Correct handling of mixed bidi runs (numbers, code, paths kept LTR) and graceful degradation across terminals complete the picture.

## Technical Context

**Language/Version**: Go 1.26.x (existing module `github.com/muhiya/muhiyacode`).

**Primary Dependencies** (already in `go.mod`): `charm.land/bubbletea/v2`, `charm.land/bubbles/v2` (textarea, viewport), `charm.land/lipgloss/v2`, `github.com/charmbracelet/x/ansi`, `github.com/mattn/go-runewidth`, `golang.org/x/text/unicode/bidi` (used in `rtl.go`), and `github.com/rivo/uniseg` (currently indirect — **promote to direct**; justified because per-rune `go-runewidth.RuneWidth` returns 1 for Arabic harakat, so cluster-aware width + cursor stepping must come from `uniseg`). One **optional** new dependency, `github.com/go-text/typesetting/shaping`, gives higher-fidelity Arabic shaping/ligatures; the v1 fallback (extend the `shapeArabic` table with the LAM+ALEF ligature) needs no new dependency (Principle IX — decided in research.md R4).

**Storage**: N/A for new data. RTL preferences already persist in `settings.json` (`RTL.Mode`, `RTL.Align`). No new files, no schema migration.

**Testing**: `go test ./... -count=1`, `go vet`, `go fmt`; golden-render assertions for shaping/order/alignment; logical round-trip (copy/paste) tests; width tests for presentation forms + harakat; and a prefix-stability test proving Arabic content leaves the cached prefix byte-identical.

**Target Platform**: Cross-platform terminals — Windows Terminal + legacy conhost, macOS Terminal/iTerm, Linux VTE. Existing cross-platform posture is preserved.

**Project Type**: Single-project Go terminal application (agent CLI + TUI). All changes live under `internal/tui/` plus small settings/diagnostics touches.

**Performance Goals**: Typing stays instant — RTL work happens only at render time and only when a line actually contains RTL characters (`IsRTL` fast-path), so pure-LTR sessions are byte-identical and unaffected. Per-line shaping/reordering is O(line length) over the bounded transcript window; the transcript render already runs off the typing hot path (memoized per item), and the composer reshape runs once per keystroke over a single short line.

**Constraints**: Presentation-only and cache-neutral (stable prefix byte-identical); model receives logical Unicode on the user message only; local-only, no new network calls; no fork of the Bubbles textarea unless justified (Improve-Don't-Rewrite); graceful degradation on incapable terminals; no change to path containment, workspace trust, mutation approval, secret redaction, or command blocking.

**Scale/Scope**: Bounded — a windowed transcript (hundreds of entries) and a single-composer input. ~8–12 source files touched under `internal/tui/`, all additive or localized.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment | Status |
|-----------|------------|--------|
| I. Correctness before optimization | RTL correctness (readable output, correct editing, logical copy, logical model I/O) is the goal; no optimization is traded against it. | ✅ PASS |
| II. Cache efficiency without quality loss | Feature is presentation-only; it neither improves nor degrades cache. It must not remove/reorder model-visible content. | ✅ PASS |
| III. Deterministic stable prefix | RTL rendering never enters the system prompt/tool schemas; no locale-dependent formatting in the prefix; no per-request mode switching there. Enforced by a prefix-stability test with Arabic content (SC-006). | ✅ PASS (gated by test) |
| IV. Separation of dynamic/cached | Arabic content rides the user message (dynamic); RTL is a TUI display transform. Nothing new is placed earlier in the request. | ✅ PASS |
| V. No redundant retransmission | History/memory stay logical and append-only; no re-sending, no rewriting introduced. | ✅ PASS |
| VI. Honest measurement | Verification uses real multi-turn Arabic/mixed sessions; cache read/write/tokens taken from provider usage, labeled honestly. | ✅ PASS (in verification) |
| VII. Reference: DeepSeek Reasonix | This feature is **not** cache-affecting, so the Reasonix cache study is not triggered; research.md records why it is cache-neutral. | ✅ PASS (documented) |
| VIII. Improve, don't rewrite | Extend `rtl.go` + centralize the existing `RenderRTL`; the composer is handled by post-processing the textarea's rendered output, not by forking the widget. Any deeper widget integration is justified in Complexity Tracking. | ✅ PASS (see risk) |
| IX. Clean, secure, provider-compatible | `go fmt`/`vet`/`test` gates; no security-model change; logical Unicode is standard and provider-agnostic; no new dependency unless justified. | ✅ PASS |
| X. Verified improvements | Before/after with realistic Arabic sessions + golden/round-trip/fuzz tests; results stored under this feature dir. | ✅ PASS (in verification) |

**Initial gate: PASS.** One risk to watch (tracked in Complexity Tracking): the composer cursor mapping needs custom-rendering; research resolved the approach before implementation.

**Post-Design Re-check (after Phase 1): PASS.** Research confirmed the design stays presentation-only and cache-neutral (SC-006 prefix-stability test gates III/IV); it extends `rtl.go`/`RenderRTL` and keeps the textarea as the editor (VIII — the composer custom-render is presentation, not a fork, and is justified in Complexity Tracking); it fixes two latent correctness bugs found in the audit (the `ReverseString`+harakat mis-association and the per-rune width over-count) which strengthens I/IX; dependencies stay within `go.mod` except one *optional* shaping engine (IX — justified, with a no-new-dep fallback). No new Constitution violations were introduced by the design; Complexity Tracking captures the one justified extension.

## Project Structure

### Documentation (this feature)

```text
specs/006-arabic-language-support/
├── plan.md              # This file (/speckit-plan)
├── research.md          # Phase 0 — approach decisions (input, copy-logical, wrapping, capability, width)
├── data-model.md        # Phase 1 — logical/visual text model, directional runs, RTL settings
├── quickstart.md        # Phase 1 — how to validate Arabic support end-to-end
├── contracts/
│   ├── rtl-render.md     # The display-pass behavioral contract (shaping/order/alignment, purity, determinism)
│   ├── rtl-settings.md   # RTL mode/alignment settings + `config`/`doctor` surface contract
│   └── logical-io.md     # Logical-text guarantees for model send, history/memory, and clipboard copy
├── checklists/
│   └── requirements.md   # Spec quality checklist (from /speckit-specify)
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/tui/
├── rtl.go            # EXTEND: shaping + bidi reorder; add alignment + a single display entry point (renderForDisplay)
├── bidi.go           # NEW (optional split): directional-run helpers, logical↔visual column map for copy
├── composer.go       # EXTEND: RTL display of the input (shape/reorder/right-align) with cursor mapping; Value() stays logical
├── view.go           # EDIT: route EVERY surface (tools, modals, notices, header, plan, agent chip, table, hint, summary) through the display pass; apply alignment
├── render.go         # EDIT: RenderMarkdown wrap-then-shape order confirmed; table cells shaped; width for harakat
├── selection.go      # EDIT: copy resolves to LOGICAL text (fix visual-copy bug) via the logical layer
└── *_test.go         # NEW/EXTEND: golden render, round-trip copy, width, robustness/fuzz, prefix-stability

internal/state/config.go     # EDIT (small): capability detection default for "auto" mode; validation unchanged
internal/command/root.go     # EDIT (small): extend the `doctor` RTL diagnostics to cover composer + copy round-trip
internal/contract/types.go   # (unchanged) RTL settings already present
docs/…                       # UPDATE: document Arabic/RTL support (README + agent/architecture where UI behavior is described)
```

**Structure Decision**: Single Go project, existing layout. All rendering changes are contained in `internal/tui/` and pass through one centralized display function so no surface is left unhandled and behavior stays uniform. Settings and diagnostics get small, localized edits. No new top-level modules.

## Complexity Tracking

> Filled only where a Constitution Check risk needs justification.

| Risk / Potential Violation | Why it may be needed | Simpler Alternative & Decision |
|-----------|------------|-------------------------------------|
| Composer RTL requires **custom-rendering** the RTL input line rather than post-processing the textarea's output (tension with Principle VIII "don't rewrite the widget"). | Research proved post-processing is impossible: the widget bakes in the cursor column, wrapping, and ANSI/padding, and offers no API to reposition the caret afterward. Correct RTL editing needs the logical caret mapped to a visual column after per-run shaping+reordering. | **Decision (research-confirmed):** keep the textarea as the logical **editor** (state, history, grapheme navigation, paste, and unchanged **logical** editing/movement); take over **display** of RTL lines only — render them from the logical buffer + caret index, drawing the caret via the cell map. This is an extension (presentation), not a fork; LTR editing is untouched. Add at most a minimal read-only caret accessor if the widget doesn't expose line+column. |
| A logical↔visual column map is needed so selection copy returns logical text. | Today copy reads the visually-shaped transcript and corrupts Arabic; FR-015/SC-004 require a logical round-trip. | **Decision:** carry a per-line logical shadow with an index map produced by the same display pass (single source of truth), rather than attempting to invert presentation forms + reordering ad hoc at copy time. Rejected the ad-hoc inversion because mixed bidi runs make it fragile. |
