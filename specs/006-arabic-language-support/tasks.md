# Tasks: Arabic Language Support (RTL & Bidirectional)

**Input**: Design documents from `specs/006-arabic-language-support/`
**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: INCLUDED. This feature fixes two latent correctness bugs (the `bidi.ReverseString`+harakat mis-association and the per-rune width over-count) and must prove cache-neutrality — per the MuhiyaCode Constitution (Principles VI & X and the cache-affecting prefix-stability gate), the verification and prefix-stability tasks are **not** optional.

**Organization**: Tasks are grouped by user story so each can be implemented and tested independently. All changes are presentation-only and cache-neutral; the model always receives logical NFC Unicode on the user message and the stable prompt prefix stays byte-identical.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete tasks)
- **[Story]**: US1–US6 (setup/foundational/polish carry no story label)

## Path Conventions

Single Go project. All rendering work lives under `internal/tui/`, with small edits to `internal/state/`, `internal/command/`, `internal/contract/`, `internal/orchestrator/`, `go.mod`, and `docs/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Dependencies and shared test fixtures.

- [X] T001 [P] Promote `github.com/rivo/uniseg` from an indirect to a direct dependency (`go get github.com/rivo/uniseg@v0.4.7`) in `go.mod`/`go.sum`; verify `go build ./...`.
- [X] T002 [P] Add the shared Arabic/RTL test corpus in `internal/tui/testdata_rtl_test.go` (Go vars): plain Arabic, vocalized Arabic with harakat, LAM-ALEF words (لا), mixed Arabic + English / inline `code` / numbers / file paths / URLs / `f(x)`, English-dominant line with one Arabic word, very long lines, emoji, Arabic-Indic digits, and defective/leading combining marks — reused by every story's tests.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The single centralized RTL display pass and its primitives, on which US1–US4 depend.

**⚠️ CRITICAL**: No user-story work can begin until this phase is complete.

- [X] T003 [P] Create grapheme-cluster width helpers in `internal/tui/width.go` using `uniseg` (`displayWidth(string) int`, cluster-truncate, cluster iteration) treating harakat/combining marks as zero-width and CJK/emoji as wide — the replacement for per-rune `runewidth.RuneWidth` loops (research R4).
- [X] T004 [P] Complete Arabic shaping in `internal/tui/rtl.go`: add the mandatory LAM+ALEF ligature (U+FEF5–U+FEFC) and any missing contextual forms to `shapeArabic`.
- [X] T005 Replace the whole-line `bidi.ReverseString` with **per-run, grapheme-aware** reversal in `internal/tui/rtl.go` (reverse grapheme clusters so base+harakat stay together) — fixes the Go #50633 latent bug (depends on T004; same file).
- [X] T006 [P] Create the BiDi run/segment layer in `internal/tui/bidi.go`: force `bidi.DefaultDirection(bidi.RightToLeft)` for Arabic-primary lines, walk `Ordering.Run(i)` in visual order via `Run.Pos()`, and emit ordered directional runs (logical spans + direction).
- [X] T007 Build the `DisplayLine{visual, logical, cellMap, align}` producer and the glyph-cell→logical-span `cellMap` (ligature 1→N aware) in `internal/tui/bidi.go` (depends on T003, T004, T005, T006).
- [X] T008 Implement the centralized display pass `renderForDisplay(logical, width, mode, align) DisplayLine` in `internal/tui/rtl.go` per [contracts/rtl-render.md](./contracts/rtl-render.md): dispatch off/native/visual/auto, LTR fast-path byte-identical, purity/determinism; keep `RenderRTL` as a thin wrapper for back-compat (depends on T007).

**Checkpoint**: The display engine exists and is unit-testable; user stories can begin.

---

## Phase 3: User Story 1 - Read Arabic content correctly everywhere (Priority: P1) 🎯 MVP

**Goal**: Every display surface shows Arabic joined, right-to-left, and right-aligned — readable during streaming and after wrapping.

**Independent Test**: Feed the corpus into each surface (transcript text, tool rows, chrome, tables) and confirm shaping, RTL order, and right alignment against a reference; a long Arabic reply wraps with each line individually shaped.

### Tests for User Story 1

- [X] T009 [P] [US1] Golden-render tests (shaping, RTL order, alignment, streaming prefix, harakat attach) across surfaces in `internal/tui/rtl_test.go` using the T002 corpus.
- [X] T010 [P] [US1] Width tests (harakat zero-width, LAM-ALEF, defective clusters, presentation forms = 1 cell) in `internal/tui/width_test.go`.

### Implementation for User Story 1

- [X] T011 [US1] Replace per-rune width loops (`oneLine`, `truncateMiddle`, `truncateLeft`) in `internal/tui/view.go` with the T003 cluster-width helpers.
- [X] T012 [P] [US1] Replace per-rune width loops (`wrapPlain`, `runePrefix`, table `measure`) in `internal/tui/render.go` with cluster-width helpers; confirm wrap-then-shape order in `RenderMarkdown`.
- [X] T013 [US1] Route transcript text blocks (user, assistant, system — streamed + settled) through `renderForDisplay` with alignment in `internal/tui/view.go` and `internal/tui/render.go` (replace the scattered `RenderRTL` calls).
- [X] T014 [US1] Route the remaining transcript surfaces — tool rows (label/target/outcome/expanded diff), agent chips, task-summary line, first-run hint — through the display pass in `internal/tui/view.go`.
- [X] T015 [US1] Route the chrome surfaces — thinking line, plan/goal line, notices, modal (title/message/choices), command-palette rows, header (workspace path + agent-view labels) — through the display pass in `internal/tui/view.go`.
- [X] T016 [US1] Route markdown **table cells** through the display pass and cluster-width padding in `internal/tui/render.go` (`renderTable`).
- [X] T017 [US1] Implement right-alignment: apply `RTL.Align` (auto → right for dominant-RTL blocks) in the display pass and surface rendering — the setting is currently stored but unused (`internal/tui/rtl.go`, `internal/tui/view.go`).

**Checkpoint**: Arabic is fully readable on every surface. **This is the MVP** (readable Arabic responses deliver standalone value).

---

## Phase 4: User Story 2 - Type Arabic in the composer (Priority: P1)

**Goal**: Typing Arabic joins live, reads right-to-left, sits right-aligned, and the cursor lands correctly — the reported screenshot defect is gone.

**Independent Test**: Type a multi-word Arabic sentence; confirm live joining, RTL order, right alignment, correct caret; edit mid-string and confirm the edit lands where expected.

### Tests for User Story 2

- [X] T018 [P] [US2] Composer RTL render + caret-mapping tests in `internal/tui/composer_rtl_test.go`: typed Arabic joins/RTL/right-aligned; caret maps logical→visual; Backspace/Delete/motion stay logical; `Value()` stays clean logical.

### Implementation for User Story 2

- [X] T019 [US2] Expose a minimal read-only caret accessor (logical line + column) on the Composer in `internal/tui/composer.go` if the embedded textarea does not already provide it (no editing-behavior change).
- [X] T020 [US2] Implement RTL custom-render of the composed line in `internal/tui/composer.go`: for RTL lines, render from the logical `Value()` via `renderForDisplay`, draw the caret at the mapped visual column, and right-align; pure-LTR lines keep the textarea's own rendering; editing/movement stay logical (research R2).
- [X] T021 [US2] Wire the composer's RTL render into `renderInput` in `internal/tui/view.go`; verify the large-paste placeholder and the logical submitted value are unaffected.

**Checkpoint**: Reading (US1) and writing (US2) both work — a full Arabic conversation is possible.

---

## Phase 5: User Story 3 - Mixed Arabic + English/code/numbers/paths (Priority: P2)

**Goal**: In mixed lines, Arabic reads RTL while identifiers, `code`, numbers, paths, and URLs stay intact and left-to-right in their correct positions; pure-LTR is unchanged.

**Independent Test**: Render/type the mixed corpus and confirm each segment's direction and the overall reading order against a reference, in both transcript and composer.

### Tests for User Story 3

- [X] T022 [P] [US3] Mixed-content tests in `internal/tui/bidi_test.go`: Arabic around `main.go`, `42`, `internal/tui/rtl.go`, a URL, and `f(x)` keep those tokens intact & positioned; pure-LTR byte-identical; a single foreign token does not flip the line.

### Implementation for User Story 3

- [X] T023 [US3] Implement the technical-token tokenizer in `internal/tui/bidi.go`: pre-segment file paths, URLs, inline `code` spans, and bracketed calls as **atomic LTR runs**, working around the `x/text/bidi` bracket bug (#72089) and weak-separator fragmentation (research R5/R8).
- [X] T024 [US3] Integrate the tokenizer into the run walk (T006/T007) so technical tokens stay whole within RTL context, and refine base-direction resolution (force RTL only for Arabic-dominant lines; leave English-dominant lines to first-strong) in `internal/tui/bidi.go`.

**Checkpoint**: Arabic and English interoperate correctly in both directions.

---

## Phase 6: User Story 4 - Clean model I/O and logical copy (Priority: P2)

**Goal**: The model receives normalized logical Arabic; copy yields logical text; history/memory round-trip; the stable prefix is byte-identical.

**Independent Test**: Inspect the recorded request bytes for an Arabic prompt (NFC logical, no presentation forms); copy an Arabic passage and paste into an external editor (byte-for-byte logical); confirm the stable prefix is unchanged.

### Tests for User Story 4

- [X] T025 [P] [US4] Logical-copy round-trip tests in `internal/tui/selection_test.go` (extend): selecting Arabic/mixed copies **logical** text (no presentation forms, no reversal); byte-for-byte round-trip; whole-message selection exact.
- [X] T026 [P] [US4] NFC-send test: an Arabic prompt reaches the request as NFC logical Unicode with zero presentation-form code points, in `internal/tui/model_test.go` (or `internal/orchestrator/`).
- [X] T027 [P] [US4] **Prefix-stability / cache-neutrality test** (constitutional gate): growing an Arabic transcript and sending an Arabic user message leaves the stable prompt prefix byte-identical, in `internal/orchestrator/prompt_stability_test.go` (extend) or `internal/orchestrator/rtl_cache_test.go`.

### Implementation for User Story 4

- [X] T028 [US4] Rewrite selection copy in `internal/tui/selection.go` to resolve the selection through the per-line `cellMap` to **logical** text (anchor selection in logical offsets) instead of reading the visual `transcriptContent` — fixes the corrupt-copy bug (FR-015).
- [X] T029 [US4] Maintain the per-line logical shadow + `cellMap` for the rendered transcript (emitted by the display pass) so selection/copy can map visual→logical, in `internal/tui/view.go` and `internal/tui/selection.go`.
- [X] T030 [US4] Normalize the submitted user message to Unicode NFC at the submit boundary (`internal/tui/model.go` submit path and `internal/tui/run.go` line mode) using `golang.org/x/text/unicode/norm`; do NOT normalize the cached prefix or project-context files (keep the cache untouched).

**Checkpoint**: Model I/O, history, and clipboard all carry clean logical text; cache proven neutral.

---

## Phase 7: User Story 5 - Configuration & graceful degradation (Priority: P3)

**Goal**: RTL modes/alignment behave predictably and apply live; no crashes, corruption, or double-reversal; incapable terminals degrade legibly.

**Independent Test**: Exercise each mode/alignment across a BiDi-capable and a legacy terminal; run the fuzz corpus; confirm no double-reversal, no crash, live setting changes.

### Tests for User Story 5

- [X] T031 [P] [US5] Robustness/fuzz tests in `internal/tui/rtl_fuzz_test.go`: harakat + emoji + Arabic-Indic digits + very-long/defective/mixed inputs never crash/hang/corrupt (render + composer).
- [X] T032 [P] [US5] Mode/alignment + no-double-reversal tests in `internal/tui/rtl_test.go`: `off`/`visual`/`native`/`auto` per [contracts/rtl-render.md](./contracts/rtl-render.md); pure-LTR byte-identical; `native` performs no app-side reordering.

### Implementation for User Story 5

- [X] T033 [US5] Emit terminal BiDi negotiation at TUI startup/teardown — BDSM explicit `CSI 8 l` for `auto`/`visual`, implicit `CSI 8 h` for `native`, restored on exit — in `internal/tui/run.go` / `internal/tui/model.go` Init (research R7).
- [X] T034 [US5] Ensure RTL `mode`/`align` changes apply live (next render, no restart, no disruption to an in-flight task); verify the settings flow through `internal/tui/actions.go` and `internal/tui/view.go`.
- [X] T035 [US5] Implement graceful degradation for terminals/fonts lacking Arabic shaping (legible fallback, never corrupted glyph sequences) in the display pass (`internal/tui/rtl.go`).
- [X] T036 [US5] Extend the `doctor` RTL diagnostics in `internal/command/root.go`: print mode/align, a mixed sample, the negotiation emitted, and a copy round-trip check (per [contracts/rtl-settings.md](./contracts/rtl-settings.md)).

**Checkpoint**: RTL is robust and configurable everywhere.

---

## Phase 8: User Story 6 - Optional Arabic interface locale (Priority: P4, stretch)

**Goal**: The interface chrome can optionally be shown in Arabic; English remains the default.

**Independent Test**: Toggle the interface locale to Arabic; confirm hints/labels/command descriptions render translated, shaped, RTL, right-aligned; English unchanged when default.

### Tests for User Story 6

- [ ] T037 [P] [US6] Locale-toggle tests (chrome strings translated + shaped when `ar`; English default unchanged) in `internal/tui/locale_test.go`.

### Implementation for User Story 6

- [ ] T038 [P] [US6] Add a `uiLocale` setting (`en`|`ar`, default `en`) to `internal/contract/types.go` with validation and `config set uiLocale` in `internal/state/config.go`.
- [ ] T039 [US6] Provide Arabic translations for chrome strings (key hints, command descriptions, first-run guidance) and render them through the display pass in `internal/tui/` (gated on `uiLocale`, English default always available).

**Checkpoint**: Full Arabic experience available for users who opt in.

---

## Phase 9: Polish & Cross-Cutting Concerns

- [X] T040 [P] Update `docs/` (README.md RTL/keymap section, docs/agent-design.md, docs/architecture.md) to document Arabic/RTL support, the modes, and the presentation-only / cache-neutral guarantee.
- [X] T041 Run `go fmt ./...`, `go vet ./...`, and `go test ./... -count=1`; fix gaps and ensure gofmt-clean.
- [ ] T042 Verified-improvement evidence (Constitution X): run a realistic multi-turn Arabic session vs an equivalent English session (same model/gateway/effort), record cache/token numbers honestly and before/after Arabic-rendering evidence under `specs/006-arabic-language-support/benchmarks/`.
- [ ] T043 Execute the [quickstart.md](./quickstart.md) scenarios 1–7 and record the sign-off.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup; **blocks all user stories**.
- **User Stories (Phases 3–8)**: all depend on Foundational. US1, US2, US5 are independent of each other. US3 refines the Foundational run-walk (best after US1). US4 depends on the `cellMap` (Foundational T007) and, for copy, on US1's transcript rendering (T029). US6 depends on the display pass (any prior story).
- **Polish (Phase 9)**: after all desired stories.

### User Story Dependencies

- **US1 (P1)**: after Foundational. Independent — the MVP.
- **US2 (P1)**: after Foundational. Independent of US1 (composer vs transcript).
- **US3 (P2)**: after Foundational; best after US1 (shared run-walk). Independently testable.
- **US4 (P2)**: after Foundational; copy path builds on US1's transcript render (T029). NFC-send and prefix-stability are independent.
- **US5 (P3)**: after Foundational. Independent.
- **US6 (P4)**: after any story that establishes the display pass. Independent, optional.

### Within Each User Story

- Tests (marked [P]) are written first and must FAIL before implementation.
- Foundational primitives (width, shaping, reversal, run-walk, cellMap) before the display pass; display pass before surface routing.

### Parallel Opportunities

- Setup: T001, T002 in parallel.
- Foundational: T003, T004, T006 in parallel; T005 after T004 (same file); T007 after T003–T006; T008 after T007.
- Once Foundational completes: US1, US2, and US5 can proceed in parallel (different files/areas).
- All per-story test tasks marked [P] can run together.

---

## Parallel Example: Foundational

```bash
Task: "T003 [P] grapheme-cluster width helpers in internal/tui/width.go"
Task: "T004 [P] LAM-ALEF ligature + forms in internal/tui/rtl.go"
Task: "T006 [P] BiDi run/segment layer in internal/tui/bidi.go"
# then T005 (rtl.go) → T007 (bidi.go) → T008 (rtl.go)
```

## Parallel Example: User Story 1 tests

```bash
Task: "T009 [P] [US1] golden-render tests in internal/tui/rtl_test.go"
Task: "T010 [P] [US1] width tests in internal/tui/width_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1)

1. Phase 1 Setup → 2. Phase 2 Foundational (blocks all) → 3. Phase 3 US1 (read).
4. **STOP and VALIDATE**: Arabic responses are readable on every surface. This alone is a shippable increment.

### Incremental Delivery

1. Setup + Foundational → engine ready.
2. US1 (read) → MVP. 3. US2 (write) → full conversation. 4. US3 (mixed) → coding content. 5. US4 (clean I/O + copy) → correctness/interchange. 6. US5 (config/robustness) → hardening. 7. US6 (locale) → optional full-Arabic UI.

### Notes

- All work is presentation-only and cache-neutral; T027 gates the prefix-stability guarantee (Constitution III/VI/X).
- Two latent bugs are fixed en route: the `bidi.ReverseString`+harakat mis-association (T005) and the per-rune width over-count (T003/T011/T012).
- [P] = different files, no incomplete-task dependency. Verify tests fail before implementing. Commit after each task or logical group. Stop at any checkpoint to validate a story independently.
