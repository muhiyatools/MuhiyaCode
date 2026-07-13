# Contract: Task Summary & Thinking Indicator

**Feature**: `003-tui-ux-overhaul` | Serves FR-009..013a, SC-003/004/006 | Consumes [usage-api.md](usage-api.md), data-model §1/§2.1

## 1. Task boundary

A **task** spans from the user's prompt submission to the engine's `TaskComplete` callback for that prompt (all intermediate turns and tool rounds included). Token/cache metrics use the existing per-task delta `TaskStats.Usage = subtractUsage(sessionUsage, usageStart)` (`engine.go:556`). **Credits use a per-task record scan, not the delta**: the engine captures the usage-record boundary at task start and sums `CostUSD` over the records in that range at completion — main, aux (onboarding/compaction), and subagent records included (real spend is never hidden); member-set and nil rules in [data-model §1.1](../data-model.md). Steering messages mid-task do not start a new task; a queued follow-up prompt starts the next one.

## 2. Thinking indicator lifecycle

| Phase | Display | Source |
|---|---|---|
| Reasoning streaming | one line: `▏ thinking… <tail>` + elapsed time (gutter glyph from the [visual-system §2](visual-system.md) table; Ctrl+O expands to the ~12-line reasoning tail — existing behavior) | `m.reasoning`/`m.busy` |
| Reasoning done, task still busy | indicator row shows current status (tool activity), no thinking text | existing status row |
| Task complete | **nothing** — the thinking row and any duration text are gone; `lastThoughtDuration` rendering removed | D7 |

**Prohibited**: any post-task "thought for Ns" text (FR-009), any effort label anywhere in the transcript or status area (FR-010).

## 3. Summary entry — content

Rendered as one transcript line, dimmed relative to body text, directly after the task's final assistant message (FR-013), preceded by a blank line:

```
credits 2.42 · 48.3k tokens · cache 94%
~credits 2.42 · 48.3k tokens · cache 94%          (cost partially estimated)
credits 1.10 · 12.9k tokens · cache 91% · interrupted
48.3k tokens · cache 94%                          (credits unavailable)
credits 2.42 · 48.3k tokens                       (cache fields unavailable)
```

| Segment | Formula | Omission rule |
|---|---|---|
| `credits C` | Σ `CostUSD` × 100 over the task's usage-record range, 2 decimals (data-model §1.1) | records with no provider usage are excluded (spec edge case); omitted if any **remaining** member has nil `CostUSD`; `~` prefix if any member `CostEstimated` |
| `T tokens` | `TaskStats.Usage.TotalTokens` (all reported input incl. cache reads + output; FR-012c) | always present (0-token tasks show nothing — no summary at all if no request completed) |
| `cache H%` | `CacheReadTokens ÷ (CacheReadTokens + CacheMissTokens)` from the task delta, token-weighted (FR-012b), rounded to whole % | omitted when either pointer nil (FR-012) |
| `interrupted` | appended when `TaskStats.StopCause ≠ ""` — new field stamped by the engine on the three early-end paths: `"user stop"` (Esc → `Engine.Cancel` context cancellation), `"error"` (provider/stream error from the turn loop), `"disconnect"` (connection loss); constants in `contract/types.go` (data-model §1.3; FR-013a). Independent of the existing H5-breaker `TerminatedReason`, which keeps its own "Task terminated" notice. | absent on normal completion |

**Prohibited content** (SC-006): duration, effort, task class ("chat"), tool counts, agent counts, file counts, cached/new token breakdown, session totals or percentages, invalidation notes. All diagnostics remain available in `/context` ([research D10](../research.md)).

## 4. Lifecycle & placement rules

- Created exactly once per task on `TaskComplete` (`statsMsg` path, `bridge.go` → `model.go`); immutable; scrolls with the transcript history.
- The old bottom-docked usage footer (`renderUsageFooter`, `view.go:111-125`) is **removed** from `View()` composition; no metric renders between transcript and input box.
- Interrupted tasks (Esc/stop, stream error, disconnect): the entry renders with metrics for turns whose usage was received; turns with no usage payload are excluded from all three sums, never estimated (spec edge case).
- Session-switch/`/new`/`/resume`: summaries are part of transcript history and persist/restore with it.

## 5. Number formatting

- Tokens: `<1000` verbatim; `≥1000` → `N.Nk`; `≥1_000_000` → `N.NM` (existing `formatTokens`).
- Credits: 2 decimals, no unit suffix beyond the word `credits`; never converted to raw USD in the summary (USD detail lives in `/usage`).
- Cache: whole percent, no decimals.

## 6. Test obligations (D14)

- Update-layer: summary emitted once per task; segment omission for each nil combination; `~` marker; footer absence.
- Interruption: each of the three `StopCause` paths (user stop / error / disconnect) produces the `interrupted` marker; H5-breaker termination does not set it (keeps its own notice); Esc path also exercised manually (quickstart Scenario 3 step 4).
- Credits robustness: a task containing a failed/empty-usage aux call (e.g. timed-out onboarding) still renders credits from its priced members; a task containing a usage-bearing record with nil `CostUSD` omits credits — and only that task does (next task unaffected).
- Golden (80×24/Ascii): each §3 example line.
- Accuracy (SC-004): benchmark run cross-check — Σ per-task credits over a session == Σ gateway `request_logs.cost` × 100 for the same `log_id` set (via `muhiya_log.log_id`), scoped to the tasks' member sets, exact match.
