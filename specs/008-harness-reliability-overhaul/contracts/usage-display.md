# Contract: Honest Token Display & Session Usage Panels

Feature 008. Governs the headline formula, the session usage panel, and the
context-category table. Evidence: [research.md](../research.md) R3/R4/R5/R8;
shapes in [data-model.md](../data-model.md) §4–6.

## 1. Headline figure (live activity line + task-summary line)

| ID | Requirement |
|---|---|
| UD-1 | Headline tokens = per-task `CacheMissTokens + CompletionTokens` when the task's records carry cache metrics; the SAME formula backs both the live line and the summary line (one helper, two call sites) |
| UD-2 | Cache-read tokens MUST NOT appear in any headline figure; the live cache tag reduces to `cache N%` (percentage only) |
| UD-3 | Fallback (no cache metrics in the task's records): headline = `TotalTokens` delta; cache tag renders unavailable — never a synthesized split |
| UD-4 | Estimated-usage markers (existing `~`/estimated semantics) are preserved wherever estimated records contribute |
| UD-5 | All figures derive from provider-reported usage via the existing per-task delta source; no view may disagree with `/context`'s totals for the same task (FR-017) |

## 2. Session usage panel (rendered first inside the `/context` modal)

| ID | Requirement |
|---|---|
| UD-6 | Shows: session cost in credits (member-set rule with the existing "N of M requests priced" fallback), API time (Σ non-nil request durations), active time (Σ task durations, labeled "this session"), lines `+A −R` (Σ applied-change diff counts), and session + steady-state cache-hit rates |
| UD-7 | Per-model table: one row per model observed in the session's usage records (all streams), columns: requests, uncached input (miss), output, cache read, cost; plus a Total row. Cost per model follows the member-set rule (nil ⇒ unavailable for that row) |
| UD-8 | Every metric whose source is absent renders an explicit unavailable state (no zeros standing in for unknowns; Principle VI) |
| UD-9 | After a resume, API time and per-model rows rebuild from persisted records; active time and lines± restart at zero and stay labeled "this session" |

## 3. Context-category table (inside the same modal, after the window section)

| ID | Requirement |
|---|---|
| UD-10 | Categories: system prompt · tool definitions · project memory & skills · conversation · summary (when present) · free — each with tokens and percent, labeled estimated |
| UD-11 | Σ(categories including free) = context limit ± 1 percentage point rounding (SC-006) |
| UD-12 | Sizes are measured from the components the engine already holds at assembly time and converted via the calibrated estimator — no new tokenizer dependency |

## 4. Rendering constraints (verified modal limits)

| ID | Requirement |
|---|---|
| UD-13 | Every panel line fits ≤72 columns (the modal's max usable width) using the existing 2-space-indent label/value style; numeric columns are space-padded, no box-drawing tables |
| UD-14 | Content degrades on short terminals via the modal's existing row budget; section order puts the session panel first so clipping drops detail, not headlines |
| UD-15 | All lines must survive the modal's per-line RTL display pass unchanged for LTR content (numbers/labels left-aligned as today) |
| UD-16 | Palette/glyph tokens only (no hardcoded colors/glyphs); consistent with visual-system rules |

## 5. Acceptance (maps to spec)

- SC-005: headline equals provider-reported miss+output exactly for cache-metric
  tasks; fallback equals reported total; `/context` retains full splits.
- SC-006: two-model session renders per-model rows reconciling with recorded
  usage; categories sum to 100%±1pt.
- Unit tests: formula helper (metrics/fallback/estimated), by-model aggregation
  (member-set cost), category summation invariant, ≤72-col line-width assertion
  over rendered panels.
