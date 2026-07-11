---

description: "Task list for feature 001-prompt-cache-optimization"
---

# Tasks: Prompt Cache Optimization

**Input**: Design documents from `specs/001-prompt-cache-optimization/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: MANDATORY for this feature — per the MuhiyaCode Constitution (Principles VI and X)
and the tasks-template constitutional exception, an improvement claim of this kind requires
before/after verification, measurement tasks, and prefix-stability check tasks. They are
included below and are not optional.

**Organization**: Tasks are grouped by user story. **Critical cross-story ordering rule**: the
Foundational phase ends with the baseline benchmark capture (T014); NO Phase 3+ behavior change
may land before T014 completes, or the before/after comparison (SC-005) is unrecoverable —
there is no way to re-measure the "before" arm honestly after behavior changes merge.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 (P1 cached-context reuse), US2 (P2 trustworthy accounting), US3 (P3
  reference-informed docs/limits)
- All file paths are repository-relative

## Phase 1: Setup

**Purpose**: Reproducibility and fixtures for everything that follows

- [X] T001 Initialize version control for baseline reproducibility: run `git init`, add a
      `.gitignore` for build artifacts, and create an initial commit of the unmodified tree at
      repository root (quickstart Prerequisites; enables a tagged `baseline` tree)
- [X] T002 [P] Create the benchmark workspace fixture — a small representative project with
      files to read/search/edit across ≥20 scripted turns — in `benchmarks/cachebench/fixture/`
      (data-model §7 BenchmarkScenario inputs)
- [X] T003 [P] Record the green starting gate: run `go mod verify`, `go vet ./...`,
      `go test ./... -count=1` and save the summary to
      `specs/001-prompt-cache-optimization/benchmarks/gate-start.txt` (quickstart V0;
      Constitution I baseline)

**Checkpoint**: Repo committed, fixture exists, existing tests green.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Honest measurement core + attribution primitives + benchmark harness + baseline
capture. Blocks ALL user stories (Constitution VI: measurement precedes optimization claims;
plan.md W1).

**⚠️ CRITICAL**: No user story implementation may begin before this phase completes — T014
(baseline) is the last gate.

- [X] T004 Extend the usage model with distinct nullable cache fields `CacheReadTokens`,
      `CacheMissTokens`, plus `MissDerived`, keeping the legacy single `CachedTokens` as a
      derived display value, in `internal/contract/types.go` (data-model §1; D5)
- [X] T005 Parse provider usage per the precedence rules — DeepSeek top-level
      `prompt_cache_hit_tokens`/`prompt_cache_miss_tokens` first, OpenAI nested
      `prompt_tokens_details.cached_tokens` with derived miss second, nulls otherwise; verbatim
      values, no clamping, malformed usage yields nulls + diagnostic without failing the
      request — in `internal/gateway/sse.go` (`mergeUsage`, currently lines ~147-171)
      (contracts/cache-metrics.md Parsing; depends T004)
- [X] T006 [P] Implement `UsageRecord` append-only persistence: JSONL writer/loader for
      `sessions/<id>/usage.jsonl` with all data-model §1 fields, writing a record even when
      usage is absent (`attribution=n/a`), in `internal/state/session.go`
- [X] T007 Emit one `UsageRecord` per request from the turn loop — seq, timestamp, model sent,
      parsed usage, `cold-start` attribution for the first request after start/resume — wired
      in `internal/orchestrator/engine.go` (depends T004, T005, T006)
- [X] T008 Rebuild `SessionUsageAggregate` by folding `usage.jsonl` on session load so
      cumulative billed/cached figures survive resume, in `internal/orchestrator/engine.go` and
      `internal/command/application.go` (data-model §2; fixes the audit's resume-reset gap;
      depends T006)
- [X] T009 [P] Create `PrefixShape` (hashes of exact system-message bytes, canonical tools
      bytes, history rewrite-version, model ID) and `CompareShape` returning per-region reasons,
      in new file `internal/orchestrator/prefixshape.go` (data-model §3; D6)
- [X] T010 [P] Create the `InvalidationEvent` ledger — taxonomy, trigger, scope, pressure,
      request_seq; append-only persistence with the session; survives resume — in new file
      `internal/orchestrator/invalidation.go` plus its carrier in `internal/state/session.go`
      (data-model §4; contracts/invalidation-events.md)
- [X] T011 Compute `PrefixShape` before every send, compare with the previous request, stamp
      `prefix_changed`/`change_reasons` into the `UsageRecord`, and implement the attribution
      algorithm (`n/a` → `cold-start` → `agent` → `provider`) exactly per
      contracts/invalidation-events.md, in `internal/orchestrator/engine.go` (depends T007,
      T009, T010)
- [X] T012 Usage-parsing unit tests: golden fixtures for both provider shapes, zero-vs-null
      distinction, derived-miss flagging, contradictory payload stored-as-reported, malformed
      payload → nulls, in `internal/gateway/sse_usage_test.go` (quickstart V3 / SC-004)
- [X] T013 Build the live benchmark harness: scripted ≥20-turn scenarios over the fixture,
      N-run repetition, JSON output per contracts/cache-metrics.md (per-request verbatim
      records, both hit rates, latency, derived cost + price source, `unattributed_misses`),
      plus a raw provider-payload log as ground truth, in `benchmarks/cachebench/main.go`
      (D8; depends T004–T011)
- [X] T014 Capture the BASELINE arm: 3 runs of all scenarios on the current (pre-behavior-
      change) build against the pinned concrete model, stored under
      `specs/001-prompt-cache-optimization/benchmarks/baseline/` (quickstart V4; FR-010).
      ⚠️ Gate: Phase 3 merges are forbidden until this lands. Requires a live gateway API key —
      if unavailable, STOP and get the user to provide/run it; do not fabricate a baseline

**Checkpoint**: Cache-read/miss measured honestly and persisted; shape+ledger primitives exist;
baseline evidence recorded. User stories may now proceed.

---

## Phase 3: User Story 1 — Long coding sessions reuse cached context (Priority: P1) 🎯 MVP

**Goal**: From each session's second request onward, previously transmitted stable context is
served from cache at 99–100%; dynamic content and unchanged files never invalidate or
retransmit it; every rewrite is rare, boundary-scheduled, and attributed.

**Independent Test**: quickstart V2 (mock-endpoint guard ≥90% tail-average, strict
prefix-of relationship between consecutive requests) + V5 (live: improved arm
`steady_state_hit_rate ≥ 0.99`, baseline below; variance ≤ 1pp; `unattributed_misses = 0`).

### Deterministic tool surface (research D1, D3; defects G1/G2/G5)

- [X] T015 [P] [US1] Implement the `ToolSurfaceSnapshot` persisted schema cache — entry per
      server-spec fingerprint (transport, command/URL, args, env with **sorted keys**),
      canonicalized tools sorted by namespaced name, `captured_at`, `spec_version` — as an
      additive store in `internal/state/` (new file `internal/state/toolcache.go`;
      data-model §5)
- [X] T016 [P] [US1] Add one-time schema canonicalization at registration and namespaced-name
      sorting for the MCP block (workspace tools and engine built-ins keep their existing fixed
      order) in `internal/orchestrator/registry.go` and `internal/mcpclient/manager.go`
      (contract W3)
- [X] T017 [US1] Pin the session tool surface: at session build, register the cached MCP
      surface *before the first request* as forwarding entries that route execution to live
      tools once handshakes complete; live handshake results update ONLY the disk cache (next
      session), never the in-session registry — in `internal/mcpclient/manager.go`
      (currently registers per-tool via `onTool` at lines ~136-160) and
      `internal/command/application.go` (build path ~485-486) (depends T015, T016)
- [X] T018 [US1] Enforce `/mcp` action rules: list/test never touch the registry;
      add/remove/enable/disable/authorize apply at a task boundary and record a
      `toolset-change` event; remove the blanket `RemovePrefix("mcp__")`+Refresh from read-only
      paths — in `internal/command/application.go` (~297-338, 609-622) (depends T010, T017)
- [X] T019 [US1] Handle first-ever (uncached) servers: absent a cache entry the server
      contributes nothing until its first handshake, which joins at a task boundary with one
      `toolset-change` event — in `internal/mcpclient/manager.go` (depends T017)
- [X] T020 [P] [US1] Persist the web-search `ProbeSnapshot` keyed by endpoint fingerprint with
      last-good merge (definitive results overwrite; transient failures preserve previous;
      first-run failure defaults `unsupported`), emitting `probe-change` events on definitive
      change — replace the in-process cache in `internal/command/application.go` (~538-560)
      with a store in `internal/state/` (data-model §6; D3)

### Stable prompt fields (research D2, D11; defects G3/G6)

- [X] T021 [P] [US1] Remove the `date:` line from the system prompt template and
      `PromptContext` (`internal/orchestrator/prompt.go` ~63-64,
      `internal/command/application.go` ~514-517) and add the current date to the per-task
      brief in `internal/orchestrator/classify.go` (`BudgetFor` brief, ~line 136) (contract W2)
- [X] T022 [P] [US1] On `/model`, refresh the prompt-context model fields and record a
      `model-switch` invalidation event at the task boundary, in
      `internal/command/application.go` (~196-206) and `internal/orchestrator/engine.go`
      (contract W12; depends T010)

### Rewrite scheduling (research D4; defect G4)

- [X] T023 [US1] Switch context-pressure inputs to the latest provider-reported
      `prompt_tokens` (estimator only as bootstrap before the first response, flagged so tests
      can assert the switchover) in `internal/orchestrator/history.go` (~line 80) and
      `internal/orchestrator/engine.go` (depends T007)
- [X] T024 [US1] Consolidate fold+trim into ONE boundary-scheduled maintenance pass with a
      0.60 usable-context floor (replacing fold-at-0.55-per-task at `engine.go` ~284 and
      per-turn TrimAged-above-0.65 at ~359-361), emitting exactly one `fold`/`trim` event with
      combined scope per pass, preserving tool-call/result pairing — in
      `internal/orchestrator/engine.go` and `internal/orchestrator/history.go` (~104-166)
      (contract W4; depends T010, T023)
- [X] T025 [US1] Add the anti-thrash latch: two consecutive pressure passes pause automatic
      rewrites until pressure recedes below the floor or compaction resolves; latch state
      visible in diagnostics — in `internal/orchestrator/history.go` (depends T024)
- [X] T026 [P] [US1] Record `window-drop` events when request assembly excludes previously
      transmitted units, in `internal/orchestrator/history.go` (`assembleRequest` ~213-235)
      (depends T010)
- [X] T027 [P] [US1] Record `compact` (auto) and `user-compact` (/compact) events with
      pressure and scope, in `internal/orchestrator/engine.go` (~747-750) (depends T010)

### Replay hygiene (research D9; contract W8–W11)

- [X] T028 [P] [US1] Verify assistant reasoning content is never replayed to the provider;
      where DeepSeek thinking mode requires the `reasoning_content` key on assistant
      `tool_calls` turns, emit the minimal required form only there — audit and conform
      `internal/gateway/provider.go` and history serialization in
      `internal/orchestrator/history.go` (contract W10)
- [X] T029 [P] [US1] Verify subagent request streams satisfy the same contract (own stable
      R1/R2, no main-session mutation, own usage records) and fix any deviation, in
      `internal/orchestrator/subagent.go` (~104-131) (contract W11; FR-014)

### Mandatory prefix-stability verification (Constitution III/X; quickstart V1/V2)

- [X] T030 [US1] Golden byte-stability tests: system prompt double-build identity; tools array
      identity across double-build and across randomized registration orders (MCP block);
      full-body marshal determinism for identical logical state — in
      `internal/orchestrator/prompt_stability_test.go` (extend) and new
      `internal/gateway/marshal_determinism_test.go` (contract W7; D10)
- [X] T031 [US1] Restart-determinism test: build session → send mock turns → persist → reload
      in a fresh process context → assert the next request's R1+R2+R3 bytes are identical
      (covers date, probe, MCP-pinning fixes; SC-002) — new
      `internal/orchestrator/restart_determinism_test.go` (contract W9; depends T017, T020,
      T021)
- [X] T032 [US1] Mock-endpoint cache-guard suite: OpenAI-compatible mock whose "hit tokens" =
      byte-identical common prefix with the previous request; scenarios per quickstart V2
      (dialogue, tool loops, MCP-pinned surface, restart replay, steering, goal continuation,
      pressure maintenance); assertions: tail-average ≥ 90%, strict prefix-of relationship
      absent maintenance events, zero PrefixShape diffs without a matching ledger event — new
      `internal/orchestrator/cachehit_guard_test.go` (D7; depends T011, T024)
- [X] T033 [P] [US1] Retry/reconnect byte-identity test: retried request marshals identically
      to the original attempt — in `internal/gateway/provider_retry_test.go` (contract W8)
- [ ] T034 [US1] Capture the IMPROVED arm (3 runs, same scenarios/model/effort/fixture as
      T014) to `specs/001-prompt-cache-optimization/benchmarks/improved/`, then produce the
      comparison report: SC-001 (≥0.99 steady-state on improved, baseline below), SC-003
      (steady-state uncached ≈ new tail), SC-005 (variance ≤ 1pp, total-cost delta stated),
      SC-007 (`unattributed_misses = 0`) — store the report alongside the runs (quickstart V5;
      depends ALL prior US1 tasks)

**Checkpoint**: US1 independently proven — offline guard green, live before/after evidence
recorded, every miss attributed.

---

## Phase 4: User Story 2 — Trustworthy cache and cost accounting (Priority: P2)

**Goal**: Users see per-request and session cache-read / cache-write / uncached / output
figures that match provider payloads exactly; absent reporting renders as "unavailable";
prefix changes are annotated with their cause.

**Independent Test**: quickstart V3 (parsing honesty) + V6 items 4–5 + SC-004 cross-check
(agent-reported == provider-reported on 100% of benchmarked requests; "unavailable" never
rendered as zero).

**Note**: The measurement core landed in Phase 2 (T004–T012); this phase is the user-facing
surfacing. Only T037 depends on a US1 task (event recording sites, T024–T027); the rest can
run in parallel with US1.

- [X] T035 [US2] Extend `/context` output: session totals (prompt/output), cache split
      (read/uncached), session + steady-state hit rates, unavailable-request count when > 0,
      and the most recent invalidation events (cause, scope, when) — in
      `internal/tui/actions.go` (~44-47) (contracts/cache-metrics.md Display; depends T008,
      T011)
- [X] T036 [P] [US2] Add the compact per-request cache tag to the activity line (e.g.,
      `cache 99% (12.3k read / 128 new)`) and billed-vs-cached figures to task summaries — in
      `internal/tui/view.go` (~117-118, ~576) (depends T007)
- [X] T037 [US2] Annotate the usage/notice line with prefix-change causes
      (`cache prefix changed: tools (mcp add supabase)`) sourced from the event ledger — in
      `internal/tui/view.go` and the engine's task-complete notices in
      `internal/orchestrator/engine.go` (~267-277) (depends T011, T024–T027 for real causes)
- [X] T038 [P] [US2] Display-honesty tests: null renders "unavailable" (never `0`), zero
      renders `0`, derived misses labeled, no fabricated figures on cache-less endpoints
      (FR-013) — in `internal/tui/actions_test.go` (depends T035)
- [X] T039 [US2] SC-004 cross-check test: drive the mock endpoint with injected usage payloads
      (both provider shapes + absent + malformed) through the full engine loop and assert
      recorded/displayed figures equal the injected payloads exactly — in
      `internal/orchestrator/usage_integrity_test.go` (depends T011, T035)
- [X] T040 [US2] Resume-continuity test: run turns → restart → assert `/context` cumulative
      figures equal the pre-restart sums from `usage.jsonl` — in
      `internal/orchestrator/usage_resume_test.go` (depends T008, T035)

**Checkpoint**: Accounting is visibly trustworthy and provider-faithful, independent of US1's
behavior fixes.

---

## Phase 5: User Story 3 — Reference-informed design and documented limits (Priority: P3)

**Goal**: Durable, evidence-backed documentation: why the design is what it is (Reasonix
matrix kept true), and why any shortfall from 100% is a documented limitation rather than a
defect.

**Independent Test**: US3 acceptance scenarios — every cache-relevant reference mechanism has
an adopt/adapt/reject entry reflecting what was actually built; every benchmark miss maps to a
register entry or a filed defect (SC-007 audit trail).

- [ ] T041 [US3] Write `docs/prompt-caching.md`: user-facing cache guide (how caching works,
      how to read `/context` and the activity tag, model-pinning guidance) + the provider
      limitations register seeded from research.md Part E and populated with observed evidence
      from `specs/001-prompt-cache-optimization/benchmarks/` (cold start, TTL/eviction,
      64-token block granularity, per-model scoping, router fragmentation, reporting variance,
      thinking-mode replay) (FR-011; D12; depends T014, T034)
- [ ] T042 [P] [US3] Reconcile existing docs with implemented behavior: README.md
      "Prefix caching" + "Agent and token design" sections; `docs/agent-design.md` (stale
      "separate sub-180-token prompt" claim vs audited single stable prompt — verify against
      code and fix whichever is stale); `docs/architecture.md` invariants #5/#6 (prefix
      stability strengthens from "within a task" to session-scoped-with-attributable-events)
      (Constitution workflow gate)
- [ ] T043 [US3] Update `specs/001-prompt-cache-optimization/research.md` Part C with any
      implementation-time deviations from decisions D1–D12, and file defects for any
      benchmark miss that attribution could not explain (must be zero per SC-007; depends
      T034)

**Checkpoint**: Knowledge is durable; a maintainer can answer "why not 100%?" with evidence.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T044 Full gate run per quickstart V0: `go mod verify`, `go fmt ./...` diff-clean,
      `go vet ./...`, `go test ./... -count=1` at repository root; on a CGO-capable runner
      also `CGO_ENABLED=1 go test -race ./... -count=1` (Constitution IX; SC-006)
- [ ] T045 [P] Execute the quickstart V6 live UX spot-check list (MCP session, `/mcp`
      mid-session, cross-day resume, `/compact`, cache-less endpoint, external file edit) and
      record outcomes in `specs/001-prompt-cache-optimization/benchmarks/v6-spotchecks.md`
- [ ] T046 Verify the system-prompt regression ceiling (~1,900 estimated tokens) and the
      conversational-prompt budget still hold after prompt changes, adjusting ceiling tests
      only with justification, in `internal/orchestrator/prompt_stability_test.go`
      (Constitution II — no quality-affecting prompt growth/shrinkage snuck in)
- [ ] T047 Final sign-off sweep: walk quickstart V0–V7 end-to-end; confirm SC-001…SC-007 each
      have recorded evidence under `specs/001-prompt-cache-optimization/benchmarks/`; update
      `specs/001-prompt-cache-optimization/checklists/requirements.md` notes with completion
      state

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup. **T014 (baseline) is a hard gate**: it must
  complete before any Phase 3 behavior change merges (unrecoverable otherwise)
- **US1 (Phase 3)**: depends on Foundational. Internal order: tool surface (T015→T017→T018/T019)
  and prompt fields (T021, T022) and scheduling (T023→T024→T025) can proceed as three parallel
  tracks; verification tasks (T030–T033) follow their subjects; T034 is last
- **US2 (Phase 4)**: depends on Foundational only (T035, T036, T038–T040 can run parallel to
  US1; T037 needs US1's event-recording sites T024–T027)
- **US3 (Phase 5)**: T041/T043 depend on benchmark evidence (T014 + T034); T042 can start
  once US1's behavior is settled
- **Polish (Phase 6)**: depends on all desired stories

### Story completion order

1. US1 (MVP) — delivers the hit-rate outcome and its proof
2. US2 — surfaces the accounting (largely parallelizable with US1)
3. US3 — documentation hardening on top of recorded evidence

### Parallel opportunities

```text
Phase 2: T006 ∥ T009 ∥ T010 (different new files) after T004/T005
Phase 3, three tracks after T014:
  Track A (tools):  T015 ∥ T016 → T017 → T018, T019; T020 alongside
  Track B (prompt): T021 ∥ T022
  Track C (sched):  T023 → T024 → T025; T026 ∥ T027 alongside
  Hygiene:          T028 ∥ T029 anytime
  Verification:     T030 ∥ T033 early; T031 after A+B; T032 after C; T034 last
Phase 4 (mostly parallel with Phase 3): T036 ∥ T038 after their deps; T035 → T039/T040
Cross-story: US2 tasks T035/T036/T038–T040 ∥ US1 Track A/B/C (disjoint files)
```

## Implementation Strategy

**MVP first (US1 + its Foundational base)**: Phases 1–2, then Phase 3 complete, delivers the
entire P1 outcome with offline + live proof (T032/T034). Stop-and-validate point: quickstart
V2 + V5 acceptance.

**Incremental delivery**: US2 can ship its display work in the same window as US1 (disjoint
files) but is independently valuable even if US1 slipped — honest metrics on the unmodified
agent is exactly the baseline story. US3 lands last by design: its register must cite real
benchmark evidence, not predictions.

**Constitutional guardrails while executing**: any rewrite of previously transmitted bytes
without a ledger event is a defect (contract rule 1); any tuning of D4 thresholds stays within
research.md bounds and is re-verified by T032 + T034; no task may trade answer quality for hit
rate (Principles I–II) — SC-006 failures block merge regardless of cache gains.
