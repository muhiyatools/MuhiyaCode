# Cachebench comparison

SC-001 uses prefix stability; raw steady-state remains the cost/workload KPI. These historical
runs predate `new_tail_tokens`, so prefix stability is unavailable and neither gate can pass.

| Scenario | Baseline stability | Improved stability | Baseline raw | Improved raw | Variance (pp) | Cost delta | Unattributed | SC-001 | SC-005 |
|---|---:|---:|---:|---:|---:|---:|---:|---|---|
| coding-session | unavailable | unavailable | 96.57% | 96.56% | unavailable | unavailable | 0* | false | false |

\* The historical attribution logic defaulted unexplained misses to `provider`; zero is not
accepted as post-fix SC-007 evidence.
