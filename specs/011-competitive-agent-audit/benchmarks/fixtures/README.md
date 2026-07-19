# Benchmark Fixtures — suite_version `011-v1`

Fixed task matrix for the 011 competitive-agent benchmark suite. Committed per
[contracts/benchmark-run.md](../../contracts/benchmark-run.md) §2.6 (reproducibility): task prompts,
workspace fixtures, and runner invocation live in the repo so any reviewer can rerun a record from
its `suite_version` + `config`.

## How fixtures work

- **One fixture = one task.** `tasks.json` is the ordered task matrix; each entry names a fixture
  workspace and carries the exact `prompt` the benchmark runner (T004/D9, `scripts/bench_011.*`)
  submits to the agent in **one-shot mode**.
- **Workspaces are immutable templates.** Before each task the runner copies the named workspace
  directory to a fresh temp dir and runs the agent there — fixtures under `workspaces/` are never
  edited in place, so every run starts from an identical byte-for-byte state.
- **Records, not fixtures, carry results.** The runner emits one run record per suite execution
  (schema in contracts/benchmark-run.md §1) under `benchmarks/runs/`. `task_id` and `category` in
  each record row come verbatim from `tasks.json`; `task_class` comes from the LIVE classifier at
  run time (contract §2.7 — size segmentation always uses `task_class`, never `category`).
- **Versioning.** This matrix is `suite_version: 011-v1`. Any change to a prompt, a workspace file,
  or the task list invalidates cross-run comparability: bump the suite version (contract §1) and
  re-baseline (contract §2.1).

## Category enum (contract §1)

`trivial-docs | trivial-comment | rename | format | config | standard-logic | risky-auth |
risky-billing | large-multifile | greenfield-scaffold`

`category` describes **fixture intent** (what gating behavior the task provokes). It is never used
for size segmentation — that is `task_class` (`chat|tiny|small|standard|large|epic`).

## Task distribution (22 tasks)

| Category | Count | Task ids | Workspace |
|---|---|---|---|
| trivial-docs | 3 | td-001..td-003 | wsdocs |
| trivial-comment | 2 | tc-001, tc-002 | wsdocs |
| rename | 2 | rn-001, rn-002 | wsdocs |
| format | 2 | fm-001, fm-002 | wsdocs |
| config | 2 | cf-001 (wslogic), cf-002 (wsauth) | wslogic, wsauth |
| standard-logic | 5 | sl-001..sl-005 (1-file/~10-line up to 5-file) | wslogic |
| risky-auth | 2 | ra-001 (one-line auth-check removal), ra-002 | wsauth |
| risky-billing | 1 | rb-001 | wsauth |
| large-multifile | 2 | lm-001, lm-002 (oversized-review fixture) | wslogic |
| greenfield-scaffold | 1 | gs-001 | wsempty |

Every prompt names exact files/symbols that exist in its workspace, so runs are deterministic.
`expect.review_tier_default` is the tier the review-gating contract
([contracts/review-gating.md](../../contracts/review-gating.md) §2–3) predicts in **default** gating
mode on a healthy run (tests passing); `notes` cites the rule and any conservative-mode difference.

## Fixture workspaces

| Workspace | Contents | Serves |
|---|---|---|
| `workspaces/wsdocs` | Go string-utility lib: README (deliberate 'libary' typo), stringutil.go (wrong doc comment on `Reverse`, misspelled `Captialize`), truncate.go (unsorted imports, `maxLen` param), padding.go (deliberately misformatted), tests | trivial-docs, trivial-comment, rename, format |
| `workspaces/wsauth` | Toy HTTP service: middleware/auth.go with a clearly-removable `if !authorized { 401 }` check, billing/billing.go price/refund math, main.go, config.yaml | risky-auth, risky-billing, config |
| `workspaces/wslogic` | 3-package module (calc/store/report, 7 files) with a seeded off-by-one in `calc.Average` (divides by `len+1`; not covered by a committed test — the workspace is green at rest), tests, config.json | standard-logic, large-multifile, config |
| `workspaces/wsempty` | `.gitkeep` only | greenfield-scaffold |

Invariants: every workspace file is ≤ 40 lines; each workspace has ≤ 8 files; every non-empty
workspace passes `go build ./...` and `go vet ./...` (deliberate gofmt violations in wsdocs are
format-task targets, not build breaks).

## SC coverage map

| SC | Exercised by |
|---|---|
| SC-001 (trivial auto-review < 10%) | all trivial-docs/trivial-comment/rename/format/config tasks + sl-001..sl-003 (expected `skip`) |
| SC-002 (high-risk review retention ≥ 95%) | ra-001 (the H4 one-liner), ra-002, rb-001 (expected ≥ `focused`) |
| SC-003 (median small-task tokens ↓ ≥ 30%) | the `task_class ∈ {tiny, small}` population — in practice the trivial categories + sl-001..sl-003 |
| SC-004 (review overhead ≤ 20%; ceiling compliance) | overhead: small/medium segment of the whole suite; ceiling: lm-002 (oversized fixture, expect `ceiling_hit:true` + CoverageReport) |
| SC-005 (mixed-model cache pins) | full suite rerun under the mixed MiniMax/DeepSeek config (quickstart Step 3) |
| SC-006 (tool discipline < 2% violations) | every task — `violations` counters in each record row |
| SC-008 (variance band) | double runs of the full matrix per config (contract §2.2) |
| SC-009 (rationale line + gating modes) | every task emits `review: <tier> — <reason>`; risky fixtures rerun with `review_gating off` (quickstart Step 1 'Gating off') |

Greenfield edge case (spec: "heavyweight review must not fire on initial scaffolding") is gs-001 via
the greenfield cap (review-gating §3 modifier).
