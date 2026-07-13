# Data Model: MuhiyaCode TUI & Agent Experience Overhaul

**Feature**: `003-tui-ux-overhaul` | **Date**: 2026-07-12 | **Plan**: [plan.md](plan.md)

Entities are grouped by owning layer. "New" marks fields/types added by this feature; everything else is existing structure the feature consumes. Wire shapes live in [contracts/](contracts/); this file defines the in-process model.

## 1. Contract layer (`internal/contract`)

### 1.1 Usage (extended)

Existing per-request/per-aggregate token usage (`types.go:159-171`). Additive fields:

| Field | Type | New | Semantics |
|---|---|---|---|
| `PromptTokens` | `int` | | Provider-reported input tokens (incl. cache reads) |
| `CompletionTokens` | `int` | | Provider-reported output tokens |
| `TotalTokens` | `int` | | Prompt + completion (FR-012c basis) |
| `CacheReadTokens` | `*int` | | nil = provider did not report (→ omit displays, FR-012) |
| `CacheMissTokens` | `*int` | | nil = provider did not report |
| `CostUSD` | `*float64` | ✔ | Gateway-reported request cost in USD from `muhiya_log.cost`. nil = not received (non-allowlisted gateway, non-stream path, older gateway) → credits omitted. Never locally computed (FR-012a). |
| `CostEstimated` | `bool` | ✔ | Mirror of gateway `usage_estimated`; true ⇒ display with `~` marker (Constitution VI), false when `CostUSD` nil. |

**Validation**: `CostUSD` non-negative when present.

**Aggregation rule (credits)**: `CostUSD`/`CostEstimated` are **not** folded into `sessionUsage`/`subtractUsage` (the token-delta machinery is unchanged). Credits are always computed by scanning an **explicit member set of UsageRecords**:

- *Task credits* (2.1): records in the task's record range — the engine captures the usage-record boundary index at task start (alongside `usageStart`, `engine.go:512`) and on `TaskComplete` sums records in `(startIdx, endIdx]`. The range includes main, aux (onboarding/compaction), and subagent records — real spend is never hidden.
- *Session credits* (3.2): all `usageRecords`.
- *Member-set rules*: records that carry **no provider usage at all** (failed/timed-out calls that recorded empty usage — e.g. onboarding error path `onboarding.go:39-41` → `engine.go:526`) are excluded, matching the spec edge case "a turn with no returned usage payload is excluded from totals, never guessed". Among the remaining members, **any nil `CostUSD` ⇒ the set's credits are unavailable** (partial money totals are dishonest); `CostEstimated` ORs across members. A costless record therefore affects only the task whose range contains it (and the session sum's honesty line) — never subsequent tasks.

### 1.2 UsageRecord (extended)

Append-only per-request ledger entry (`cache.go:27-44`). Gains the same two fields as 1.1, populated at record time (`recordMainUsage`/`recordAuxUsage`/`recordIsolatedUsage`). Persisted with the session; resume re-aggregation (`usage_record_test.go`) must round-trip the new fields (backward-compatible: absent in old sessions ⇒ nil/false).

### 1.3 TaskStats (extended)

`types.go:353-382`. Token/cache metrics for the summary come from the existing per-task delta `Usage = subtractUsage(…)` (engine.go:556); credits come from the task's record range (§1.1). Two additions:

| Field | Type | New | Semantics |
|---|---|---|---|
| `StopCause` | `string` | ✔ | `""` = completed normally; `"user stop"` (context cancellation via Esc/`Engine.Cancel`), `"error"` (provider/stream error returned from the turn loop), `"disconnect"` (connection loss). Stamped by `Engine.Run` before the deferred `TaskComplete` (engine.go:554-573) fires. Drives the `interrupted` marker (FR-013a). **Not** `TerminatedReason` — that existing field is set only by the H5 circuit breakers and already renders its own "Task terminated: …" notice (`tui/model.go:294-295`); the two signals stay independent. Value constants live in `contract/types.go`. |
| record range | `int` pair (or equivalent) | ✔ | Task's usage-record boundary captured at task start, closing at completion — the credits member set (§1.1). |

## 2. TUI layer (`internal/tui`)

### 2.1 TaskSummaryEntry (new transcript entry kind)

Immutable transcript item appended on `TaskComplete`, rendered after the final assistant message (FR-013).

| Field | Type | Semantics |
|---|---|---|
| `Credits` | `*float64` | Σ `CostUSD` × 100 over the task's record range (§1.1 member-set rules); nil ⇒ segment omitted (FR-012/012a) |
| `CreditsEstimated` | `bool` | true ⇒ `~` prefix on credits (VI) |
| `TotalTokens` | `int` | FR-012c: all reported input + output across the task's turns |
| `CacheHitRate` | `*float64` | FR-012b: token-weighted ΣCacheRead ÷ Σ(CacheRead+CacheMiss); nil ⇒ omitted |
| `Interrupted` | `bool` | true when `TaskStats.StopCause ≠ ""` (§1.3; FR-013a) |

**Lifecycle**: created once per task completion; never mutated; scrolls with the transcript. **States**: n/a (immutable). **Validation**: rendered segments = exactly the non-nil subset of {credits, tokens, cache %} + optional `interrupted` marker — nothing else (FR-010/011, SC-003/006).

### 2.2 ToolDisplay (new render-layer value)

Computed per `toolView` (existing `model.go:62-66`) at render time; not stored.

| Field | Type | Semantics |
|---|---|---|
| `Label` | `string` | From the labels table (`view.go:609`), e.g. `Edit` |
| `Target` | `string` | Primary target (path/command/query), middle-truncated |
| `Outcome` | `string` | Minimal measure per tool class — see [contracts/tool-display.md](contracts/tool-display.md) (e.g. `+12 −3`, `41 lines`, `7 matches`, `exit 0`, `5 results`) |
| `Detail` | `[]string` | Expanded-view body (diff lines / boxed output / result list) |

**Derivation rules**: `+A −R` counted from the existing `--- diff ---` payload lines (`+`/`-` prefixes, excluding `+++`/`---` headers); nil-safe when no diff marker present. **Invariant**: collapsed view renders Label+Target+Outcome only (FR-006); expanded renders the same header + Detail in the uniform block (FR-007); the `verbose` flag is the only switch (FR-008).

### 2.3 Palette → SemanticTokens (rebuilt)

Replaces the 12 hardcoded styles (`render.go:13-32`).

| Token group | Tokens | Notes |
|---|---|---|
| `text` | `primary, muted, faint` | body / labels / hints |
| `accent` | `primary, soft` | brand greens; focus borders |
| `status` | `success, warning, error, info` | paired with words/symbols, never color-alone |
| `diff` | `add, remove, meta` | transcript diffs |
| `bg` | `surface, overlay` | user-message band, modal; **no forced base background** (D4) |
| `border` | `default, focus` | rules, boxes, focused input |

**Resolution**: token → (dark hex, light hex) chosen once at startup via `HasDarkBackground`/`LightDark`; `Settings.Theme` ∈ {auto, dark, light} overrides; `NO_COLOR` ⇒ attribute-only styles (bold/dim/reverse). Downsampling via `colorprofile`; 16-color mapping reviewed per token. **Validation**: monochrome legibility required — every meaning carried by color must also be carried by text or weight ([contracts/visual-system.md](contracts/visual-system.md)).

### 2.4 GlyphTable (new)

Single source for all non-ASCII glyphs: `marker.ok ●`, `marker.fail ×`, `marker.brand`, `spinner frames`, `rule ─`, `bar ▏`, `ellipsis …`, `bullet ·`, tree/border set. Two variants: unicode (width-verified on Warp/WT/iTerm2) and ascii (`MUHIYA_ASCII=1` / `Settings.UI.BorderMode=ascii`). **Invariant**: no glyph literal outside the table in `internal/tui` (enforced by review + grep check in quickstart).

### 2.5 CommandEntry visibility (extended)

Existing `{name, description}` list (`model.go:164-171`) gains a visibility predicate evaluated against session state:

| Command | Visible when |
|---|---|
| `/login` | `Secrets.ProviderAPIKey` empty (FR-019) |
| `/logout`, `/usage` | `Secrets.ProviderAPIKey` set |
| all others | always |

Direct invocation of a hidden command yields the friendly explanation, not silence (spec edge case).

## 3. Orchestrator layer (`internal/orchestrator`)

### 3.1 SkillListing (new, stable-prefix content)

Built once at session start from `workspace.DiscoverSkills` (session-pinned snapshot).

| Field | Type | Semantics |
|---|---|---|
| `Name` | `string` | frontmatter name; falls back to the skill's directory basename when frontmatter lacks `name` (existing `readSkill` behavior, `skills.go:122-124`); dedupe key, lowercased |
| `Description` | `string` | single-line; newlines collapsed; truncated at 200 chars; **may be empty** (frontmatter-less SKILL.md) — rendered in the fixed no-description form (skills-delivery §2) |
| `Path` | `string` | workspace-relative `SKILL.md` path (forward slashes, deterministic) — the advertised listing includes **workspace-resident skills only** (skills-delivery §1), so relative paths always exist and `read_file` stays inside the containment boundary |

**Rendering** (into `SystemPrompt`, [contracts/skills-delivery.md](contracts/skills-delivery.md)): sorted by lowercased `Name`; one line each; byte-identical for identical configuration (Constitution III, 002 FR-013). Empty discovery ⇒ section omitted entirely. **Lifecycle**: snapshot built at *new-session* creation and **persisted with the session** (sidecar, 001 ProbeSnapshot precedent); `/resume` and process restart restore it verbatim — the `## Skills` section is byte-identical across save/load regardless of on-disk changes; re-discovery happens only on `/new` or first launch (no per-turn or resume-time churn → no cache invalidation).

### 3.2 Session credit aggregation (derived)

`ContextReport` (existing, `engine.go:456-476`) gains session credits: Σ `CostUSD` × 100 over all `usageRecords` under the §1.1 member-set rules (empty-usage records excluded; any remaining nil ⇒ "credits unavailable — N of M requests priced" line for honesty).

## 4. Gateway wire entities (owned by contracts)

Defined normatively in [contracts/usage-api.md](contracts/usage-api.md); summarized:

- **MetaChunk** (`muhiya_log`): `{cost: USD float, log_id: string, usage_estimated: bool}` on the final SSE frame for allowlisted client apps.
- **AccountUsage** (`GET /v1/usage` response): `plan{name, windows[{name, budget_usd, current_spent_usd, reset_time}]}`, `credits{extra_total, extra_remaining}`, `spend{today_usd}`. Billing-period figures come from the plan's window entries; session figures are client-side (2.1/3.2 sums).

**Relationships**: MetaChunk.cost → Usage.CostUSD (1.1) → TaskSummaryEntry.Credits (2.1) & ContextReport session credits (3.2). AccountUsage → `/usage` modal only (never mixed into task metrics — different sources, different freshness).

## 5. Entity relationship overview

```text
gateway request_logs (ledger, USD)
  └─ muhiya_log chunk ──▶ contract.Usage.CostUSD ──▶ UsageRecord (per request)
                                   │                        │
                                   │                        └─▶ ContextReport (session credits, /context)
                                   └─▶ TaskStats.Usage (per-task delta, engine.go:556)
                                             └─▶ tui.TaskSummaryEntry (credits · tokens · cache %)
gateway GET /v1/usage ──▶ AccountUsage ──▶ /usage modal (plan windows, extra credits, today)
workspace SKILL.md files ──▶ SkillListing (session-pinned) ──▶ SystemPrompt §Skills (stable prefix)
                                   └─ on-match: read_file(SKILL.md) ──▶ turn context (dynamic)
```
