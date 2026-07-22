# Phase-cap tuning status

Date: 2026-07-22
Policy version: `014-output-v1`

The runtime now has conservative offline-safe provisional caps, but T066 is not accepted as live-data tuning. The user prohibited paid requests and tests, and the Phase-0/US1 live p75 and truncation rows therefore do not exist. The current table is an implementation seed guarded by `tokenEconomyMode`; it must not be described as provider-tuned.

| Phase | Floor (generic/DeepSeek) | Floor (MiniMax) | Default | Ceiling |
|---|---:|---:|---:|---:|
| orient | 256 | 1,024 | 512 / 1,024 | 2,000 |
| inspect | 256 | 1,024 | 1,200 | 4,000 |
| change | 256 | 1,024 | 4,000 | 16,000 |
| verify | 256 | 1,024 | 1,200 | 4,000 |
| finish | 256 | 1,024 | 800 / 1,024 | 2,000 |
| recover | 256 | 1,024 | 2,400 | 8,000 |

All values are additionally clamped by the provider ceiling, catalog ceiling, and remaining shared context window. One truncation retry may double the active cap up to the phase ceiling. A second truncation stops.

Required future evidence for T066: paired valid baseline/candidate rows, per-phase p75 output, provider finish reasons, incomplete-response counts, correctness rubrics, and the exact policy-version change justified by those rows.
