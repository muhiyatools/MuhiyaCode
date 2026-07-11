# Baseline benchmark summary

- Source commit: `ba2814e` (`baseline-ready`)
- Build label: `baseline`
- Scenario: `coding-session` (24 scripted turns per run)
- Model: `deepseek-v4-flash` (concrete model, not a router)
- Effort: `max`
- Runs: 3
- Completed scripted turns: 72/72
- Provider requests: 277
- Prompt tokens: 4,715,353
- Completion tokens: 48,383
- Cache-read tokens: 4,546,560
- Cache-miss tokens: 168,793
- Mean session hit rate: 96.4156%
- Mean steady-state hit rate: 96.5699%
- Steady-state range: 96.4543%–96.7473% (0.2931 percentage points)
- Unavailable usage records: 0
- Unattributed misses: 0
- Total wall time: 760,205 ms
- Monetary cost: unavailable; the gateway model catalog did not expose an authoritative price table, so no rate was fabricated.

Per-run JSON contains the verbatim usage records and derived aggregates. The adjacent
`*-raw-usage.jsonl` files preserve the provider's raw usage objects as ground truth.

The baseline is repeatable within the ±1 percentage-point variance requirement and remains
below the 99% steady-state target in every run, establishing the required pre-optimization arm.
