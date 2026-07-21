# Data Model: Automatic Skill Use & Interface Polish

**Feature**: `013-auto-skills-ui-polish` | **Date**: 2026-07-20 | **Sources**: [spec.md](spec.md), [research.md](research.md)

No persisted formats change (`~/.muhiya` layout, `usage.jsonl`, session files, `prefix_shape.json` schema all untouched). Every change below is an in-memory/contract-level extension.

## 1. Skill & Catalog

### 1.1 `workspace.Skill` (existing — unchanged)

| Field | Type | Notes |
|-------|------|-------|
| Name | string | From SKILL.md frontmatter `name:`, else parent-dir name; dedupe key (lowercased) |
| Description | string | Frontmatter `description:`; single-lined + 200-rune-truncated at listing time |
| Path | string | Absolute path to SKILL.md |

Validation (existing, now contractual): unreadable/malformed files → skipped (FR-009); per-body read bound 32 KiB (`LoadSkillInstructions`); discovery bound 40 skills; deterministic order = stable sort by lowercased name; first-root-wins dedupe across roots (workspace → `MUHIYA_SKILLS_DIR` → `CODEX_HOME|~/.codex/skills` → `~/.agents/skills`).

### 1.2 `orchestrator.SkillListing` (extended)

| Field | Type | Change | Notes |
|-------|------|--------|-------|
| Name | string | unchanged | The `read_skill` key; rendered in the SKILLS section |
| Description | string | unchanged | Rendered after the name |
| Path | string | **no longer rendered** | Kept for engine-side resolution; absolute (was workspace-relative render-only) |

Rendered section format (byte-fixed per identical input): see [contracts/skills-autouse.md](contracts/skills-autouse.md) §2.

### 1.3 Session Skill Catalog (new, engine-side)

Frozen at session start (`EngineConfig`), never mutated mid-session (edge case: mid-session installs take effect next session).

| Field | Type | Notes |
|-------|------|-------|
| entries | ordered list of SkillListing | Same slice that rendered the prefix section — single source of truth |
| byName | map lowercased name → index | Resolution for `read_skill` and `run_subagent.skills` |

State per task (reset at task start):
| Field | Type | Notes |
|-------|------|-------|
| providedSkills | set of lowercased names | Seeded by scanning the submitted prompt for `<skill name="X">` markers (manual flow); a successful `read_skill` adds its name — a repeat call returns the pointer notice (FR-008, Constitution V) |

## 2. Tool Contracts (schema-level entities)

### 2.1 `read_skill` (new)

| Direction | Field | Type | Rules |
|-----------|-------|------|-------|
| input | name | string, required | Case-insensitive match against catalog |
| output | — | string | Skill body (≤32 KiB, trimmed); or already-provided notice; or unknown-name error naming no paths |

### 2.2 `run_subagent` input (extended)

| Field | Type | Change |
|-------|------|--------|
| agent | string | unchanged |
| role | string | unchanged |
| title | string | unchanged |
| task | string | unchanged |
| skills | []string, optional | **new** — names resolved from the session catalog; rendered as `<skill name="…">body</skill>` sections appended to the delegated brief; unknown names degrade to a one-line notice; sub-agent toolsets do NOT gain `read_skill` |

## 3. Usage & Display Aggregates

### 3.1 `contract.SessionUsageAggregate` (extended)

| Field | Type | Change | Definition |
|-------|------|--------|------------|
| AllStreamHitRate | *float64 | **new** | `HitRate(PairedCacheRead, PairedCacheMiss)` — every session request (main + subagent + aux) with both cache operands provider-reported; nil when no paired record exists ("unavailable", FR-022) |
| SessionHitRate, SteadyStateHitRate, PrefixStabilityRate | *float64 | unchanged | Benchmark KPIs; no longer displayed |

Display rule (FR-021): every user-visible "session cache-hit" figure binds to `AllStreamHitRate`.

### 3.2 Token display formatting (new helper)

`contract.FullTokens(value int) string` — full digits, ASCII comma thousands grouping, no sign handling needed (counts are non-negative). Replaces `HumanTokens` at all user-visible call sites (FR-019/FR-020): live activity line, agent-view header, sub-agent tool row, task summary, context card, engine notices (cold-start, maintenance), model capacity lines.

### 3.3 Per-task headline (changed semantics)

`headlineTokens(usage)` returns cache-inclusive totals: `CacheRead + CacheMiss + Completion` when cache operands exist, else `TotalTokens`. Cache-% tag unchanged. Consumed by both the live activity line and `taskSummaryLine` → surfaces agree by construction (FR-024).

## 4. Context Card (display view-model, replaces rich readout)

Input: existing `orchestrator.ContextReport` (struct unchanged; TUI consumes a subset — dropped fields remain for bench tooling).

| Group | Line | Source |
|-------|------|--------|
| Context | in use / free of limit (+percent) | HistoryTokens, ContextLimit |
| Session | prompt / output totals (cache-inclusive) | UsageAggregate.SumPrompt, SumCompletion |
| Session | cache read / uncached | SumCacheRead, SumCacheMiss (line omitted when no cache-reporting request exists) |
| Session | cache-hit rate (all streams) | AllStreamHitRate (nil → "unavailable") |
| Session | cost (credits, `~` when estimated) | SessionCreditsUSD/Estimated (omitted when nil) |
| Models | main + sub-agent model names | session runtime settings |

Dropped from display (FR-023): per-model rows, per-pairing rows, window-category table, invalidation log, pressure diagnostics, maintenance latch line, API/active time, lines ±, steady-state rate.

## 5. TUI Model State (field-level changes)

| Field / binding | Change | Rule |
|-----------------|--------|------|
| `todoVisible` | **removed** | Panel renders whenever `busy && has items`; retire rules unchanged (FR-025/FR-026) |
| `ctrl+t` case | **removed** | No replacement (FR-025) |
| `tab` case | agent branch removed | Tab = command autocomplete only (FR-027) |
| `left` / `right` cases | **new** | Empty composer (and not in `/` palette) → `cycleAgentBack()` / `cycleAgent()`; otherwise fall through to the textarea (FR-028, Clarification Q1) |
| `cycleAgentBack()` | **new** | Reverse ring: main → last agent → … → first → main |
| Mode line | changed | Left hints: `Esc stop · ←/→ agents · / commands`; right cluster: `[skills] [mode chip] [effort]` with mode chip always rendered (`normal` muted, `auto-accept` warning) |
| Hint sub-line | **new** | `Shift + Tab to cycle` faint, right-aligned beneath the mode chip cluster; drops first on narrow widths (FR-015) |
| `commands` table | rows removed/renamed | `/errors` and `/permissions` rows deleted; `/logout` description = "Log Out of Account" (FR-012/FR-013/FR-014) |
| Slash dispatch | cases removed | `/errors`, `/permissions`, `/mode` fall through to the standard unknown-command notice (FR-018); `cyclePermission`/`setPermission` retained for Shift+Tab |
| Reasoning strings | changed | Level descriptions and chooser subtitle carry no DeepSeek mapping text (FR-016) |
| `taskSummaryLine` | marker removed | `⚠ N harness` part deleted; `TaskStats.HarnessEvents` field retained in contract (telemetry) but unrendered (FR-013, R8) |
| `formatHarnessEvents` | **removed** | Panel and its tests deleted (FR-013) |

## 6. Instructions Registry (provenance entries)

| ID | Change |
|----|--------|
| `prompt.skills.hint` | Body rewritten: read the relevant skill via `read_skill` **before** starting related work, keep applying it through the task, blend with own judgment, equip sub-agents via `run_subagent.skills`, ignore skills for unrelated tasks. `MentionsTools` becomes `["read_skill", "run_subagent"]` (drift guard DC1) |
| `prompt.skills.header` | unchanged (`SKILLS`) |
| Reasoning/effort description strings | DeepSeek mapping sentences removed (registered UI strings updated where applicable) |

## 7. State Transitions

**Skill use (per task)**: catalog frozen → task starts (providedSkills seeded from manual markers) → model may call `read_skill` at any turn → body enters tail; name joins providedSkills → repeat call returns pointer notice → task ends (per-task state cleared). Session end/resume: catalog re-discovered at next session build; aggregates rebuild from `usage.jsonl` (resume edge case in spec).

**Permission mode**: `normal ⇄ auto-accept` via Shift+Tab only (UI); chip + hint always visible; persistence unchanged (settings).

**Agent view ring**: `main → A1 → … → An → main` (right/Tab-replacement) and inverse (left); Alt+1..9 direct jumps unchanged; Esc returns to main (unchanged).
