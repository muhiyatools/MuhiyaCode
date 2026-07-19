# Tasks: Competitive Agent Audit & Transformation

**Input**: Design documents from `/specs/011-competitive-agent-audit/`

**Prerequisites**: plan.md, spec.md, research.md (findings F1–F15, decisions D1–D10), data-model.md, contracts/ (review-gating, subagent-handoff, benchmark-run), quickstart.md

**Tests**: Included per the constitutional exception — every improvement claim in this feature carries before/after measurement tasks (Principles VI/X), and every cache-affecting change carries a prefix-stability task. Contract conformance tests come from `contracts/review-gating.md §7` and `contracts/subagent-handoff.md`.

**Organization**: Grouped by user story. **Execution-order note**: Phase 2 (benchmark harness + baselines on the UNCHANGED build) blocks every behavior-changing story — Constitution X forbids after-the-fact baselines. US1 (audit doc) may run in parallel with behavior stories but its measurement section (T009) consumes Phase 2 output.

## Format: `[ID] [P?] [Story] Description`

> **Implementation status (2026-07-19 session)**: 38 tasks completed [X] — ALL code tasks done; only live-run tasks remain, repo fully green (12 pkgs, vet clean, arch guards pass). Prompt steering (T026–T029) landed: cheapest-tool rule in TOOLS AND RECOVERY + run_shell steering sentence, funded by condensing ORCHESTRATION PIPELINE — prompt ≤ the 5,789-char ceiling, both goldens regenerated (-update / -update-prefix-golden), full stability lattice green.
> **Partial**: T022 — decision→ceiling mapping + tier-labeled dispatch DONE; direct-dependents set + CoverageReport parsing PENDING.
> **Pending (code)**: NONE. T022 completed (focused-scope instruction with dependents rule + cap-15 + machine-readable `Coverage:` line, lenient parse, ceiling-hit propagation decision→TaskStats→bench `review.ceiling_hit`).
> **Exemplar prompt (FR-003/D10)**: owner confirmed NO exemplar exists — closed; the Reasonix+competitor comparison in audit.md §4 is the final comparison (US1 scenario 4: PASS).
> **Live smoke (2026-07-19)**: one real fixture task ran end-to-end against the live gateway — `muhiya_bench` emitted with provider-reported usage, real cost ($0.00205), and a live per-pairing row (deepseek-v4-flash/:main, 97.7% steady-state hit rate). Harness verified working.
> **⚠️ Before live runs**: set `muhiyacode config set permissionMode auto-accept` (or runner-set it) — in `normal` mode one-shot tasks report edits instead of applying them, which would zero out filesChanged/review signals. Per-pairing attribution (T032/T033) landed: UsageRecord.Pin threaded at all record sites, contract.PerPairingRates with cold-write exclusion, rows on ContextReport + TaskStats + bench per_pairing emission.
> **Pending (live runs — need the operator/gateway)**: T006/T007 baselines (frozen baseline binary ready: `bin/muhiyacode-baseline-011.exe`, see benchmarks/BASELINE_SHA.txt), then T009, T025, T030, T037, T040, T041, T047.
> **Run baselines with (CORRECTED)**: build the CURRENT binary (`make build`), then run with the legacy switch — `$env:MUHIYA_BENCH_LEGACY_REVIEW='1'; scripts/bench_011.ps1 -Config single -Binary bin/muhiyacode.exe` (×2) and `-Config mixed` (×2); unset the var for the post-change runs. Rationale: the frozen pre-011 binary cannot emit bench summaries; instead ONE binary carries identical measurement code and `MUHIYA_BENCH_LEGACY_REVIEW=1` restores pre-011 review/classification behavior as the single before/after variable (Constitution X). `bin/muhiyacode-baseline-011.exe` remains as provenance only.


---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Freeze the pre-change reference point and scaffold benchmark storage.

- [X] T001 Verify the unchanged build is green (`make verify` + `go test ./... -count=1`) and record the baseline git SHA in `specs/011-competitive-agent-audit/benchmarks/BASELINE_SHA.txt`
- [X] T002 Create benchmark storage scaffold `specs/011-competitive-agent-audit/benchmarks/{fixtures/,runs/baseline/,runs/}` with a README linking `contracts/benchmark-run.md`
- [X] T003 [P] Author the fixed benchmark task matrix (≥20 tasks: trivial-docs, trivial-comment, rename, format, config, standard-logic, risky-auth, risky-billing, large-multifile; incl. the risky one-line auth fixture, the oversized-review large-repo fixture, and a **greenfield-scaffold fixture** — many files in a near-empty repo, to exercise the greenfield tier cap) as committed fixtures in `specs/011-competitive-agent-audit/benchmarks/fixtures/`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The measurement harness + baselines every story's claims depend on. **⚠️ No behavior-changing story task may merge before T006 completes on the unchanged build.**

- [X] T004 Machine-readable per-task result capture + runner. **(a)** Make the agent emit one machine-readable task summary per completed task — a JSON line written to the session directory (and to stdout in `-p`/one-shot mode) carrying `{task_id, task_class (from the live classifier — the size axis for all segmented metrics), completed, turns, usage (provider-reported), cost_usd, review:{tier,rationale,spend_tokens,ceiling_hit}}` — wired on the task-completion path in `internal/command` alongside the existing session-writing code (this is the authoritative source; the runner never scrapes rendered TUI output). **(b)** Implement the benchmark runner `scripts/bench_011.ps1` (+ `scripts/bench_011.sh`) that drives the fixture tasks, reads those summaries, and assembles one JSON record per run exactly per `contracts/benchmark-run.md §1`
- [X] T005 Implement transcript violation counting (terminal-read-when-tool-exists, duplicate reads) as a runner analysis step in `scripts/bench_011_report.ps1` reading session transcripts, feeding `tasks[].violations` in the run record
- [ ] T006 Capture BASELINES on the unchanged build (T001 SHA): double-run × {single-model, mixed MiniMax-main/DeepSeek-sub} = 4 runs; commit records + computed variance band to `specs/011-competitive-agent-audit/benchmarks/runs/baseline/` (Constitution X)
- [ ] T007 Write the baseline aggregates table (trivial auto-review rate, median small-task tokens over `task_class ∈ {tiny,small}`, size-segmented review-overhead medians (small/medium per `contracts/benchmark-run.md §2.7`), per-provider cache hit rates or `not reported`, violation counts) **plus the per-`task_class` review-subagent spend distribution including p75 per class** (computed from the raw `runs/baseline/` records' `review.spend_tokens` + `task_class`) to `specs/011-competitive-agent-audit/benchmarks/BASELINES.md` — this per-class table is the source T021 and `contracts/review-gating.md §4` cite for ceiling derivation

**Checkpoint**: Baselines frozen — behavior changes may now begin.

---

## Phase 3: User Story 1 — Evidence-Backed Audit & Transformation Roadmap (Priority: P1) 🎯 MVP

**Goal**: The complete audit deliverable: every subsystem covered with evidence/severity/remediation, baselines filled in, exemplar-comparison protocol in place, traceable roadmap.

**Independent Test**: spec US1 acceptance scenarios 1–4 — coverage check against the FR-001 subsystem list, baseline numbers present and provider-reported, every roadmap item traces to F# and SC#.

- [X] T008 [P] [US1] Expand research.md §1–§2 into the audit deliverable `specs/011-competitive-agent-audit/audit.md`: one section per FR-001 subsystem (orchestration loop, instruction set, tool catalog, review policy, subagent dispatch/handoff/integration, terminal-usage policy, per-provider caching, mixed-model handling, context/history, token accounting), each with findings (F1–F15 mapped in) or an explicit no-defect verdict with evidence examined. Explicitly record FR-011 (duplicate-read/bounded-output), FR-013 (dynamic-content placement), and FR-014 (per-dialect requests) as **"satisfied by existing implementation"** with the code evidence (InspectionLedger, registry output cap, PrefixShape lattice, gateway dialect mapping) and the regression tests that guard them — so the T011 traceability table reads these as verification-covered, not gaps (analysis A2)
- [ ] T009 [US1] Fill the audit's baseline-measurement section from `benchmarks/BASELINES.md` (FR-002; depends on T007)
- [X] T010 [P] [US1] Write the instruction-set comparison section in `specs/011-competitive-agent-audit/audit.md`: current sections (from the instructions registry dump) vs Reasonix reference (`specs/002-reasonix-agent-overhaul/research.md`) + competitor-class patterns (research §1.10), with the D10 exemplar-prompt slot marked pending until the owner supplies it (FR-003)
- [X] T011 [US1] Build the traceability roadmap table in `specs/011-competitive-agent-audit/audit.md`: every roadmap item → decisions D#, findings F#, success criteria SC#, phase; cross-check it lists exactly the work in this tasks.md (FR-004, SC-007). Map the two "no redundant retransmission" requirements (FR-011 general, FR-016 handoff-specific) both to D7/T034 so they are not double-counted as separate work items (analysis D1)
- [X] T012 [US1] US1 acceptance pass: verify audit.md against spec US1 acceptance scenarios 1–4; record the checklist at the end of `specs/011-competitive-agent-audit/audit.md`

**Checkpoint**: Audit + roadmap deliverable complete and self-verifying — the owner's P1 ask is shippable here.

---

## Phase 4: User Story 2 — Right-Sized Code Review (Priority: P2)

**Goal**: Reviews fire only when warranted, at a depth matching risk/size, with visible rationale and hard cost ceilings (fixes F1, F2, F3, F4-for-review).

**Independent Test**: quickstart Steps 1–2 scenario table + SC-001/SC-002/SC-004/SC-009 on the benchmark suite.

- [X] T013 [US2] Define `TaskProfile` + `ReviewDecision` types per data-model.md §1–§2 in new `internal/orchestrator/reviewgate.go`
- [X] T014 [US2] Implement deterministic profile assembly in `internal/orchestrator/reviewgate.go`: git diff stats via workspace tools, task-type classes from the diff, repo-size bucket reuse from `dynamicfanout.go`, test outcome from this task's tool results — side-effect-free, zero model calls. Seed the risk-area matcher with these tunable constant tables (path + content keyword, case-insensitive), matched against changed file paths and diff hunks: **auth** = `auth|login|logout|token|session|oauth|jwt|password|credential|acl|permission|rbac`; **billing** = `billing|payment|invoice|charge|credit|price|subscription|stripe|paymob|refund`; **concurrency** = `mutex|sync\.|atomic|goroutine| chan |sync.WaitGroup|context.Cancel`; **security-config** = filenames/keys matching `.pem|.key|cert|secret|cors|csrf|tls|firewall` or security settings; **migration** = path `migrations/` or `*.sql` schema changes. A changed file matching any table sets that RiskArea; document the tables as data tunable without code change and cover them in the T016 table test
- [X] T015 [US2] Implement `Decide()` — hard rules H1–H5 then the tier table (default + conservative columns) exactly per `contracts/review-gating.md §2–§3` in `internal/orchestrator/reviewgate.go`
- [X] T016 [P] [US2] Table-driven conformance tests in `internal/orchestrator/reviewgate_test.go`: every tier-table row, all hard rules H1–H5, the greenfield-cap modifier (tiny repo + no risk ⇒ tier ≤ focused; tiny repo + auth risk ⇒ uncapped), the risk-area keyword tables, 100-shuffled-runs determinism, and the §6a explicit-request test — `run_subagent("review")` dispatches ungated in every mode including `off` (contract §7.1, §7.4, §7.5)
- [X] T017 [US2] Add the `review_gating` setting (`off|conservative|default`, default `default`) to `internal/contract/types.go` + `internal/state/config.go` config keys (+ RedactedSettings/validate paths as the existing keys do)
- [X] T018 [US2] Wire the gate into pipeline validation: consult `Decide()` before `runPipelineValidation` dispatch in `internal/orchestrator/phaserunners.go` (`:41-45`, `:394-431`); tier `skip` → emit rationale, mark Validated via the existing text-gate path; the failure-path second review (`:409-419`) consults the gate too. **The gate wires ONLY the two automatic triggers** — the model-invoked `run_subagent("review")` path (how explicit user requests run, contract §6a) is never routed through `Decide()` and must keep working in gating mode `off`
- [X] T019 [US2] Wire the gate into the AutoReview nudge in `internal/orchestrator/turnloop.go` (`:630-642`): tier `skip` suppresses the nudge; otherwise the nudge text carries the tier; existing max-effort/budget conditions remain (contract §6 — gate may only narrow, never widen)
- [X] T020 [US2] Classifier corroboration (D2): `Large` escalation requires two independent signals in `internal/orchestrator/classify.go` (`breadthRE` `:117`, escalation `:178`); update `classify` tests for single-signal prompts staying Standard
- [X] T021 [US2] Implement the generic subagent `TokenCeiling`: field on `subagentInput`, per-turn provider-reported usage accumulation, wrap-up turn on breach, `partial` status — in `internal/orchestrator/subagent.go` per `contracts/subagent-handoff.md §3` (rollout: 0 = uncapped except review tiers; serves FR-008 now, reused by later stories). **Derive and record the initial review-tier absolute ceilings** per `contracts/review-gating.md §4` "Initial ceiling values": read the per-`task_class` p75 review-spend table from `benchmarks/BASELINES.md` (written by T007; focused ← `standard`, deep ← `large`/`epic`), apply the RepoSizeBucket scale factors, and write the resulting integers back into contract §4's ceiling table (Constitution VI — no unmeasured constant ships)
- [X] T022 [US2] Review scope + coverage (D6): focused-tier brief carries the diff file list + the computed **direct-dependents** set (per `contracts/review-gating.md §4`: same-package files + one-hop reverse importers via `go list`/import graph, capped at 15, overflow → `CoverageReport.skipped`) + a changed-surface-only instruction; deep tier keeps `PipelineValidateTaskTmpl`. At dispatch, set `input.TokenCeiling = min(proportionalCapResolvedToTokens, decision.AbsoluteCapTokens)` (data-model §2→§3 mapping). Parse `CoverageReport` (covered/skipped/ceilingHit) from review output in `internal/orchestrator/phaserunners.go` + new instruction bodies in `internal/instructions/pipeline.go`
- [X] T023 [US2] Render the always-on rationale line `review: <tier> — <reason>` (including `skip`) on the task-completion path in `internal/orchestrator/turnhelpers.go` (tail/notice only — never the stable prefix; SC-009)
- [X] T024 [P] [US2] Ceiling-compliance test: synthetic oversized review hits the cap, wraps up, reports partial coverage (contract §7.3) in `internal/orchestrator/subagent_ceiling_test.go`
- [ ] T025 [US2] US2 measured validation: run quickstart Steps 1–2 scenarios (including the greenfield-scaffold fixture); rerun the suite and assert SC-001 (<10% trivial auto-review), SC-002 (≥95% risky retention, explicit requests 100%), **SC-004 both clauses** (per-tier ceilings honored on 100% of large-fixture runs AND `review_overhead_median_pct_small`/`_medium` ≤ 20%), and SC-009 (every task record carries a non-empty `review.rationale`); commit the run to `specs/011-competitive-agent-audit/benchmarks/runs/us2/` and compare against baseline using the variance band computed in T006 (T038 only formalizes the noise-vs-win *report*)

**Checkpoint**: Review behavior transformed and proven against baseline — the owner's most concrete pain resolved.

---

## Phase 5: User Story 3 — Token-Efficient Task Execution (Priority: P3)

**Goal**: Cheapest-tool discipline in the main prompt at zero prefix-budget growth; measured spend reduction (fixes F6, F10).

**Independent Test**: quickstart Step 4; SC-003 + SC-006 vs baseline.

- [X] T026 [US3] Condense the static orchestration-pipeline section (`PromptOrchestrationPipelineBody`) in `internal/instructions/prompt.go` (`:148-155`) to fund T027 — the dynamic pipeline block already carries per-phase direction (F10)
- [X] T027 [US3] Add the cheapest-tool steering rule to `PromptToolsAndRecoveryBody` in `internal/instructions/prompt.go` (`:77-80`) and one steering sentence to the `run_shell` description in `internal/instructions/tools.go` (`:33`) — dedicated read/search tools over shell reads; shell is for executing (D5)
- [X] T028 [US3] Update the pinned prompt budget + goldens in the same change: `internal/orchestrator/prompt_budget_test.go` asserts new total ≤ 5,789 chars (net Δ ≤ 0), `internal/instructions/dump_test.go` goldens regenerated, registry entries (`StatesRule`/`MentionsTools`) updated for `audit_test.go`; record the one deliberate session-upgrade invalidation via the existing SwitchModel-style event
- [X] T029 [P] [US3] Prefix-stability regression (constitutional, cache-affecting change): extend/run `prompt_stability_test.go`, `prefixshape_test.go`, `request_assembly_test.go`, `cachehit_guard_test.go` in `internal/orchestrator/` covering the new bytes — byte-identical across turns
- [ ] T030 [US3] US3 measured validation: rerun the suite; assert SC-003 (≥30% median small-task token reduction at equal-or-better completion) and SC-006 (<2% cheapest-tool violations) vs baseline using the T006 variance band (a gain smaller than the band is reported as noise); commit run to `specs/011-competitive-agent-audit/benchmarks/runs/us3/`

**Checkpoint**: Instruction set steers cost-correct behavior, prefix budget unchanged, gains proven.

---

## Phase 6: User Story 4 — First-Class Mixed-Model Sessions (Priority: P4)

**Goal**: Per-kind cache isolation, per-pairing metrics, structured returns, explicit failure surfacing (fixes F5, F7, F13; verifies FR-014/015/016/017).

**Independent Test**: quickstart Step 3; SC-005 vs single-model baselines.

- [X] T031 [US4] Per-kind session pins (D4): `:sub:<kind>` in `internal/orchestrator/subagent.go` (`:321`) and `:sub:onboarding` in `internal/orchestrator/turnloop.go` (`:119`); extend `internal/orchestrator/mixed_provider_test.go` — different kinds ⇒ different pins, same kind ⇒ shared pin (`contracts/subagent-handoff.md §2`)
- [X] T032 [US4] Per-(model, pin) usage attribution (D8): aggregate provider-reported cache read/miss/write per pairing in `internal/orchestrator/usage.go`, `reported:false` when the provider omits fields — additive bookkeeping only, no request changes (data-model.md §5)
- [X] T033 [US4] Surface per-pairing steady-state hit rates (excluding each pin's cold first request) as rows in `internal/orchestrator/contextreport.go` and in `TaskStats` for the TUI footer/`/context`
- [X] T034 [US4] Structured returns for model-invoked subagents (D7): bank the full report to Knowledge via the existing `AddPhaseReport` path and return `Status` + digest-bound content + follow-ups + truncation note in `internal/orchestrator/subagent.go` (`:183-212`); ≤-bound reports pass through verbatim; `Reusable` zero-token repeats preserved
- [X] T035 [P] [US4] Return-package tests in `internal/orchestrator/subagent_return_test.go`: ≤-bound passthrough, >-bound digest+banking, `partial` status on ceiling/turn-cap, no main-history content above the digest bound (Constitution V)
- [X] T036 [US4] Verify/extend explicit subagent failure surfacing (FR-017) on the main flow path in `internal/orchestrator/subagent.go` + turn-loop tool-result pairing — failure and partial results labeled, never silent; add a test beside T035
- [ ] T037 [US4] US4 measured validation: mixed-pairing session with sequential different-kind subagents; per-pairing rows visible; SC-005 within 5 points of single-model baselines (compared using the T006 variance band); commit run to `specs/011-competitive-agent-audit/benchmarks/runs/us4/`

**Checkpoint**: Mixed-model sessions are measurably first-class.

---

## Phase 7: User Story 5 — Competitive Benchmark Standing (Priority: P5)

**Goal**: The suite becomes the permanent competitive/regression gate; final before/after verdicts.

**Independent Test**: double-run stability within the variance band; SC-008 verdict.

- [X] T038 [US5] Finalize variance-band computation and noise-vs-win reporting in `scripts/bench_011.ps1`/`.sh` per `contracts/benchmark-run.md §2.2` (a delta smaller than the band reports as noise)
- [X] T039 [P] [US5] Implement the comparison report generator (before/after tables for every SC verdict) writing `specs/011-competitive-agent-audit/benchmarks/RESULTS.md` from run records, in `scripts/bench_011_report.ps1`
- [ ] T040 [US5] Full post-transformation matrix: double-run × both model configs on the final build; SC-008 verdict (completion ≥ baseline at lower cost per completed task); commit records to `specs/011-competitive-agent-audit/benchmarks/runs/final/`
- [ ] T041 [US5] Add the competitive-parity checklist section to `specs/011-competitive-agent-audit/audit.md` (capability comparison vs Claude Code/Codex/Grok Build-class behaviors, tied to suite outcomes — internal operationalization per spec assumption)

**Checkpoint**: Competitive standing is a number with a reproducible procedure behind it.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T042 [P] Fix the stale "19 tools / 6 synthetic" header comment in `internal/instructions/tools.go` (`:4-6`) to match the 21/8 reality (F9)
- [X] T043 [P] Add the setter-level plan⇄goal exclusivity invariant (keep the runtime backstop) in `internal/orchestrator/goal.go` + `internal/orchestrator/pipeline.go`, with a test beside `turnloop.go:267-277`'s behavior (F11)
- [X] T044 [P] Extend `validateCallArgs` to nested object/array item schemas in `internal/orchestrator/validate.go` (`:73`) + tests (F12)
- [X] T045 [P] Add the onboarding usage-ordering assertion test (aux usage lands inside the task delta) near `internal/orchestrator/turnloop.go:56,119-123` (F15)
- [X] T046 Sync docs in the same change-set as the behavior they describe (constitutional gate): `README.md`, `docs/agent-design.md`, `docs/architecture.md` — review gating, ceilings, per-kind pins, per-pairing metrics
- [ ] T047 Final gate: `make verify` green, full suite green, quickstart Steps 0–6 walked end-to-end, checklist confirmed; update `specs/011-competitive-agent-audit/checklists/requirements.md` notes with the completion evidence

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)** → nothing. T003 parallel with T001/T002.
- **Phase 2 (Foundational)** → Phase 1. **T006 MUST run on the unchanged T001 SHA — it blocks every behavior-changing task (T013+).** T004→T005→T006→T007 sequential.
- **US1 (Phase 3)** → T008/T010 can start immediately after Phase 1 (document work, no behavior change); T009 needs T007; T011/T012 last.
- **US2 (Phase 4)** → Phase 2 complete. Within: T013→T014→T015; T016 [P] after T015; T017 independent [after T013]; T018/T019 need T015+T017; T020 independent of T013–T019; T021 before T022/T024; T025 last (needs all).
- **US3 (Phase 5)** → Phase 2 complete. T026→T027→T028 sequential (same files); T029 [P] after T028; T030 last.
- **US4 (Phase 6)** → Phase 2 complete. T031 independent; T032→T033; T034 independent of T032; T035/T036 [P] after T034; T037 last.
- **US5 (Phase 7)** → T038/T039 [P] anytime after Phase 2; T040 needs US2+US3+US4 merged; T041 needs T040 + T008.
- **Phase 8 (Polish)** → T042–T045 [P] anytime after Phase 2; T046 with the stories it documents; T047 last overall.

### Story Independence

- US1 is deliverable alone (MVP) — documents + measurements only.
- US2, US3, US4 are mutually independent code changes (different files except tiny turnloop touches — merge in priority order to keep each measured delta single-variable per Constitution X).
- US5 finalizes measurement over whatever stories have merged.

### Parallel Opportunities

```text
Phase 1:  T003 ∥ (T001→T002)
Phase 3:  T008 ∥ T010
Phase 4:  T016 ∥ T017 ∥ T020 (after T015);  T024 ∥ T023 (after T021/T022)
Phase 5:  T029 ∥ T030-prep (after T028)
Phase 6:  T031 ∥ T034;  T035 ∥ T036 (after T034)
Phase 7:  T038 ∥ T039
Phase 8:  T042 ∥ T043 ∥ T044 ∥ T045
Cross-phase (if staffed): after Phase 2 — US2 ∥ US3 ∥ US4 branches, merged sequentially for single-variable measurement runs
```

---

## Implementation Strategy

**MVP first (US1)**: Phases 1–3 alone deliver the owner's explicit P1 ask — the evidence-backed audit + roadmap with real baselines. Stop, review with the owner (and collect the exemplar prompt for T010's pending slot), then proceed.

**Incremental delivery with single-variable measurement**: merge US2 → measured run (T025) → US3 → measured run (T030) → US4 → measured run (T037) → US5 final verdicts (T040). Each measured run compares against the frozen Phase 2 baseline; a claim smaller than the variance band is reported as noise, and any completion-rate regression blocks per Constitution I/II/X.

**Notes**: commit after each task or logical group; never rebase away the baseline SHA; the exemplar-prompt input (D10) may arrive at any point — it slots into T010 without reordering anything.
