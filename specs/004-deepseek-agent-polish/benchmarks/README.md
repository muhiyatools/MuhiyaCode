# Benchmarks — 004 Coding Agent Quality Polish

Measurement procedure for the constitution-X verification of this feature. These
runs are **live and billed** against the MuhiyaLLM gateway with DeepSeek credits
and are owned by the operator; they are not runnable in the offline
implementation pass.

## Procedure (D9)

1. **Baseline first** — before any behavior change lands, capture `coding-session`
   and `fat-context` scenarios, N≥3 runs each, price flags supplied, into
   `baseline/`. Record: steady-state hit rate, prefix-stability rate, tokens,
   cost, invalid-call count, redundant-call count, manual interventions.
2. **Replay probe** — run the `reasoning_content` multi-turn tool-call probe
   ([contracts/deepseek-wire.md](../contracts/deepseek-wire.md) §3) against
   `deepseek-v4-pro` and `-flash`; write the verdict to `replay-probe.md`.
3. **After** — identical config, matched time-of-day window (peak/off-peak is a
   2× multiplier on Beijing hours, so token counts are the primary comparator),
   into `improved/`; then `-compare`.

## Merge gates

- Prefix stability ≥99% on every run.
- Steady-state hit rate ≥ baseline (variance ≤1.0 pp).
- Median tokens and cost per completed task ≤ baseline.
- Invalid + redundant calls per session −50% vs baseline (SC-003).
- ≥95% of tasks reach a terminal state without manual rescue (SC-008).

## Offline-verified now

Every code change in this feature is covered by `go test ./... -count=1` (all
packages green) and `go vet ./...` (clean). The one prefix-affecting change (the
US4 prompt tightening) is verified byte-deterministic and within the size budget
offline (`prompt_budget_test.go`, `prompt_stability_test.go`,
`restart_determinism_test.go`); its cache-hit validation is the operator's live
run per the procedure above.
