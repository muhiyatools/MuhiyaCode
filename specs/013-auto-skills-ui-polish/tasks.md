# Tasks: Automatic Skill Use & Interface Polish

**Input**: Design documents from `/specs/013-auto-skills-ui-polish/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included where the constitution mandates them (Principles VI/X: before/after measurement, prefix-stability check) and where existing suites pin the behavior being changed (goldens/wiring inventories must be updated in the same task as the change, or the suite goes red). No speculative TDD tasks.

**Organization**: Tasks are grouped by user story so each story is an independently testable increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 (auto skills, P1) · US2 (token/cache truth, P2) · US3 (command surface, P3) · US4 (todos & arrows, P4)

## Path Conventions

Single Go project at repo root: `internal/<pkg>/…`, docs in `docs/`, feature artifacts in `specs/013-auto-skills-ui-polish/`.

---

## Phase 1: Setup

**Purpose**: Confirm a green baseline and capture the Constitution X "before" evidence while the pre-feature binary is still at hand.

- [X] T001 Verify green baseline on the branch point: `go fmt ./...`, `go vet ./...`, `go test ./... -count=1` all clean; note the merge-base commit hash in specs/013-auto-skills-ui-polish/bench-notes.md (create the file)
- [ ] T002 [P] Capture the Constitution X baseline (quickstart §5.1): build the pre-feature binary from the merge-base, run the standard multi-turn gauntlet 3× (live gateway — flag to the owner for cost approval before running), plus 3× session-start timing; record steady-state hit rate, token totals, and startup times in specs/013-auto-skills-ui-polish/bench-notes.md — **DEFERRED: owner cost approval**

---

## Phase 2: Foundational (Blocking Prerequisites)

**No foundational tasks.** The four stories share no blocking scaffolding — they touch disjoint concerns (skills pipeline / accounting & displays / command surface / keybindings & todos) and the repo infrastructure already exists. Cross-story same-file coordination is handled in Dependencies & Execution Order below.

**Checkpoint**: After Phase 1, any user story phase may begin.

---

## Phase 3: User Story 1 — Automatic skill find & use (Priority: P1) 🎯 MVP

**Goal**: The agent sees an all-roots skill catalog in its stable prefix, decides relevance itself, reads a skill via the new `read_skill` tool before the related work, keeps applying it through the task, and equips sub-agents via `run_subagent.skills`. Manual `/skills` keeps precedence; duplicates never retransmit.

**Independent Test**: quickstart §1 — with a distinctive workspace skill and a home-folder skill installed, a matching prompt triggers `read_skill` before related work and visibly shapes output; an unrelated prompt loads nothing; manual queueing dedupes; a delegation carries `skills:` in its brief.

### Implementation for User Story 1

- [X] T003 [P] [US1] Harden all-roots discovery for catalog use in internal/workspace/skills.go (documented precedence order, first-wins dedupe, malformed-skip, 40-bound — extend, don't rewrite `DiscoverSkills`); extend internal/workspace/skills_scope_test.go with precedence/dedupe/malformed cases (contract skills-autouse §1)
- [X] T004 [P] [US1] Rewrite the skills prefix hint in internal/instructions/prompt.go per contract skills-autouse §5 (read_skill as the single load path; read-before-work; apply throughout; blend with own reasoning; multi-skill; none-when-unrelated; `run_subagent.skills` equipping; manual-block precedence) and set the registry entry's MentionsTools to ["read_skill","run_subagent"]; update any instruction-registry/drift tests that pin the old hint
- [X] T005 [US1] Replace `workspaceSkillListings` with an all-roots listing builder in internal/command/skills.go (absolute Path retained internally, name+description for render, 200-rune single-line descriptions, 40-bound); keep `listSkills`/`loadSkillBody` manual flow untouched (depends on T003)
- [X] T006 [US1] Change the rendered SKILLS section to the path-free byte-fixed format in internal/orchestrator/prompt.go (contract skills-autouse §2.2) and update internal/orchestrator/skills_prompt_test.go goldens (depends on T004, T005)
- [X] T007 [US1] Wire the catalog through session build in internal/command/runtime_build.go: PromptContext.Skills = all-roots listings; add a SkillCatalog field to orchestrator.EngineConfig and pass the same slice; keep the project-context skills snapshot consistent with the new listing content (depends on T005)
- [X] T008 [US1] Add the engine-side session catalog + per-task provided-skills state in internal/orchestrator/engine.go and internal/orchestrator/turnloop.go: frozen byName index; at task start scan the submitted prompt for `<skill name="` markers to seed providedSkills; clear per task (data-model §1.3; depends on T007)
- [X] T009 [US1] Implement the `read_skill` tool: new internal/orchestrator/skills_tool.go (case-insensitive catalog lookup, bounded body load via an injected loader, already-provided pointer notice, unknown-name error with no path disclosure — contract skills-autouse §3); definition in internal/orchestrator/definitions.go; dispatch in internal/orchestrator/toolhandlers.go; `read_skill` excluded from every sub-agent Allowed set (depends on T008)
- [X] T010 [US1] Add sub-agent equipping in internal/orchestrator/subagent.go: `Skills []string` on subagentInput; extend runSubagentDefinition's schema + instructions in internal/orchestrator/definitions.go and internal/instructions; render equipped `<skill name="…">` blocks into the delegated brief with per-dispatch and per-stream dedupe and unknown-name notice lines (contract skills-autouse §4; depends on T008)
- [X] T011 [P] [US1] Unit tests for `read_skill` in new internal/orchestrator/skills_tool_test.go: catalog hit, unknown name, oversized body, manual-marker dedupe, repeat-call dedupe, per-task reset, definition gating (depends on T009)
- [X] T012 [P] [US1] Sub-agent equipping tests in new internal/orchestrator/skills_subagent_test.go: brief contains skill blocks in order, unknown name degrades to a notice, continued stream does not re-send an equipped skill, no sub-agent Allowed set contains read_skill (depends on T010)
- [X] T013 [US1] Byte-stability & prefix gates in new internal/orchestrator/skills_prefix_stability_test.go: SystemPrompt double-construction with an all-roots listing, read_skill definition marshals byte-identically, consecutive-turn toolset/system stability, frozen catalog (contract skills-autouse §6; depends on T009, T010)
- [ ] T014 [US1] Run quickstart §1 scenarios 1–6 plus the two-model style check (SC-001/SC-002/SC-004) against a live session and record observations in specs/013-auto-skills-ui-polish/bench-notes.md (depends on T003–T013) — **DEFERRED: owner cost approval**

**Checkpoint**: US1 fully functional and independently testable — the MVP.

---

## Phase 4: User Story 2 — Token & cache numbers tell the whole truth (Priority: P2)

**Goal**: Every displayed token figure is cache-inclusive full digits; the session cache-hit rate covers main + sub-agents + aux; `/context` becomes a three-group essentials card.

**Independent Test**: quickstart §2 — live figures are comma-grouped full digits matching read+uncached+output; summary equals live; `/context` shows the card with an all-stream hit rate; no-cache providers show "unavailable".

### Implementation for User Story 2

- [X] T015 [P] [US2] Add `FullTokens` (ASCII comma grouping) to internal/contract/format.go with boundary unit tests (0, 999, 1000, 999999, 1000000, 2147483647) in internal/contract/format_test.go (contract display-formats §1)
- [X] T016 [P] [US2] Add `AllStreamHitRate` to `SessionUsageAggregate` in internal/contract/cache.go, computed as `HitRate(PairedCacheRead, PairedCacheMiss)`; tests in internal/contract/cache_test.go: paired-only rule, nil when no paired record, and a mixed-stream case where AllStreamHitRate ≠ SessionHitRate (SC-006 basis; contract display-formats §3)
- [X] T017 [US2] Make the per-task headline cache-inclusive and convert TUI display sites to FullTokens: `headlineTokens` (read+miss+completion, else TotalTokens) and all HumanTokens display calls in internal/tui/format.go, internal/tui/render_header.go, internal/tui/render_tool.go; update pinned goldens (depends on T015; contract display-formats §1–2)
- [X] T018 [US2] Bind every displayed session rate to the all-stream rate: populate TaskStats.SessionHitRate from AllStreamHitRate in internal/orchestrator/turnloop.go and confirm the persistent-footer renderer consumes it; unavailable renders "unavailable" (depends on T016; contract display-formats §3.2/§3.4)
- [X] T019 [P] [US2] Convert engine notices to FullTokens: internal/orchestrator/cacheresilience.go, internal/orchestrator/maintenance.go, internal/orchestrator/advisor.go (depends on T015)
- [X] T020 [US2] Replace `formatContextReport` in internal/tui/format.go with the three-group essentials card (Context / Session / Models, ≤14 content lines, FullTokens, all-stream hit rate, `~` cost marker, omission rules) per contract display-formats §4; delete by-model, per-pairing, categories, invalidations, pressure, API/active time, lines±, steady-state rendering (depends on T015, T016)
- [X] T021 [US2] Rewrite context-card and cross-surface tests: internal/tui/context_panel_test.go + internal/tui/actions_test.go pin the new card; add a cross-surface agreement test (final live headline == summary figure; footer rate == card rate) per contract display-formats §5.3 (depends on T017, T018, T020)
- [~] T022 [US2] Run quickstart §2 scenarios 1–6 — **offline-verified** (full-digit/cache-inclusive goldens, cross-surface agreement, card content + dropped diagnostics, unavailable states, and the SC-006 mixed-stream aggregate case); **live sub-agent session cross-checked against usage.jsonl still pending** (cost-gated)

**Checkpoint**: US1 and US2 independently functional.

---

## Phase 5: User Story 3 — Decluttered commands & clear mode hints (Priority: P3)

**Goal**: `/logout` reads "Log Out of Account"; `/errors` + all harness-friction UI and `/permissions`(+`/mode`) are gone; the permission mode chip is always visible with "Shift + Tab to cycle" beneath it; reasoning levels carry no DeepSeek text.

**Independent Test**: quickstart §3 — palette shows the rename and no removed rows; removed commands yield the standard unknown-command notice; footer shows chip + hint in both modes; Shift+Tab still cycles.

### Implementation for User Story 3

- [X] T023 [P] [US3] Update the commands table in internal/tui/model.go: delete the `/errors` and `/permissions` rows; change the `/logout` description to "Log Out of Account"
- [X] T024 [US3] Clean the slash dispatch in internal/tui/slash.go: remove the `/errors` case, the `/permissions`+`/mode` case and its choice-modal path (keep `setPermission`/`cyclePermission` for Shift+Tab); drop `/errors` from the busy-allowed list; rewrite the reasoning chooser subtitle and the four level descriptions without DeepSeek mappings (FR-014/FR-016/FR-018; depends on T023)
- [X] T025 [US3] Redesign the footer in internal/tui/render_header.go `renderModeLine`: always-rendered permission chip (`normal` muted, `auto-accept` warning) before the effort chip; add the second faint line "Shift + Tab to cycle" right-aligned beneath the chip cluster; hint line degrades/drops first at narrow widths (contract ui-surfaces §2)
- [X] T026 [P] [US3] Remove friction UI from internal/tui/format.go: delete `formatHarnessEvents` and the `⚠ N harness` marker in `taskSummaryLine` (TaskStats.HarnessEvents stays, unrendered); delete internal/tui/harness_events_test.go (research R8)
- [X] T027 [US3] Update US3 test suites: internal/tui/slash_alias_test.go, internal/tui/wiring_inventory_test.go, internal/tui/keys_test.go; new goldens for mode line + hint sub-line in both modes and at narrow width; unknown-command tests for `/errors` `/permissions` `/mode`; palette goldens; reasoning-description assertions (depends on T023–T026)
- [~] T028 [US3] Run quickstart §3 scenarios 1–6 — **offline-verified** (palette/footer render goldens, unknown-command tests, reasoning-description assertions, orphan sweep all green); **interactive terminal walkthrough still pending** (needs a human at a TTY)

**Checkpoint**: US1–US3 independently functional.

---

## Phase 6: User Story 4 — Always-on to-dos & arrow-key agent switching (Priority: P4)

**Goal**: Ctrl+T is gone and the to-do panel is permanently visible while the agent is active with items; ←/→ switch agent views on an empty composer; Tab keeps only command autocomplete.

**Independent Test**: quickstart §4 — checklist stays for the task's whole active life with no toggle; arrows cycle both directions with ≥2 agents and an empty composer; typing restores caret movement; Tab with text does nothing.

### Implementation for User Story 4

- [X] T029 [P] [US4] Add `cycleAgentBack` (exact inverse ring: main → last → … → first → main) next to `cycleAgent` in internal/tui/update.go (contract ui-surfaces §C1.1)
- [X] T030 [US4] Rework keybindings in internal/tui/keys.go: delete the `ctrl+t` case; reduce `tab` to the command-autocomplete branch only; add `left`/`right` cases that call `cycleAgentBack`/`cycleAgent` only when the composer is empty and not in the `/` palette, otherwise fall through to the textarea (Clarification Q1; depends on T029)
- [X] T031 [P] [US4] Make the to-do panel permanent-while-active: remove the `todoVisible` field from internal/tui/model.go and the hidden branch from internal/tui/render_todos.go (visible iff busy && items; retire rules untouched) (FR-025/FR-026)
- [X] T032 [US4] Update the left footer hints in internal/tui/render_header.go to exactly `Esc stop · ←/→ agents · / commands` (coordinate with T025 — same function) (FR-029)
- [X] T033 [US4] Update US4 test suites: rewrite internal/tui/render_todos_test.go (always-visible + retire cases, drop toggle cases); extend internal/tui/keys_test.go (arrows on empty vs non-empty composer, both ring directions, single/no-agent no-op, Tab-with-text no-op, ctrl+t unhandled); hint goldens (depends on T029–T032)
- [~] T034 [US4] Run quickstart §4 scenarios 1–6 — **offline-verified** (todo-visibility goldens, arrow-ring both directions, composer-editing no-regression, Tab/Ctrl+T removal all green); **interactive terminal walkthrough still pending** (needs a human at a TTY)

**Checkpoint**: All four user stories independently functional.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T035 [P] Orphan sweep per contract ui-surfaces §C4.3: case-insensitive repo sweep for `harness friction`, `/errors`, `/permissions`, `/mode`, `Clear API key`, `DeepSeek maps`, `Ctrl+T`, `Tab agents` across internal/ and docs/ — zero hits in user-visible strings (telemetry identifiers exempt); fix stragglers (SC-007)
- [X] T036 [P] Update docs in the same change (constitution workflow gate): README.md, docs/agent-design.md, docs/architecture.md — skills auto-use flow, read_skill/run_subagent.skills, full-digit displays, all-stream session rate, simplified /context, keybinding changes, removed commands
- [X] T037 [P] Security review note in docs/security.md: read_skill reviewed against real-path containment, workspace trust, and secret redaction (no path input, catalog-only, bounded — contract skills-autouse §3.5)
- [X] T038 Full quality gates: `go fmt ./...`, `go vet ./...`, `go test ./... -count=1` green — **race run DEFERRED: this host has no C compiler (cgo required); run on a CGO-capable runner/CI**
- [ ] T039 Constitution X "after" evidence (quickstart §5.2–5.3, same model/gateway/effort/workload as T002): feature-binary gauntlet 3× (SC-011), skills-installed-but-unused run shows zero read_skill calls and an unchanged cache profile (FR-011), session-start timing within +5% (SC-003); record in bench-notes.md (depends on T002, T014, T038) — **DEFERRED: owner cost approval**
- [X] T040 Prefix-stability workflow gate (quickstart §5.4): automated byte-stability suite green across consecutive turns; the one-time upgrade epoch is proven *attributed* not silent by `TestUpgradeEpochIsAttributedNotSilent` (real pre-013 vs post-013 prefix shapes → toolset-change + prompt-rebuild invalidations + a cause naming both regions)
- [~] T041 Final acceptance: quickstart §6 done-bar — fmt/vet/full suite green, docs updated, security note added; requirements checklist **re-validated against delivered code** (16/16, see checklists/requirements.md "Post-Implementation Re-Validation"). Remaining: the race run and the deferred live/interactive scenarios above

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none — start immediately. T002's live runs need owner cost approval; only T039 depends on its output, so implementation never blocks on it.
- **Foundational (Phase 2)**: empty — stories start after Phase 1.
- **User Stories (Phases 3–6)**: mutually independent in behavior; any order or in parallel. Priority order (US1 → US2 → US3 → US4) is the default sequence.
- **Polish (Phase 7)**: after all stories that will ship.

### Cross-story file coordination (only real conflicts)

- `internal/tui/format.go`: T017/T020/T021 (US2) and T026 (US3) — run within one story at a time; any order between stories.
- `internal/tui/render_header.go`: T025 (US3) before T032 (US4) when both stories are in flight; each is self-contained if the other story is skipped.
- `internal/tui/model.go`: T023 (US3) and T031 (US4) touch different fields — trivially sequenced.
- `internal/orchestrator/definitions.go`: T009 before T010 (within US1, already ordered).

### Within-story ordering

- **US1**: T003, T004 [P] → T005 → T006, T007 → T008 → T009, T010 → T011, T012 [P] → T013 → T014
- **US2**: T015, T016 [P] → T017, T018, T019, T020 → T021 → T022
- **US3**: T023, T026 [P] → T024, T025 → T027 → T028
- **US4**: T029, T031 [P] → T030, T032 → T033 → T034

### Parallel Opportunities

```text
Phase 1:  T001 ∥ T002
US1 wave: T003 ∥ T004        then  T011 ∥ T012
US2 wave: T015 ∥ T016        then  T017 ∥ T019 (different files)
US3 wave: T023 ∥ T026
US4 wave: T029 ∥ T031
Polish:   T035 ∥ T036 ∥ T037
Stories US2/US3/US4 can proceed in parallel with US1 given the file-coordination rules above.
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (T001; T002 pending cost approval)
2. Phase 3: US1 (T003–T014)
3. **STOP and VALIDATE**: quickstart §1 independently; the upgrade cache epoch ships here (T013 guards it)
4. Demo: the agent auto-uses a frontend-design skill exactly like the Claude Code behavior the spec targets

### Incremental Delivery

1. US1 → validate → MVP
2. US2 → validate (numbers become trustworthy) → ship
3. US3 → validate (surface declutter) → ship
4. US4 → validate (interaction polish) → ship
5. Phase 7 last: sweeps, docs, gates, and the Constitution X before/after evidence (T039/T040 are release-blocking per the constitution)

### Notes

- Golden/wiring suites are updated inside the task that changes the pinned behavior — the tree stays green after every task.
- Live-session tasks (T002, T014, T022 partially, T039) spend gateway credits: batch them and get owner approval first (established repo practice).
- Commit after each task or logical group; every task lists its exact files.

---

## Format Self-Check

- Total tasks: **41** (T001–T041), sequential IDs, every task has checkbox + ID + description with exact file paths.
- Story labels only inside story phases (T003–T034); Setup/Polish carry none.
- [P] only on tasks with disjoint files and satisfied dependencies.
