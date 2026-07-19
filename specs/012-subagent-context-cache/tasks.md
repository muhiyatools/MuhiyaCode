# Tasks: Subagent Context Reuse & Cache-First Orchestration

**Feature**: `012-subagent-context-cache` · **Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Ordering law (Constitution X)**: T001–T003 capture baselines on the UNCHANGED build and T004–T006 settle provider truth BEFORE any behavior-change task merges. Tasks marked **[GATED: live cost]** spend real provider money and run only with the owner's explicit go-ahead; all other tasks are free (unit/integration tests use mock providers).

**Cache-epoch discipline**: T010+T011+T024 form ONE reviewed cache epoch (goldens regenerate once, together — `-update` / `-update-prefix-golden`). No other task may touch prefix bytes.

## Status ledger

Update the checkbox AND this ledger as tasks complete.

| Phase | Tasks | Done |
|---|---|---|
| 1 Setup & baselines | T001–T003 | 2/3 |
| 2 Foundational | T004–T013 | 7/10 |
| 3 US1 continuation linking (MVP) | T014–T021 | 8/8 |
| 4 US2 role separation | T022–T025 | 4/4 |
| 5 US3 provider-aware linking | T026–T029 | 4/4 |
| 6 US4 lean handoffs | T030–T032 | 2/3 |
| 7 US5 visibility | T033–T035 | 3/3 |
| 8 Polish & acceptance | T036–T041 | 3/6 |

**Implemented 2026-07-19 (33/41)** — all free code tasks landed, full suite green (build, vet, every package, arch guards, wiring inventory, fault injection; goldens regenerated once in the reviewed T010/T011/T024 epoch). The 8 open tasks and honest caveats:

- **T003/T004/T005/T006/T039 [GATED: live cost]** — pending owner go-ahead. Probe script ready (`scripts/probe_012_continuation.ps1`, `-EffortFlip` covers P3); P2 additionally needs the owner's production providers/models query. Baseline binary frozen (`bin/muhiyacode-baseline-012.exe`, SHA e163506).
- **T032 open** — handoff/return sizes ARE ledgered per dispatch (LinkOutcome.OutboundChars/ReturnChars → bench `links[]`; SC-006 computable from records); the dedicated bound-assertion + PH-2 prohibited-content guard test is deferred and tracked here.
- **T040/T041** — depend on probe/baseline outcomes; `results.md` skeleton records everything measurable offline.
- Deviations from task text, recorded: T007's shape tests live in `contextlink_test.go` (TestTerminalShapeFor) not `subagent_ceiling_test.go`; T009 store methods are dedicated (`WriteAgentRecord`/`ReadAgentRecords` with their own id validation + compact encoding) rather than extending the flat-file allowlist regex — compact encoding fixed a real replay-breaking re-indentation bug the round-trip test caught; T011 lives in `internal/orchestrator/readplan.go` (harness-served, engine-registered) not `workspace/registry.go`; T012's restart-double-delivery guard is by-construction (planning-start prelude is a different code path) with the single-Run regression tested; T024 put the advance notice in the approval gate text (renders BEFORE implementation — clause-a-correct) instead of growing the implementing prelude; T027 effort pinning is code-complete with the P3 probe deciding whether it stays; T037 is behavioral equivalence (off → fresh dispatches, zero records/links) — byte-golden comparison against the baseline binary is a two-build exercise scoped to the gated acceptance run.

---

## Phase 1: Setup & Baselines

- [X] T001 Add multi-phase continuation fixtures to the benchmark suite in `specs/012-subagent-context-cache/benchmarks/fixtures/`: (a) a two-sequential-implement-phase task over `wslogic`, (b) a self-edit chain task (phase 1 edits ≥10 files it read), (c) an external-edit variant (harness touches 6 of 10 between phases), (d) a review-after-implement full-depth task, (e) a related follow-up-task pair. Reuse the 011 fixture format (`tasks.json` + workspaces); document each fixture's expected link outcome in `fixtures/README.md`.
- [X] T002 Freeze the pre-012 baseline: record `git rev-parse HEAD` in `specs/012-subagent-context-cache/benchmarks/BASELINE_SHA.txt`, build `bin/muhiyacode-baseline-012.exe` from it, and verify the full suite is green at that SHA (`go test ./... -count=1`).
- [ ] T003 **[GATED: live cost]** Run the baseline benchmark on the UNCHANGED build over the 011 multiphase subset + T001 fixtures with `MUHIYA_BENCH_JSON=1` (auto-accept mode) via `scripts/bench_011.ps1`, writing records to `specs/012-subagent-context-cache/benchmarks/runs/baseline/`. These are the SC-001/002/004/005/006 denominators.

## Phase 2: Foundational (blocking prerequisites)

**Provider truth probes** (research.md R-D12 — settle before SC commitments; results in `specs/012-subagent-context-cache/benchmarks/probes/`):

- [ ] T004 **[GATED: live cost]** P1 DeepSeek continuation probe: script `scripts/probe_012_continuation.ps1` runs a tiny live subagent task, then re-sends its transcript + one appended user message on the same pin; compare the continuation's `prompt_cache_hit_tokens` against the predecessor's last `prompt_tokens`. Record whether the final assistant turn hits (end-of-output boundary, R-F22) and adjust the SC-001 expectation note in `spec.md` if needed.
- [ ] T005 **[GATED: live cost + owner query]** P2 MiniMax routing truth: owner queries the production gateway `providers`/`models` tables (direct api.minimax.io vs OpenRouter); then run the P1-equivalent probe on the real MiniMax path. Record the routing answer and cached_tokens behavior; if OpenRouter-routed and zero-cache, re-scope SC-005's 70% target in `spec.md` per research.md open item 1.
- [ ] T006 **[GATED: live cost]** P3 effort-flip probe: identical DeepSeek resend with only `reasoning_effort` flipped; record whether the prefix colds. Outcome sets `EffortPinned` in T026.

**Free foundational code** (mock-tested, no live cost):

- [X] T007 [P] Terminal-shape marker (research.md R-D4, fixes R-F16): in `internal/orchestrator/subagent.go` stamp `terminalShape` (`clean-done`/`wrapup-done`/`partial-ceiling`/`failed`/`cancelled`) distinguishing turn-exhausted wrap-up reports from clean completions; add the field additively to the `run_summary` event JSON; tests in `internal/orchestrator/subagent_ceiling_test.go` cover all five shapes.
- [X] T008 [P] Per-run read/write recorder with end-of-run fingerprints (R-D5, data-model ReadWriteSet): add a `readset` recorder flag to `dispatchScope` in `internal/orchestrator/dispatch.go`; record reads (path+line ranges) and mutations per run in `internal/orchestrator/subagent.go`; one `os.Stat` pass at run completion stores final mtime+size fingerprints; path normalization = inspection-ledger convention (workspace-relative, slash-normalized, Windows-lowercased). Unit tests: self-edit run yields changed-fraction 0 against its own record.
- [X] T009 SubagentContextRecord persistence (R-D3, data-model): new `internal/orchestrator/contextrecord.go` + store glue in `internal/state/session.go` — write `~/.muhiya/sessions/<id>/agents/<runID>.json` (verbatim `[]contract.Message` with raw `ReasoningDetails`, model, pin, kind, terminal shape, final usage, ReadWriteSet, result, lineage); extend the sidecar allowlist regex additively; save-time validation (complete tool pairings per R-F8 → else `linkable=false` + reason); per-session count+bytes cap evicting oldest-completed. Round-trip byte-fidelity tests (marshal→write→read→marshal identical), including a MiniMax `reasoning_details` raw payload.
- [X] T010 Handoff relocation — cache epoch part 1 (R-D6, contracts/phase-handoff.md PH-1): in `internal/orchestrator/subagent.go` + `internal/instructions/subagents.go`, make the subagent system message per-kind-per-session stable (kind identity + spec.System + workspace + capability statement) and move the rendered HANDOFF CONTRACT into the first user message joined with the task; regenerate handoff/instruction/wiring goldens once; new PH-6 golden asserts two sequential dispatches of one kind produce byte-identical system messages and tool arrays.
- [X] T011 `read_plan` registry tool — cache epoch part 2 (R-D7, PH-3): read-only tool in `internal/workspace/registry.go` (or orchestrator-served equivalent) returning the rendered plan (whole or named phase section) from the session store — no filesystem path, no outside-workspace prompt; add to all four kind allowlists in `internal/orchestrator/subagent.go`; tests: sections render, absent plan → guided empty result, main loop unaffected.
- [X] T012 [P] Planning-briefing delivery fix (R-D11, PH-5, fixes verified gap R-F15): in `internal/orchestrator/phaserunners.go` append the findings briefing (existing 4000-char bound) once to the research→planning mid-Run status line; regression test proves single-Run flow delivers findings exactly once and restart flow doesn't double-deliver.
- [X] T013 [P] `Settings.ContextLinking` config key (`default`/`off`) in `internal/contract/types.go` + `internal/state/config.go`; add the row to `specs/010-ultimate-consolidation/wiring-inventory-data.md`; config round-trip tests.

**Checkpoint**: full suite green; wire bytes for non-orchestrated flows unchanged except the reviewed T010/T011 epoch.

## Phase 3: User Story 1 — Continuation subagent inherits its predecessor's context (P1) 🎯 MVP

**Goal**: a phase-2 subagent extends the phase-1 conversation; provider bills the shared prefix as cache reads; still-current files are never re-read.
**Independent test**: fixture (a) links `continued` with verified cache share; fixture (b) links with changed-fraction 0; fixture (c) falls back `digest-seeded / stale:60%`.

- [X] T014 [P] [US1] Link/ledger types (data-model ContextLink, DelegationLedger, LinkOutcome): `internal/contract/types.go` additions + `TaskStats.Links []LinkOutcome`; JSON shapes tested.
- [X] T015 [US1] The linker — new `internal/orchestrator/contextlink.go`: CL-1 eight-criterion decision procedure with machine-readable reason codes, relatedness predicate (R-D9: term-vs-touched-set overlap via the existing BriefingForScope extraction), staleness computation (changed-fraction vs end-of-run fingerprints, >0.5 declines), window predicate (R-D10: finalPromptTokens + handoff estimate + 20% headroom vs ModelProfile window). Table-driven test per criterion producing its reason code (contracts/context-linking.md CL-6).
- [X] T016 [US1] Continuation replay assembly (CL-2): in `internal/orchestrator/subagent.go` + `contextlink.go`, build the request as predecessor transcript verbatim + one appended handoff user message; reuse system message, tool array, model, pin byte-identically; extend the subagent PrefixShape guard across the boundary — first continuation shape must equal predecessor's final shape + appended message, mismatch aborts to digest fallback with reason `replay-drift`. Replay byte-identity goldens per provider family (extend `internal/gateway/marshal_determinism_test.go` pattern).
- [X] T017 [US1] Dispatch-path integration in `internal/orchestrator/subagent.go`: consult the linker before every dispatch (harness sites and model-issued `run_subagent` alike), route continued/digest/fresh, exempt `rereadDirectives` paths from the duplicate-read block for that run, stamp `verification` from the first provider response's paired cache fields, append the ContextLink to the DelegationLedger; emit the notice line (`link: continued (same-kind) — …`).
- [X] T018 [P] [US1] Digest fallback (CL-3): carryForward assembled from the predecessor record's result digest + banked-knowledge pointer + touched-file list with changed markers, within existing briefing bounds; never rendered as "linked"; unit tests.
- [X] T019 [US1] Same-task phase lineage in `internal/orchestrator/phaserunners.go`: sequential implement groups and phases record lineage so each dispatch's candidate is the prior `clean-done` implementer; parallel groups form parallel chains (R-F16 — no cross-claim; concurrent claims serialize by dispatch order per CL-5).
- [X] T020 [US1] Follow-up-task linking in `internal/orchestrator/turnloop.go`: at task start, candidate = session's most recent terminal-task chain passing R-D9; link decision recorded even when declined (`relatedness-miss`).
- [X] T021 [US1] Scenario tests S1+S2 (quickstart §2) as fixture-driven integration tests with a mock provider in `internal/orchestrator/contextlink_integration_test.go`: continued link with injected cache usage, self-edit chain changed-fraction 0, external-edit 60% → `digest-seeded / stale:60%`.

**Checkpoint**: US1 fully testable offline — the MVP increment.

## Phase 4: User Story 2 — Main model orchestrates; subagents implement (P2)

**Goal**: full-depth implementation reads belong to subagents; handoffs are plan-references; the gate yields honestly.
**Independent test**: quickstart S4 — deny×2, waive 3rd with recorded degradation, shell-cat denied, grep passes, failure opens the gate, light-depth never gates.

- [X] T022 [US2] The role gate (contracts/role-gate.md): new gate in `internal/orchestrator/gates.go` chain implementing RG-1 predicate (orchestrated ∧ full ∧ implementing ∧ ¬degraded ∧ ¬post-failure), RG-2 surface (`read_file` + terminal-read shell regex reuse from `internal/orchestrator/reviewgate.go`; grep/search/glob/list/git/read_plan ungated), RG-3 bounds (≤2 denials then `read-gate-waived` degradation), RG-4 post-failure exemption flag set by the first non-succeeded dispatch of the phase; denial text registered in `internal/instructions/gates.go` (Sidecar class); telemetry codes `phase-read-block`/`-waived`/`-exempt:*`; `TaskStats.ReadGate` counters. Predicate-table unit tests + fault-injection recovery invariant stays green.
- [X] T023 [P] [US2] Phase-enriched handoffs (PH-2): implement/review dispatch tasks in `internal/orchestrator/phaserunners.go` gain `phaseRef` (step titles + plan-Note Verification/Risks digest, bounded) and the prohibited-content guard (assembly rejects embedded file bodies); guard test with poisoned carryForward.
- [X] T024 [US2] Prompt alignment — cache epoch part 3 (RG-5): full-depth implementing prelude in `internal/instructions/pipeline.go` states the read rule in advance; delegation section in `internal/instructions/prompt.go` updated; prompt budget re-baselined and goldens regenerated within the same epoch as T010/T011.
- [X] T025 [US2] Scenario test S4 in `internal/orchestrator/reviewgate_test.go` or new `rolegate_test.go`: full RG-6 matrix (bounds, shell bypass, exemptions, light-depth immunity, telemetry landing in TaskStats.ReadGate).

## Phase 5: User Story 3 — Provider-aware cache discipline (P3)

**Goal**: linking obeys each provider's real cache rules; displayed figures are provider-reported or unavailable.
**Independent test**: capability-profile unit tests + S6's linked-but-cold rendering under injected zero-cache usage.

- [X] T026 [P] [US3] ProviderCacheProfile extensions in `internal/gateway/model.go` (data-model table): `ContinuationLinking` (deepseek/minimax supported; glm/generic digest-only), `EffortPinned` (per T006 outcome), prefix-identity notes as comments anchored to R-F19/R-F20; `internal/gateway/model_capability_test.go` rows for every family including generic degradation.
- [X] T027 [P] [US3] Effort pinning for continuations in `internal/orchestrator/contextlink.go` + `subagent.go`: when the profile says `EffortPinned`, the successor inherits the predecessor's reasoning tier (recorded in the link); test that an effort change mid-chain either pins or declines per profile.
- [X] T028 [US3] Verification honesty (CL-4): `pairedCacheReadShare` computed only from paired provider-reported fields; `reported=false` → "unavailable"; `continued` with share <20% on a reporting provider raises `linked but cold (provider cache miss)` with attribution; injected-usage tests for all three renderings.
- [X] T029 [US3] Pairing integration (R-F12): continuation requests join the predecessor's (model, pin) pairing in `internal/contract/cache.go` PerPairingRates — no new cold-write exclusion; extend `internal/contract/pairing_test.go` with a continuation sequence asserting the warm bucket is reused.

## Phase 6: User Story 4 — Lean handoffs and structured returns (P4)

**Goal**: bounded two-way communication; overhead ≤10% of task tokens.
**Independent test**: quickstart S5 on fixture (a) — bounds hold, no file bodies, overhead within target.

- [X] T030 [P] [US4] Structured return trailer (PH-4): subagent report gains lenient `Changed:`/`Verified:`/`CarryForward:` trailer parsing in `internal/orchestrator/subagent.go`; absence degrades to prose handling; parser unit tests.
- [X] T031 [US4] Overhead accounting: DelegationLedger records outbound/return sizes per dispatch in `internal/orchestrator/contextlink.go`; SC-006 share computed into `TaskStats`; unit test with synthetic dispatches.
- [ ] T032 [US4] Scenario test S5 in `internal/orchestrator/handoff_test.go`: handoff bound, `phaseRef` presence, prohibited-content rejection, overhead computation.

## Phase 7: User Story 5 — The user can see reuse working (P5)

**Goal**: link decisions, reasons, and cache shares visible per dispatch; nothing fabricated.
**Independent test**: quickstart S6 — every dispatch has a reasoned `links[]` row; digest never renders as linked; unavailable renders honestly.

- [X] T033 [P] [US5] Task summary + TUI: render `TaskStats.Links` lines (link form, reason, verified share or "unavailable") and `ReadGate` counters in `internal/tui/format.go` task summary; declined links show their reason (US5-AS2).
- [X] T034 [P] [US5] Bench JSON: `links` array + per-pin spend attribution in `internal/command/benchjson.go` (retire the feature-011 T032 upper-bound placeholder using per-pin UsageRecord aggregation); schema documented in `specs/012-subagent-context-cache/contracts/context-linking.md` CL-4 note; unit test on synthetic stats.
- [X] T035 [US5] Scenario test S6: fixture run asserts 100% dispatch → link-row coverage (SC-008) and the three honesty renderings.

## Phase 8: Polish & Acceptance

- [X] T036 [P] Doc sync (R-D14 + Constitution workflow gate): correct the pin taxonomy in `docs/prompt-caching.md` (no `:aux` wire pin; per-kind `:sub:<kind>`; compaction wire/ledger divergence documented); update `README.md`, `docs/agent-design.md`, `docs/architecture.md` for context linking, handoff relocation, read gate, and the new sidecar family.
- [X] T037 [P] Legacy degradation test S7: `ContextLinking=off` and a pre-012 session dir (no `agents/` sidecars) reproduce baseline wire bytes (golden comparison) and behavior; one-shot mode unaffected; test in `internal/orchestrator/` + `internal/command/`.
- [X] T038 Full gate: `gofmt`, `go vet`, `go test ./... -count=1`, race-enabled run, arch guards (800-line budget — split `contextlink_store.go` if needed), dead-code budget, wiring inventory green.
- [ ] T039 **[GATED: live cost]** After-run: same benchmark suite as T003 on the finished build → `benchmarks/runs/after/`; generate the comparison with `scripts/bench_011_report.ps1` (baseline vs after, single variable = the feature).
- [ ] T040 Reconcile spec/plan with probe outcomes: SC-001/SC-005 numbers confirmed or re-scoped per T004/T005 records; "MiniMax through OpenRouter" wording adjusted if P2 shows direct routing (research.md open item 1).
- [ ] T041 `specs/012-subagent-context-cache/results.md`: honest before/after evidence per Constitution VI/X — all eight SC verdicts with provider-reported figures, cold vs steady separated, quality parity (SC-007) stated, any regression called out.

---

## Dependencies

```text
T001 ─► T002 ─► T003 (baseline law: blocks merging T007+)
T004,T005,T006 (probes) ─► T026/T027 parameters, T040 wording   [cost-gated; code may proceed on conservative defaults, merge blocked on T003 only]
T007,T008 ─► T009 ─► T014 ─► T015 ─► T016 ─► T017 ─► T019,T020 ─► T021
T010 ─► T011 ─► T024 (one cache epoch, sequential)
T012, T013 independent after T003
US2 (T022–T025) needs T012 (briefing fix) + T013 (kill switch)
US3 (T026–T029) needs T017 (links exist to verify)
US4 (T030–T032) needs T010 (new handoff shape)
US5 (T033–T035) needs T014 (types) + T017 (outcomes recorded)
Phase 8 last; T039 needs T038 green; T041 needs T039.
```

**Story independence**: US1 is complete alone (MVP). US2 depends only on Foundational. US3/US4/US5 each layer on US1's types but are separately testable and shippable.

## Parallel opportunities

- Phase 2: T007 ∥ T008 ∥ T012 ∥ T013 (disjoint files); T009 after T007/T008; the T010→T011→T024 epoch runs as its own serial lane.
- Phase 3: T014 ∥ T018 once T009 lands; T015→T016→T017 serial core.
- Phases 5–7 largely parallel across stories after T017: T026 ∥ T027 ∥ T030 ∥ T033 ∥ T034 ∥ T036 ∥ T037.
- Probes T004–T006 can run any time the owner approves, independent of code lanes.

## Implementation strategy

**MVP first**: Phases 1–3 (baselines + foundational + US1) deliver the headline value — linked continuations with verified cache reuse — behind the `ContextLinking` kill switch. Ship, measure S1/S2 offline, then layer US2 (the gate), US3 (provider honesty), US4/US5 (bounds + visibility), and close with the gated live acceptance run. Every phase ends on a green full suite; behavior stays baseline-identical until T017 lands, and even then `off` restores today's behavior exactly (FR-017).
