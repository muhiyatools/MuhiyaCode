# Feature 014 benchmark contract

This directory is the reproducible evidence boundary for the token-economy overhaul. Efficiency results are valid only when paired with execution correctness and provider-reported usage.

## Run classes

- `runs/baseline/`: frozen pre-enforcement binary and unchanged request behavior.
- `runs/observe/`: candidate binary with economy decisions recorded but not applied.
- `runs/balanced/`: correctness-gated conservative enforcement.
- `runs/aggressive/`: opt-in experimental enforcement; never used to justify balanced defaults.
- `raw-usage/`: immutable, sanitized provider response rows referenced by run manifests.

Every run manifest must identify the binary SHA-256, Git HEAD and dirty-manifest digest, fixture/version, provider/model/transport, upstream policy, effort, permission mode, configuration hash, prefix hashes, start/end time, and repeat ordinal. Baseline and candidate comparisons must hold all fields constant except the declared treatment.

## Required measurements

For every provider request retain availability-aware prompt, output, cache-read, cache-write/creation, and uncached-input tokens; request stream (`main` or auxiliary); phase; epoch; transport; finish reason; retry linkage; duration; and provider credit/cost when reported. Missing values remain `null`/unavailable and are never converted to zero.

For every fixture retain completion status, rubric result, changed-file fingerprints/diff summary, checks, safety violations, main/auxiliary request count, maximum prompt, replay amplification, wall time, and invalidation events. Estimates must be labelled and reconcile through an explicit residual.

## Validity rules

A run is invalid when any required fixture is absent, a provider request lacks the usage payload required by that fixture, the correctness rubric did not execute, the initial workspace/reset failed, raw rows cannot be joined to the summary, or baseline/candidate configuration drifted. Invalid rows remain visible with reasons but are excluded from aggregates. They are never scored as zero.

An optimization fails regardless of token savings when execution correctness exceeds the allowed non-inferiority margin, a critical-risk fixture regresses, a new safety violation occurs, provider protocol continuity breaks, or persistence recovery is not old-or-new atomic.

## Repetition and statistics

Run each approved provider/configuration at least twice in cold and warm conditions. Reports include every raw row plus median, p75, p95, population variance, sample size, invalid count, and availability count per metric. Do not merge distinct providers, transports, models, effort levels, or credit schedules into one headline.

Replay amplification is `sum(main prompt tokens) / max(main prompt tokens)` when all required members are available. Cache-read reduction is not a regression when total replay and weighted cost fall without quality loss.

## Retention

Committed fixtures, manifests, sanitized summaries, and release-gate evidence are durable. Raw provider rows may be compressed but must remain content-addressed and referenced by digest. Local scratch workspaces, binaries, transient logs, and credentials are excluded. Artifact deletion must not leave a report claiming reproducibility.

## Secret exclusions

Never store API keys, bearer tokens, cookies, `.env` values, full secret-bearing prompts/tool output, user home paths beyond a sanitized workspace identity, or provider response headers containing credentials. Redact before persistence; record that redaction occurred. A row that cannot be sanitized without losing required accounting is kept locally and the committed report marks the evidence unavailable.

## Directory conventions

```text
benchmarks/
  BASELINE_SHA.txt
  fixtures/*.json
  raw-usage/<run-id>/*.jsonl
  runs/baseline/<run-id>/
  runs/observe/<run-id>/
  runs/balanced/<run-id>/
  runs/aggressive/<run-id>/
  comparison.md
```

Generated report JSON uses forward-compatible schema versions and deterministic ordering. Human Markdown is a projection; raw rows and machine JSON are authoritative.
