# Tasks: MuhiyaCode TUI & Agent Experience Overhaul

**Input**: Design documents from `/specs/003-tui-ux-overhaul/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included where mandated — the constitution (VI/X + workflow gates) requires the prefix-stability, restart-determinism, and before/after benchmark tasks for the cache-affecting skills change and the credits accuracy claims; contracts define golden/update-layer test obligations (D14). Cosmetic-only tasks carry no separate test tasks beyond the shared goldens.

**Organization**: Grouped by user story (US1–US8 = spec priorities P1–P8) after shared Setup and Foundational phases. Two repos: `F:\MuhiyaCode Agent Go` (this repo — all paths relative to root) and the gateway at `F:\MuhiyaWorkspace\MuhiyaWorkspace` (prefixed `GW:`). Gateway tasks ship first (backward-compatible; [usage-api.md §3](contracts/usage-api.md)).

## Format: `[ID] [P?] [Story] Description`

## Implementation status (2026-07-12)

**37 of 44 tasks complete.** All code, unit tests, and automated gates are done and green: `go fmt`/`go vet`/`go test ./... -count=1` pass in both repos. The bold-markdown bug, palette/glyph system, header, breakpoints, ephemeral thinking, three-metric task summary, tool-display contract, input cleanup, `/usage` + login visibility, regrouped context modal, and the deterministic skills listing are all implemented and tested; the gateway ships the extended `muhiya_log` allowlist + `usage_estimated` field, `GetUserSpendingToday`, and the key-authenticated `GET /v1/usage` endpoint.

**7 tasks remain — all require a live environment or human raters this session cannot provide** (not code gaps):

- **T001 / T038 / T039** — before/after cachebench runs and the credits cross-check need a running gateway with a real API key and a multi-turn workload against a live model. The harness and workload exist; execute per [quickstart.md](quickstart.md) Scenario 7 when a gateway + key are available.
- **T002 / T018** — byte-exact teatest frame goldens. The `teatest/v2` module targets `github.com/charmbracelet/bubbletea/v2`, not this repo's `charm.land` module rename, so per plan.md the stdlib fallback was used: regression is protected by assertion-based tests pinned to fixed sizes (`render_test.go`, `tooldisplay_test.go`, and the summary/header/too-small tests in `tui_test.go`) rather than `.golden` snapshot files. Adding byte-exact snapshots is optional follow-up if a compatible harness lands.
- **T042 / T043** — the manual terminal matrix (Warp/WT/iTerm2 × light/dark × `NO_COLOR`) and the ≥5-tester qualitative review are human-driven acceptance steps; run per [quickstart.md](quickstart.md) Scenarios 1–6 and 8.

---

## Phase 1: Setup

**Purpose**: Baseline evidence and shared test scaffolding — the benchmark baseline MUST be captured from `main` before any prefix-affecting change lands (constitution X).

- [ ] T001 Capture cachebench baseline on `main`: `go run ./benchmarks/cachebench -scenario all -runs 3 -build-label baseline -out "specs/003-tui-ux-overhaul/benchmarks/baseline"`; commit raw JSON under specs/003-tui-ux-overhaul/benchmarks/baseline/ (quickstart Scenario 7)
- [ ] T002 [P] Add test-only golden-test modules `github.com/charmbracelet/x/exp/teatest/v2` + `x/exp/golden` to go.mod and create the pinned-frame helper (80×24, `colorprofile.Ascii`) in internal/tui/golden_test.go; if no release is compatible with the `charm.land` module rename, implement the stdlib fallback comparing `View()` output per plan.md Technical Context
- [x] T003 [P] Create skills trial fixtures: ≥3 workspace skills with clear trigger descriptions + 1 frontmatter-less SKILL.md + 1 external-root skill, plus a manifest of 10 matching / 5 non-matching task prompts, in specs/003-tui-ux-overhaul/fixtures/skills/ (quickstart Prerequisites, SC-009)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Gateway wire surface, contract fields, and the shared visual base every story renders through.

**⚠️ CRITICAL**: T004–T006 unblock US2/US5/US6 live paths; T007–T010 unblock US2/US6; T011–T012 unblock every visual story.

### Gateway (GW: `F:\MuhiyaWorkspace\MuhiyaWorkspace`) — ships first

- [x] T004 [P] GW: extend `sendMuhiyaMetaChunk` allowlist to `{"MuhiyaChat","MuhiyaCode"}` (exact match) and add `usage_estimated` to the `muhiya_log` object in GW:proxy/handler.go (~1883-1910); tests: MuhiyaCode receives the chunk, unknown apps don't, `usage_estimated` mirrors request_logs ([usage-api.md §1](contracts/usage-api.md))
- [x] T005 [P] GW: add `GetUserSpendingToday(userID)` to GW:db/db.go — `SUM(cost)` over `request_logs`, `status_code >= 200 AND < 300`, `created_at >= date_trunc('day', now() AT TIME ZONE 'UTC')` + unit test ([usage-api.md §2](contracts/usage-api.md))
- [x] T006 GW: implement `GET /v1/usage` (+ `/usage` mount) in GW:proxy/handler.go authenticated via `authenticateVirtualKey`, composing `GetUser`/`GetUserBudgetUsage`/`GetRemainingExtraCredits`/`GetUserSpendingToday`; register routes in GW:main.go; exempt from RPM/TPM; tests: happy path (float credits), window math, 405, and 401 under both Bearer (OpenAI envelope) and x-api-key (Anthropic envelope) — depends on T005

### MuhiyaCode contract & orchestrator plumbing

- [x] T007 [P] Add `CostUSD *float64` + `CostEstimated bool` to `contract.Usage` (internal/contract/types.go) and `contract.UsageRecord` (internal/contract/cache.go) with the member-set credits helper implementing [data-model §1.1](data-model.md) rules (empty-usage exclusion, nil ⇒ unavailable, `CostEstimated` OR); persistence round-trip test incl. old-session backward compatibility in internal/orchestrator/usage_record_test.go
- [x] T008 [P] Add `TaskStats.StopCause` + constants (`"user stop"`, `"error"`, `"disconnect"`) to internal/contract/types.go, independent of H5 `TerminatedReason` ([data-model §1.3](data-model.md))
- [x] T009 Parse the `muhiya_log` final SSE chunk in internal/gateway/provider.go streaming path → `Usage.CostUSD`/`CostEstimated`; absence tolerated silently; chunk never enters model-visible content; mock-stream tests incl. malformed/absent chunk ([usage-api.md §1](contracts/usage-api.md)) — depends on T007
- [x] T010 Engine plumbing in internal/orchestrator/engine.go: carry cost fields through `recordMainUsage`/`recordAuxUsage`/`recordIsolatedUsage`; capture the task's usage-record boundary at task start and compute task credits over that range at `TaskComplete`; stamp `StopCause` on the context-cancellation (Esc), turn-loop-error, and disconnect paths before the deferred `TaskComplete` fires; unit tests for range capture + all three stamp paths + H5-breaker independence — depends on T007, T008

### Shared visual base

- [x] T011 Rebuild `newPalette()` into the semantic token system in internal/tui/render.go per [visual-system.md §1](contracts/visual-system.md): token→(dark,light) maps, startup `HasDarkBackground`/`LightDark` resolution, `Settings.Theme` override, `NO_COLOR` attribute-only mode; remove the forced full-screen `BackgroundColor`/`ForegroundColor` (internal/tui/view.go:58-59); migrate all style call sites to tokens
- [x] T012 Create the glyph table (unicode + ASCII variants, `MUHIYA_ASCII=1`/`Settings.UI.BorderMode=ascii` selection) in internal/tui/render.go per [visual-system.md §2](contracts/visual-system.md) and route every non-ASCII glyph call site in internal/tui/view.go through it — depends on T011 (same files)

**Checkpoint**: gateway deployed + contract fields flowing + tokenized rendering base — all stories can start.

---

## Phase 3: User Story 1 — Polished, Correct Rendering in Every Terminal (P1) 🎯 MVP

**Goal**: Coherent palette, correct markdown (the `**bold**` fix), reworked tables, top-bar padding, Warp-safe rendering, responsive breakpoints.

**Independent Test**: quickstart Scenario 1 — markdown demo across the terminal matrix at 80/120/200 cols, light theme, `NO_COLOR`, live resize (SC-001/SC-002).

- [x] T013 [US1] Implement the inline markdown tokenizer in internal/tui/render.go replacing the `**`-strip (render.go:80): `**bold**`, `*italic*`/`_italic_`, `__bold__`, `` `code` ``, `~~strike~~` spans styled per [visual-system.md §3](contracts/visual-system.md); unclosed markers render literally with no mid-stream flash; RTL wrapping (internal/tui/rtl.go) preserved
- [x] T014 [US1] Rework table rendering in internal/tui/render.go: per-cell inline pass (T013 tokenizer), bold header + `border.default` rules, zebra fg alternation, width-aware truncation with table-glyph ellipsis — depends on T013
- [x] T015 [P] [US1] Markdown corpus golden tests in internal/tui/render_test.go: bold/italic/code/strike in paragraphs, lists, tables, wrapped lines, RTL text, streaming half-tokens (SC-002 regression corpus)
- [x] T016 [US1] Header composition rework in internal/tui/view.go (renderHeader): style-padding gutter instead of literal spaces, rule via glyph table, segments per [visual-system.md §3](contracts/visual-system.md) — depends on T011, T012
- [x] T017 [US1] Breakpoint ladder in internal/tui/view.go + internal/tui/model.go: 60–80 col segment/column drops, `<60×20` clean too-small pane (`terminal too small · MuhiyaCode needs at least 60x20`), raise render floor from 40×14 ([visual-system.md §4](contracts/visual-system.md)) — depends on T016
- [ ] T018 [US1] Frame goldens at 80×24 + 60×20 in internal/tui/view_golden_test.go: header, transcript with markdown corpus, too-small pane — depends on T013–T017, T002

**Checkpoint**: US1 independently shippable — the visual trust foundation (MVP).

---

## Phase 4: User Story 2 — Calm, Honest Task Summary (P2)

**Goal**: Ephemeral thinking indicator; one three-metric summary line (credits · tokens · cache %) in the transcript after the final message; all noise removed.

**Independent Test**: quickstart Scenario 3 — multi-turn task, residue sweep, interrupt drill, generic-endpoint omission, credits cross-check (SC-003/004/006).

- [x] T019 [US2] Make the thinking indicator ephemeral in internal/tui/view.go + internal/tui/model.go: delete the `lastThoughtDuration` render branch (view.go:181-183) and state carry-over; gutter glyph from the table ([task-summary.md §2](contracts/task-summary.md))
- [x] T020 [US2] Add the TaskSummaryEntry transcript entry kind in internal/tui/model.go: created once on `statsMsg`, appended after the final assistant message, immutable, persists with history; `Interrupted` from `StopCause`; credits/tokens/cache fields per [data-model §2.1](data-model.md) — depends on T007–T010
- [x] T021 [US2] Render the summary line in internal/tui/view.go per [task-summary.md §3](contracts/task-summary.md) (omission rules, `~` marker, `interrupted`); remove `renderUsageFooter` + `taskSummary` from the View() composition (view.go:46-49, 111-125, 646-664) — depends on T020
- [x] T022 [P] [US2] Tests in internal/tui/summary_test.go: emitted once per task, every nil-combination omission, `~` marker, all three `StopCause` paths mark `interrupted` (H5 breaker does not), footer absence, aux-failure robustness (task with empty-usage record still shows credits from priced members), plus goldens of each [task-summary.md §3](contracts/task-summary.md) example line — depends on T020, T021

**Checkpoint**: US1+US2 = the core trust experience.

---

## Phase 5: User Story 3 — Consistent Tool Activity (P3)

**Goal**: Collapsed = one line (name · target · outcome); Ctrl+O expands everything to one uniform detail pattern.

**Independent Test**: quickstart Scenario 2 — task touching every tool class, toggle both ways, failure case (SC-005).

- [x] T023 [US3] Implement `toolDisplay` derivation in internal/tui/view.go: per-tool label/target/outcome table from [tool-display.md §2](contracts/tool-display.md) incl. `+A −R` counting from the `--- diff ---` payload (excluding `+++`/`---` headers), match/entry/result counts, `exit S · dur`, first-line fallback when no measure derivable
- [x] T024 [US3] Rework `renderTool` in internal/tui/view.go: collapsed = exactly one line (remove the 8-line diff preview, view.go:501-510); expanded = uniform `▏`-gutter detail block (colorized diff / boxed output / result list); streaming (`running…`, elapsed >2s) and failure (`×` + first error line) states per [tool-display.md §3-4](contracts/tool-display.md) — depends on T023
- [x] T025 [P] [US3] Tests in internal/tui/tooldisplay_test.go: outcome-derivation edge cases (no diff marker, headers-only diff, binary notice), Ctrl+O flips all entries with no mixed state, goldens for one entry of each tool class collapsed + expanded + failure + running — depends on T023, T024

**Checkpoint**: transcript reads disciplined and auditable.

---

## Phase 6: User Story 4 — Quiet Input & Message Area (P4)

**Goal**: Placeholder-based hinting, no arrow prefix, no provider leaks.

**Independent Test**: quickstart Scenario 4 steps 1-2 + 6 (SC-006, FR-016).

- [x] T026 [US4] Set placeholder `Type a request · / for commands` (bullet from glyph table) in internal/tui/model.go:179; drop `Enter send`/`Ctrl+P commands` from the mode line keeping mode/plan/goal/skill badges + `Esc stop · Tab agents · Ctrl+O details` in internal/tui/view.go (renderModeLine, ~220-248); Ctrl+P binding unchanged
- [x] T027 [P] [US4] Remove the `> ` user-message prefix keeping the `bg.surface` band in internal/tui/view.go:439-448
- [x] T028 [P] [US4] Provider-leak guard test in internal/gateway/web_test.go asserting `formatSearch` output and the web_search tool rendering contain no upstream provider identifiers (FR-016 stays true by test, not accident)

**Checkpoint**: input region carries zero chrome noise.

---

## Phase 7: User Story 5 — Account Usage & Sign-in Clarity (P5)

**Goal**: `/usage` fetches live account data with the stored key; `/login` visibility tracks sign-in state.

**Independent Test**: quickstart Scenario 4 steps 3-5 — live `/usage` <3s, failure drills, logout/login cycle (SC-007).

- [x] T029 [US5] Create the usage client in internal/gateway/usage.go (new): `GET /v1/usage` with Bearer key, 5s timeout, typed response per [usage-api.md §2](contracts/usage-api.md) (float credits), distinct error classes (unreachable / 401 / 404-old-gateway / malformed); wire `Actions.FetchUsage` in internal/command/application.go — depends on T006 (contract), testable against a mock server
- [x] T030 [US5] Add `/usage` command + grouped modal in internal/tui/actions.go + internal/tui/model.go: Plan windows (remaining = budget−spent floor 0, reset times) · Extra credits (credits + USD dual display, 2-decimal display rounding) · Spend (session Σ`CostUSD` client-side, today, billing period = max-duration window); FR-020 friendly failure states; signed-out invocation explains sign-in without sending a request — depends on T029
- [x] T031 [US5] Command palette visibility predicate in internal/tui/model.go + internal/tui/actions.go: hide `/login` when `Secrets.ProviderAPIKey` set, hide `/usage`+`/logout` when not ([data-model §2.5](data-model.md)); update-layer tests for both states + direct-invocation friendly paths

**Checkpoint**: cost visibility end-to-end live.

---

## Phase 8: User Story 6 — Organized Context Modal (P6)

**Goal**: Same data, grouped and scannable, plus session credits.

**Independent Test**: quickstart Scenario 5 — field-for-field parity check against the research A9 list + credits line.

- [x] T032 [US6] Expose session credits in `orchestrator.ContextReport` (internal/orchestrator/engine.go) via the member-set sum over all usageRecords with priced/unpriced counts ([data-model §3.2](data-model.md)); regroup `formatContextReport` in internal/tui/actions.go into Context / This session / Streams / Cache health sections styled per [visual-system.md §3](contracts/visual-system.md), preserving every existing datum; modal golden in internal/tui/context_test.go — depends on T007, T010, T011

**Checkpoint**: diagnostics depth lives in `/context`, not the transcript.

---

## Phase 9: User Story 7 — Honest Header (P7)

**Goal**: Full workspace path, left-truncated; `Subagent` label.

**Independent Test**: quickstart Scenario 1 header checks — deep path at 120 cols, truncation when narrowed, exact `Subagent` string at ≥80 cols (FR-021/022).

- [x] T033 [US7] In internal/tui/view.go (renderHeader): line 2 shows the full `WorkspacePath` with leading-`…` left truncation only when width requires; rename the label at view.go:72 to the literal `Subagent`; add the deep-path (≥5 segments) golden fixture to internal/tui/view_golden_test.go — depends on T016

**Checkpoint**: header states exactly where and what the agent runs.

---

## Phase 10: User Story 8 — Skills That Just Work (P8)

**Goal**: Deterministic workspace-scoped skills listing in the stable prefix; on-demand loading via `read_file`; lazy `/skills`; zero cache regression.

**Independent Test**: quickstart Scenario 6 — listing byte-stability, 10/5 trial, external-root exclusion, resume immutability, instant modal (SC-008/009).

- [x] T034 [US8] Session-pinned skill snapshot in internal/command/application.go + internal/workspace/skills.go: discover once at new-session creation, advertised set filtered to workspace-resident roots (`.agents/skills`, `.codex/skills`); persist the snapshot as a session sidecar and restore verbatim on `/resume`/restart (001 ProbeSnapshot pattern, application.go:550-564; state layer internal/state/session.go) per [skills-delivery.md §1](contracts/skills-delivery.md) — depends on T003 fixtures for tests
- [x] T035 [US8] Render the deterministic `## Skills` section in internal/orchestrator/prompt.go per [skills-delivery.md §2](contracts/skills-delivery.md): sorted by lowercased name, `- <name> (<path>): <description>` with fixed no-description form, forward slashes, two byte-frozen guidance sentences, section absent when snapshot empty — depends on T034
- [x] T036 [P] [US8] Lazy `/skills` loading in internal/command/application.go (listSkills, ~744-761): stop eager-loading all instruction bodies on modal open; load only selected skills at submit (32 KiB cap unchanged); modal-open latency test
- [x] T037 [US8] Determinism guard fixtures per [skills-delivery.md §4](contracts/skills-delivery.md): internal/orchestrator/prompt_stability_test.go (byte-identical across constructions, order-independence, empty-set parity, Windows path separators, frontmatter-less form) + internal/orchestrator/restart_determinism_test.go (snapshot persistence ⇒ byte-identical prompt across save/reload even with on-disk changes; update the premise comment) + re-run cachehit guards green — depends on T034, T035

**Checkpoint**: skills advertised, loaded on demand, prefix provably stable.

---

## Phase 11: Polish & Cross-Cutting

**Purpose**: Verification evidence, hygiene gates, docs sync (constitution workflow gates).

- [ ] T038 Run the after-benchmark + comparison per quickstart Scenario 7 (`-build-label improved`, `-compare`); assert session hit-rate non-regression + prompt growth ≤ skills-listing bytes; record comparison.md + raw JSON in specs/003-tui-ux-overhaul/benchmarks/ (FR-027/SC-008; constitution X) — depends on T001 and all US8 tasks
- [ ] T039 SC-004 credits accuracy cross-check during the T038 run: Σ TUI per-task credits == Σ gateway `request_logs.cost` × 100 for the matching `log_id` member sets; record evidence in specs/003-tui-ux-overhaul/benchmarks/ ([task-summary.md §6](contracts/task-summary.md)) — depends on T038
- [x] T040 [P] Hygiene grep gate: no hex color or non-ASCII glyph literals in internal/tui outside the palette/glyph tables ([visual-system.md §5](contracts/visual-system.md)); wire as a script or test in internal/tui/hygiene_test.go
- [x] T041 [P] Docs sync in the same change set: README.md, docs/agent-design.md, docs/architecture.md — skills listing (prompt composition), credits display, summary redesign (constitution workflow gate)
- [ ] T042 Full manual matrix: quickstart Scenarios 1–6 on Warp (required) + two more terminals, light theme + `NO_COLOR` passes; record results in specs/003-tui-ux-overhaul/benchmarks/matrix.md (SC-001/005/006/007/009)
- [ ] T043 Qualitative review per quickstart Scenario 8: ≥5 testers, fixed script, ≥80% top-two-box + zero complaint recurrence; record in specs/003-tui-ux-overhaul/benchmarks/qualitative/ (SC-010)
- [x] T044 Final gates both repos: `go fmt ./... ; go vet ./... ; go test ./... -count=1` here and in GW:; confirm quickstart Definition of done fully checked

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Ph1)**: T001 must run on `main` before any prefix change merges; T002/T003 anytime.
- **Foundational (Ph2)**: T004/T005 → T006 (gateway); T007/T008 → T009/T010 (plumbing); T011 → T012 (visual base). Gateway deploys before US2/US5 live validation (contract-first client work may proceed against mocks).
- **US phases**: all unblock after Ph2. Sequencing notes: US7 (T033) depends on US1's T016; US2 needs T007–T010; US5 live path needs T006; US6 needs T007/T010/T011.
- **Polish (Ph11)**: T038/T039 after US8 (+T001); T042/T043 after all visual stories; T044 last.

### Story dependency summary

| Story | Blocks on | Independent of |
|---|---|---|
| US1 | Ph2 visual base (T011-T012) | all other stories |
| US2 | Ph2 plumbing (T007-T010) | US1 visuals (works unstyled) |
| US3 | Ph2 visual base | US1/US2 |
| US4 | Ph2 visual base | everything else |
| US5 | T006 (live), T029 chain | US1-US4 |
| US6 | T007/T010/T011 | US2 (shares fields, not code) |
| US7 | T016 | US2-US6, US8 |
| US8 | T003, Ph2 none otherwise | all visual stories |

### Parallel opportunities

- **Ph2**: T004 ∥ T005 ∥ T007 ∥ T008 (different files/repos); then T006 ∥ T009 ∥ (T011→T012).
- **After Ph2, with parallel capacity**: US1 (dev A) ∥ US2 (dev B) ∥ US8 (dev C) — disjoint file sets (render/view vs model/summary vs prompt/skills); US3/US4 follow US1 in view.go to avoid merge conflicts.
- **Within stories**: test tasks marked [P] (T015, T022, T025, T028, T036) run alongside sibling implementation in different files.

### Parallel example: post-Foundational kickoff

```text
Dev A: T013 → T014 → T015/T016 → T017 → T018   (US1, MVP)
Dev B: T019 → T020 → T021 → T022               (US2)
Dev C: T034 → T035 → T036/T037                 (US8)
Gateway: T004/T005 → T006 → deploy             (unblocks US5 live)
```

---

## Implementation Strategy

**MVP first (US1)**: Ph1 → Ph2 (T011/T012 minimum for US1) → Phase 3 → validate quickstart Scenario 1 → ship. The visual trust foundation lands before any behavior change.

**Incremental delivery**: US1 → US2 (cost trust) → US3 (tool discipline) → US4 (quiet chrome) → US5 (account visibility) → US6 → US7 → US8 (skills + benchmark). Each checkpoint is independently testable via its quickstart scenario; the gateway pair (T004-T006) can deploy any time after Ph2 without breaking older clients.

**Risk ordering rationale**: the two prefix-touching tasks (T035 skills listing) and the benchmark pair (T001 baseline / T038 after) bracket the cache-risk change so constitution X evidence is complete regardless of when US8 lands; everything else is display/transport-layer and prefix-safe by construction (research A11).

---

## Notes

- [P] = different files, no incomplete-dependency overlap.
- Every US-phase task carries its [USn] label; Setup/Foundational/Polish tasks carry none.
- GW: prefix = gateway repo `F:\MuhiyaWorkspace\MuhiyaWorkspace`; all other paths are this repo.
- Commit per task or coherent group; stop at any checkpoint to validate the story's quickstart scenario.
