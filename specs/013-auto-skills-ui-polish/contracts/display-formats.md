# Contract: Token & Cache Display Formats

**Feature**: `013-auto-skills-ui-polish` | Covers FR-019..FR-024 | Amends feature 008's `usage-display.md` UD-1/UD-2 (headline semantics) and the feature 011/012 context readout

## 1. Full-Digit Formatting

- C1.1 `contract.FullTokens(value int) string`: full decimal digits with ASCII comma thousands grouping (`0`, `999`, `1,234`, `1,234,567`). No abbreviation, no locale variance, no sign handling (counts are non-negative).
- C1.2 Every user-visible token figure renders through `FullTokens` (FR-020): live activity line, agent-view header line, sub-agent tool rows, task summary line, context card, cold-start/maintenance notices, model capacity lines (`/model` details). `HumanTokens` must not remain at any user-visible call site (verified by a repo-wide call-site sweep; the function itself may remain only if a non-display consumer exists).
- C1.3 Layout rule: surfaces hosting full-digit figures must tolerate 11-character values (`999,999,999`) without corrupting adjacent segments — existing `fitLine`/truncation discipline applies; no fixed-width column may silently clip a digit (FR-020 honesty beats alignment).

## 2. Cache-Inclusive Totals

- C2.1 Per-task headline (`headlineTokens`, consumed by BOTH the live activity line and `taskSummaryLine` — they may never diverge, FR-024): when both cache operands are available, tokens = `CacheRead + CacheMiss + Completion`; otherwise `TotalTokens`. The cache-% tag semantics are unchanged.
- C2.2 Per-agent figures (`agent.usage.TotalTokens` sites) already include cached prompt tokens — they switch to `FullTokens` rendering only.
- C2.3 Session totals in the context card: prompt total = `SumPrompt` (provider-reported, cache-inclusive by definition), output total = `SumCompletion`; the read/uncached split renders from `SumCacheRead`/`SumCacheMiss`.
- C2.4 Cross-surface agreement (FR-024): for the same scope, figures derive from the same aggregate/helper — never re-derived through a second formula.

## 3. Session-Wide Cache-Hit Rate

- C3.1 New aggregate field `SessionUsageAggregate.AllStreamHitRate = HitRate(PairedCacheRead, PairedCacheMiss)`: numerator/denominator include **every** session request — main, sub-agent, and aux streams — where the provider reported both operands (paired-only rule; a one-sided record never fabricates a denominator).
- C3.2 Every user-visible "session cache-hit" figure binds to `AllStreamHitRate`: the persistent usage footer (T043 site) and the context card. The per-task cache tag stays per-task (C2.1).
- C3.3 `SessionHitRate` (main-only), `SteadyStateHitRate`, and `PrefixStabilityRate` keep their current definitions for benchmark tooling but are no longer displayed anywhere.
- C3.4 Unavailability (FR-022): `AllStreamHitRate == nil` (no paired record yet, or provider reports no cache fields) renders exactly as `unavailable` — never `0%`, never an estimate.
- C3.5 Resume: aggregates rebuild from the append-only `usage.jsonl`, so the all-stream rate covers pre-resume traffic including prior sub-agent records (spec edge case).

## 4. Simplified Context Card (`/context`)

- C4.1 The card renders exactly three groups, in order, ≤14 content lines total:

```text
Context
  In use:  <FullTokens(history)> of <FullTokens(limit)> tokens (<pct>%)
  Free:    <FullTokens(free)> tokens

Session
  Tokens:     <FullTokens(SumPrompt)> in / <FullTokens(SumCompletion)> out
  Cache:      <FullTokens(SumCacheRead)> read / <FullTokens(SumCacheMiss)> uncached   ← omitted when no cache-reporting request exists
  Hit rate:   <pct AllStreamHitRate>%  |  unavailable
  Cost:       [~]<credits> credits                                                    ← omitted when SessionCreditsUSD is nil

Models
  Main:      <main model name>
  Sub-agent: <sub-agent model name>
```

- C4.2 Dropped from display (FR-023): per-model usage rows, per-pairing cache rows, window-category breakdown, invalidation log, pressure diagnostics, maintenance latch, API/active time, lines ±, steady-state rate. `orchestrator.ContextReport` keeps its fields (bench/tooling consumers); the TUI consumes the subset above.
- C4.3 Honesty markers survive the simplification: `~` prefix for estimated cost; `unavailable` states per C3.4. The card introduces **no** estimated figures of its own.
- C4.4 Modal chrome, scroll, and dismissal behavior are unchanged — content-only change.

## 5. Verification

- C5.1 Golden tests: activity line, task summary, context card, agent rows re-pinned with full-digit, cache-inclusive expectations (replacing abbreviated goldens).
- C5.2 Property check: `FullTokens` grouping for boundary values (0, 999, 1000, 999999, 1000000, 2^31-1 class values).
- C5.3 Cross-surface test: one synthetic task's live headline equals its summary figure; session footer rate equals context-card rate (same aggregate instance).
- C5.4 Sub-agent inclusion test: aggregate over records with differing main vs sub-agent cache behavior yields `AllStreamHitRate ≠ SessionHitRate` and the displayed value is the former (SC-006).
