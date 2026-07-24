# Terminal-Bench Readiness Requirements Checklist

## Isolation and Security

- [x] Benchmark mode auto-trusts only the canonical task workspace under auto-accept.
- [x] Normal interactive workspace approval remains unchanged.
- [x] Sensitive roots, symlink escapes, and out-of-root destructive paths remain denied.
- [x] Bench mode blocks repository publication actions.
- [x] One state root/process serves one task only.

## Fairness and Anti-Leakage

- [x] Prompts and tools contain no Terminal-Bench task names, fixture names, hidden-test names, or gold-patch logic.
- [x] Verification uses only generic project markers.
- [x] Result status is not presented as the objective grader verdict.
- [x] Fixed-model runs cannot silently substitute a model.
- [x] Routed runs report all model switches and reasons.

## Observability and Quality

- [x] Result records link to redacted per-tool trajectories.
- [x] Timeout, budget, blocked, and error outcomes are distinguishable.
- [x] Usage/cost provenance is labelled reported or estimated.
- [x] Repository-wide validation and container smoke results are recorded before readiness is claimed.

