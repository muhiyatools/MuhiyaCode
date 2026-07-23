# Phase 0 Research: Automatic Skill Use & Interface Polish

**Feature**: `013-auto-skills-ui-polish` | **Date**: 2026-07-20

All findings below come from direct code inspection of the current branch (`feature/native-agent-v1.1.0` lineage). No NEEDS CLARIFICATION markers existed in the Technical Context; the five product-level ambiguities were resolved in the spec's Clarifications session. This document settles the *engineering* unknowns.

---

## R1. Where skill awareness stands today

**Finding**: Partial machinery exists.

- `workspace.DiscoverWorkspaceSkills` (workspace `.agents/skills` + `.codex/skills`, limit 40, sorted) feeds `PromptContext.Skills` at session build ([runtime_build.go:446](../../internal/command/runtime_build.go)), rendered by `renderSkillsSection` as a `SKILLS` block in the **stable prefix** with name + workspace-relative path + description.
- The prefix hint (`instructions.SkillsHintBody`) already says: *"When a task matches a skill's purpose, read its file with read_file and follow its instructions."*
- `workspace.DiscoverSkills` (all roots: workspace + `MUHIYA_SKILLS_DIR` + `~/.codex/skills`/`CODEX_HOME` + `~/.agents/skills`) powers only the **manual** `/skills` modal; bodies load at submit via `LoadSkillInstructions` (32 KiB cap) and wrap into the prompt as `<skill name="…">body</skill>` sections (`app.AssemblePrompt`).
- Home/env-root skills are **unreachable** by the model: file-tool containment blocks reads outside the workspace (deliberate — see `internal/instructions/prompt.go` comment about `~/.muhiya` being a sensitive root).

**Decision**: Build automatic use on these seams — widen the *catalog* to all roots, keep discovery session-frozen, and add a containment-safe read path (R2).

**Alternatives considered**: Fresh skills subsystem — rejected (Constitution VIII; the seams are healthy).

## R2. Skill read mechanism: `read_skill` tool

**Decision**: Add a `read_skill` tool: input `{name: string}`; the engine resolves the name against the session's frozen skill catalog, loads the body via the existing `LoadSkillInstructions` bound (32 KiB), and returns it as the tool result. Unknown name → instructive error listing nothing (the catalog is already in the prefix). The prefix hint is rewritten to direct skill reads through `read_skill` (uniform for workspace and home skills alike).

**Rationale**:
- Home-root skills become reachable **without** widening file-tool containment (Constitution IX: never weaken the security model). The tool exposes only cataloged `SKILL.md` bodies — no raw path input exists, so it cannot be steered at arbitrary files.
- Selection stays the model's reasoned choice (FR-003, standing model-driven-dispatch directive): the harness supplies catalog + tool; it never injects.
- Bodies arrive as tool results in the conversation tail — append-only, cache-friendly (Constitution IV/V).
- Mid-task adoption (FR-007) is free: the tool is callable on any turn.

**Alternatives considered**:
- *Keep `read_file` for workspace + widen containment to home skill roots* — rejected: weakens the documented security model for marginal uniformity.
- *Harness keyword-matching auto-injection* — rejected: violates FR-003/FR-005 and the standing directive that dispatch decisions belong to the model (feature 013 memory, commit 21c0d3a lineage).
- *Inline all skill bodies into the prefix* — rejected: bloats every request, violates FR-002 and Constitution IV.

## R3. Catalog rendering change

**Decision**: The `SKILLS` prefix section lists `- name: description` only — the per-skill path is dropped from the rendered block (names are the `read_skill` key). `SkillListing` keeps `Path` internally for the manual flow and the loader. Section stays sorted by lowercased name, session-frozen, byte-stable; entries beyond the 40-skill bound are silently not cataloged (existing behavior, now stated in the contract).

**Rationale**: Uniform name-keyed access; removes workspace-relative-path normalization concerns for external roots; fewer bytes in the prefix; deterministic. The hint text change and listing change ride the same one-time upgrade epoch as the toolset change (R12).

**Alternatives considered**: Keep paths and teach dual access (`read_file` for workspace, `read_skill` for home) — rejected: two access paths for one concept invites drift and model confusion.

## R4. Duplicate-load prevention (FR-008)

**Decision**: At task start the engine scans the submitted prompt for the manual-flow wrapper markers (`<skill name="X">`, produced only by `app.AssemblePrompt`) and records those names as already-provided for the task. `read_skill` on such a name returns a one-line notice ("skill X was already provided in this task's prompt — apply it from there") instead of the body.

**Rationale**: Zero API churn — `Engine.Run(ctx, prompt)` signature and the TUI submit path stay untouched; the wrapper format is app-controlled and deterministic. The notice is cheap (tens of bytes) and honest.

**Alternatives considered**: Extending `Engine.Run` with a skills parameter, or TUI→engine side-channel state — rejected: signature churn across app/tui/command layers for information the prompt already carries.

## R5. Sub-agent equipping (FR-010, Clarification Q5)

**Decision**: Add optional `skills []string` to `subagentInput` (`run_subagent` tool schema). When present, the engine resolves each name against the session catalog and appends the rendered `<skill name="…">body</skill>` sections to the delegated task brief (the sub-agent's first user message; on a feature-012 continued stream, the delta brief). Unknown names degrade to a one-line notice inside the brief. Sub-agent system prompts (static per kind) and Allowed toolsets are untouched — `read_skill` is **not** granted to sub-agents.

**Rationale**: Matches the clarification exactly (main agent equips; sub-agents never discover/select). Briefs are per-run dynamic content, so cache identity per agent kind is preserved (the static system message and toolset stay byte-stable — feature 012's session-long sub-agent cache is not fragmented). Reuses the exact wrapper format the model already knows from the manual flow.

**Alternatives considered**:
- *Sub-agents get `read_skill` + catalog* — rejected by clarification Q5; would also add catalog bytes to every sub-agent prefix.
- *Parent hand-pastes skill text into `task`* — works today but unreliable (models truncate); an explicit field makes equipping deliberate and lets the engine enforce bounds/dedupe.

## R6. Session-wide cache-hit rate (FR-021)

**Finding**: `AggregateUsage` already sums **all-stream** paired operands into `PairedCacheRead`/`PairedCacheMiss` (both-operands-reported records only), but `SessionHitRate` is computed from main-stream, non-NA records exclusively ([cache.go:347-352](../../internal/contract/cache.go)) — sub-agent and aux traffic "never enter cache-rate KPIs".

**Decision**: The *displayed* session cache-hit value becomes `HitRate(PairedCacheRead, PairedCacheMiss)` — every request in the session (main + sub-agent + aux) whose provider reported both operands. Exposed as a new aggregate field (`AllStreamHitRate`) computed in `AggregateUsage`; available only when ≥1 paired record exists, otherwise nil → "unavailable". The existing `SessionHitRate`/`SteadyStateHitRate`/`PrefixStabilityRate` fields are **unchanged** (benchmark tooling and cache-guard tests consume them); they simply stop being displayed.

**Rationale**: Honest whole-picture reporting (Constitution VI) with zero disturbance to benchmark KPIs (Constitution X history stays comparable). The paired sums already implement the "no fabricated denominator" rule.

**Alternatives considered**: Redefine `SessionHitRate` itself — rejected: silently changes the meaning of every persisted benchmark record and breaks `cachehit_guard`/aggregate tests' semantics.

## R7. Cache-inclusive full-digit token displays (FR-019/FR-020)

**Finding**: 19 display call sites use `contract.HumanTokens` (abbreviating ≥1k). The live headline (`headlineTokens`) deliberately shows **miss+completion** (cache-excluded) per feature 008 UD-1.

**Decision**:
- New `contract.FullTokens(int) string` — comma-grouped full digits (hand-rolled; no new dependency, no locale variance: ASCII commas, Western digits, matching existing UI conventions).
- All TUI/notice display sites switch to `FullTokens`. `HumanTokens` remains for any non-display consumers and goldens that pin it until their sites convert (retire fully if none remain).
- `headlineTokens` changes to the cache-inclusive per-task total: `read + miss + completion` when cache metrics exist, `TotalTokens` otherwise. The cache-% tag is unchanged. The task summary consumes the same helper, so live line and summary stay agreed (FR-024).
- Model-profile capacity lines (context window, max output) also render full digits for consistency ("all displayed token numbers").

**Rationale**: Literal reading of the requirement, confirmed by clarification Q3. Layout risk is bounded: the widest realistic figure (`99,999,999`) fits every surface; the two dense tables that would have suffered (by-model, categories) are removed from `/context` by FR-023 anyway.

**Alternatives considered**: `golang.org/x/text/message` localized formatting — rejected: pulls printer machinery for one format, and locale-varying digits would break golden tests and Arabic-session determinism.

## R8. Harness-friction disposition (FR-013, deferred design decision)

**Finding**: `recordHarnessEvent` is load-bearing engine telemetry — gates, tool failures, provider retries, breakers, and sub-agent budgets all record through it, and engine tests assert on `HarnessEvents()` (telemetry_emit_test, subagent_wrapup_test, gauntlet_offline_test).

**Decision**: Keep the recorder, the `HarnessEvent` contract types, `Engine.HarnessEvents()`, and every engine test. Remove **only** the UI: the `/errors` palette row and slash case, `formatHarnessEvents` + its test, the `⚠ N harness` marker in `taskSummaryLine`, and the "harness friction" wording anywhere user-visible. `TaskStats.HarnessEvents` (count) stays in the contract for bench/telemetry consumers; the TUI stops rendering it.

**Rationale**: The spec governs user-visible surfaces; the engine's self-observability serves benchmarking and regression tests (Constitution VI/X). Deleting it would rewrite healthy machinery (Constitution VIII).

## R9. `/permissions` removal & footer redesign (FR-014/FR-015)

**Finding**: Two permission modes exist (`normal`, `auto-accept`). `shift+tab` → `cyclePermission()` flips them. `/permissions` (alias `/mode`) opens a choice modal; the footer badge renders only for auto-accept. The mode line currently carries hints `Esc stop · Tab agents · Ctrl+T to-dos · / commands` on the left and `[skills] [auto-accept] [effort]` on the right.

**Decision**:
- Remove the `/permissions`+`/mode` palette row, slash case, and the permission choice modal path; `setPermission` stays (cycle needs it).
- The right cluster always renders the mode chip: `normal` in muted style, `auto-accept` in warning style, positioned before the effort (thinking-level) chip.
- The footer gains a second line rendering `Shift + Tab to cycle` in faint style, right-aligned directly beneath the mode chip cluster. Narrow terminals: the hint line truncates/drops before the mode line does (same fitLine discipline as other footer content).
- Left hints become `Esc stop · ←/→ agents · / commands` (Ctrl+T and Tab-agents hints deleted, FR-029).

**Rationale**: Matches clarifications Q1/Q4 and the spec's discoverability requirement; smallest layout change that puts the hint literally "under" the mode text.

**Alternatives considered**: Cramming the hint inline on the same line — rejected: not "under", and steals width from hints at common terminal sizes.

## R10. To-do panel permanence (FR-025/FR-026)

**Finding**: `renderTodos` shows the checklist while busy unless `todoVisible` was toggled off via `ctrl+t`; retire rules already handle empty/complete lists.

**Decision**: Delete the `ctrl+t` key case, the `todoVisible` field, and the hidden-branch in `renderTodos`; the panel renders whenever the agent is busy and items exist. Retire behavior untouched. Goldens for the toggle (render_todos_test T033 cases) are replaced by always-visible assertions.

## R11. ←/→ agent switching (FR-027/FR-028, Clarification Q1)

**Finding**: `tab` currently completes commands (input starts with `/`) or calls forward-only `cycleAgent()` (main → agents… → main). `left`/`right` are not intercepted — they fall through to the composer.

**Decision**: In `handleKey`, add `left`/`right` cases guarded by `m.input.Value() == ""` (and not in command-palette mode): `right` = existing forward cycle, `left` = new reverse cycle (main → last agent → … → first → main). Non-empty composer falls through to the textarea exactly as today. `tab` keeps only command completion; its agent branch is deleted. Alt+1..9 direct jumps stay.

**Rationale**: Clarification Q1's empty-composer rule; mirrors the up/down empty-input precedent already in `handleKey`.

## R12. Cache-epoch accounting for the prefix/toolset changes

**Decision**: All session-stable byte changes ship together in one version: `read_skill` tool definition, `run_subagent.skills` schema addition, all-roots catalog listing, path-free listing format, rewritten skills hint. First run of the new binary triggers exactly one attributed cold start (prefix-shape sidecar: `ToolsHash`/`SystemHash` change → attribution instead of silent cold-start). Within-session determinism is preserved: catalog frozen at session start, schemas static.

**Rationale**: Constitution III tolerates upgrade breaks when attributed and one-time; precedent is the feature-g4 `ask_user` epoch (commit 488ea14). Bundling avoids two epochs across two releases.

**Verification hooks**: extend the byte-stability tests (SystemPrompt double-construction equality with an all-roots listing; toolset serialization equality across turns) and run the automated prefix-stability check required by the constitution's workflow gates.

## R13. Reference architecture: DeepSeek Reasonix (Constitution VII)

**Observations applied to this design** (from the feature-001/002 Reasonix studies recorded in this repo's cache lineage, re-examined for skills):

- Reasonix keeps its capability surface as a **short, stable pointer catalog** early in the prefix and pulls full capability text lazily into the tail — never rewriting the prefix per task. The `SKILLS` section + `read_skill` design mirrors this exactly: prefix carries name+description pointers; bodies enter as tail tool-results only when chosen.
- Reasonix's implicit prefix cache rewards append-only tails; skill bodies as tool results (not prompt rewrites) ride that property (Constitution IV/V).
- Reasonix treats per-run worker briefs as disposable dynamic payloads while worker *identities* stay static — the same split as static sub-agent system prompts + per-run skill-equipped briefs (R5).

**Deviations**: none required; MuhiyaCode's multi-provider constraint (generic OpenAI-compatible endpoints) is already honored because the design uses only standard tool-calling — no provider-specific cache API.

## R14. Reasoning-level text & logout rename (FR-012/FR-016)

**Finding**: DeepSeek-mapping text lives in `slash.go` effort descriptions (lines 188–194) and the `/reasoning` chooser subtitle (line 99). `/logout` description "Clear API key" lives in the `commands` table (model.go:352).

**Decision**: Per-level descriptions become mapping-free ("Lightest thinking, fastest — the default." / "Balanced thinking." / "Deep thinking for tricky work." / "Maximum thinking for the hardest problems."); the chooser subtitle becomes "How hard the model thinks." `/logout` description becomes "Log Out of Account". Any golden pinning old strings updates in the same change (FR-017 sweep: `grep -ri "harness friction\|/errors\|/permissions\|Clear API key\|DeepSeek maps\|Ctrl+T\|Tab agents"` over `internal/` must return only engine-internal telemetry identifiers, never UI strings).

## R15. Simplified `/context` card data (FR-023, Clarification Q2)

**Finding**: `formatContextReport` renders ~9 sections from a rich `ContextReport`.

**Decision**: `formatContextReport` is replaced by a compact card with three groups (see [contracts/display-formats.md](contracts/display-formats.md) §4): **Context** (in use / free of limit, full digits), **Session** (prompt/output totals cache-inclusive, cache read/uncached split, all-stream hit rate, cost in credits with the existing `~` estimated marker), **Models** (main + sub-agent model names from the session runtime). `ContextReport` (engine side) keeps its fields — only the TUI consumes a subset now; dropped fields stay for bench tooling. Tests for dropped sections are removed/replaced.

**Rationale**: Clarification Q2 ("essentials only"); Constitution VIII (report struct untouched — display-only shrink).
