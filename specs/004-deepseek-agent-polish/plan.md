# Implementation Plan: Coding Agent Quality Polish & DeepSeek V4 Optimization

**Branch**: `004-deepseek-agent-polish` | **Date**: 2026-07-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/004-deepseek-agent-polish/spec.md`

## Summary

A launch-hardening polish of the MuhiyaCode agent harness for the DeepSeek-V4-only production configuration, in seven wired-together strands: (1) an explicit **plan lifecycle phase machine** (`drafting → ready → pending/executing → finished | interrupted | superseded | discarded`) persisted in the existing `plan_state.json` sidecar, with step-progress-driven execution detection replacing the anchored-regex-only path that leaves completed plans advertising "say 'proceed'" forever ([research D1](research.md)); (2) a two-layer **terminal-state guarantee** — engine-emitted tool/agent end events on every path plus a TUI task-end sweep marking stuck "running" items "cancelled" (D2); (3) **mode awareness** delivered on the constitutionally-correct per-turn surfaces (subagent capability statements, aligned plan/task-brief riders) plus five precise enforcement fixes at the existing dispatch gate, including MCP `readOnlyHint` plumbing so plan mode stops over-blocking read-only MCP tools (D3); (4) a **prompt tightening pass** — dedup, one explicit priority rule, a one-sentence completion-honesty addendum — at net ≤ current ~1,900-token size, shipped with a tool-description quality pass as a single one-time prefix version bump (D4/D10); (5) **DeepSeek wire robustness**: live verification of `reasoning_content` replay semantics (with a documented contingency), tool-call salvage hardening against text-emitted calls, and a bounded retry class for zero-token empty completions (D5); (6) a **completion audit** in `finalize` reconciling the final answer against recorded plan/step/check state, appending harness-attributed disclosures instead of trusting model self-report (D6); (7) the small **tool-row target polish** completing 003's display contract — `apply_patch` path extraction and filename-preserving middle truncation (D7). Everything is evidence-gated: fresh baselines are captured *before* any change because prefix-stability and cost baselines do not exist (D9), the harness audit is a versioned artifact with 100% disposition (D8), and docs sync in the same change (D11).

## Technical Context

**Language/Version**: Go 1.25.0 ([go.mod](../../go.mod)); single repo — no gateway-repo changes this feature (the MuhiyaLLM gateway is consumed as-is; D5's replay probe is a client-side verification).

**Primary Dependencies**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2` (TUI); stdlib `net/http` + SSE parsing in `internal/gateway`. **No new dependencies** — every decision rides existing structures (research D1–D11); the one new plumbing item (MCP `readOnlyHint`) uses the existing MCP `tools/list` payload already fetched by `internal/mcpclient`.

**Storage**: `~/.muhiya` state layout, unchanged shape; `plan_state.json` gains one additive field (`phase`, string) with documented legacy derivation — no migration required, old snapshots remain readable (constitution VIII compatibility). `plan.md`/`tasks.md` session artifacts unchanged.

**Testing**: `go fmt` / `go vet` / `go test ./... -count=1` (constitution IX). New scripted suites codifying the spec's acceptance scenarios: plan-lifecycle transitions incl. save/resume (US2/SC-001), mode-scenario gate behavior (US3/SC-002), terminal-state reconciliation (US1/SC-004), completion-audit reconciliation (FR-017), tool-row target rendering (US5/SC-007, existing `stripANSI` substring conventions). Extended prefix-stability fixtures for the one-time D4/D10 prompt+description bump (`prompt_stability_test.go`, `restart_determinism_test.go`, `cachehit_guard_test.go`). Live before/after `benchmarks/cachebench` runs per D9 (constitution X).

**Target Platform**: Windows/macOS/Linux terminals (TUI) + headless one-shot (`root.go`) — both plan-hint surfaces and both completion paths are in scope.

**Project Type**: Terminal application (agent orchestrator + TUI) against an OpenAI-compatible gateway; launch configuration is DeepSeek-V4 (`deepseek-v4-pro`/`-flash`) via MuhiyaLLM exclusively, with graceful degradation on other providers preserved (constitution IX).

**Performance Goals**: SC-003 ≥50% reduction in invalid and redundant tool calls per benchmark session vs fresh baseline; SC-005 median tokens/cost per completed task ≤ baseline with cache efficiency ≥ baseline (steady-state hit-rate within 1.0 pp variance band); prefix stability ≥99% every run (001 SC-001 gate; 002's ≥99.5% target recorded alongside); SC-008 ≥95% of benchmark tasks reach terminal state without manual rescue.

**Constraints**: Constitution I–X. Byte-stable prefix: exactly **one** planned prefix-byte change for the whole feature (D4 prompt tightening + D10 description pass, bundled as one version bump with updated fixtures and live validation); everything else must be prefix-inert. System prompt net size ≤ current (~1,900 est. tokens); per-turn system-added tail stays ≤ ~50 tokens on plain follow-ups (002 SC-004). No per-mode schema filtering. No new orchestration subsystems. DeepSeek quirk handling must not hard-depend on DeepSeek (`tool_choice` auto-only, salvage, empty-completion retry all degrade gracefully).

**Scale/Scope**: ~16 files across `internal/orchestrator` (engine, plan, goal, classify, subagent, registry, prompt, inspection), `internal/contract` (types), `internal/state` (session), `internal/tui` (view, model, actions), `internal/gateway` (provider, model), `internal/mcpclient` (manager), `internal/command` (root, application); 5 new/extended test suites; benchmark evidence + audit artifact under `specs/004-deepseek-agent-polish/`; 4 docs updated.

## Constitution Check

*GATE: evaluated pre-Phase 0 and re-checked post-Phase 1 design — PASS (no violations requiring Complexity Tracking).*

| # | Principle | Gate result | Evidence / design hook |
|---|---|---|---|
| I | Correctness before optimization | PASS | The feature is correctness-first by construction (truthful plan state, terminal guarantees, honest completion). No optimization removes or delays model-needed content; D5 salvage/retry paths recover correctness, never trade it. LLM-judge and NLU-classification alternatives rejected on determinism grounds (D1d, D6a). |
| II | Cache efficiency without quality loss | PASS (guarded) | One planned prefix change (D4+D10 bundle): content *moved/deduplicated*, one sentence added, net size ≤ current — the what-moved-and-why statement lives in D4; validated by D9 live before/after with hit-rate ≥ baseline gate. All other work is prefix-inert (riders, gate feedback, TUI, sidecar). |
| III | Deterministic stable prefix | PASS (guarded) | No per-request prefix variance introduced anywhere; per-mode schema filtering explicitly rejected (D3 alt-a). Subagent capability statements are per-mission messages, not prefix. The D4/D10 bump updates stability fixtures in the same commit; `prompt_stability_test.go`/`restart_determinism_test.go`/`cachehit_guard_test.go` must stay green; `tool_choice` pinned `auto` remains a tested invariant ([cachehit_guard_test.go:200](../../internal/orchestrator/cachehit_guard_test.go)). |
| IV | Dynamic/cached separation | PASS | Every new dynamic element rides the correct surface: plan phase in the sidecar + TUI (never the prompt); capability statements on mission messages; disclosure lines on the final answer; wire-recovery nudges as turn-scoped user riders. Nothing dynamic enters the stable prefix. |
| V | No redundant retransmission | PASS | History stays append-only; D5's replay contingency (if triggered) appends `reasoning_content` on active-window turns and folds at settled boundaries like existing payloads. D2/D6 add zero request bytes. Duplicate-read guard behavior preserved; its breaker-feeding gap is documented in the audit (A5.5) with disposition decided there. |
| VI | Honest measurement | PASS | Fresh baselines from provider-reported usage with price flags (D9); 001's invalid figure explicitly not reused (D9 alt); token counts primary comparator under peak/off-peak pricing (B2); salvage repairs and empty-completion retries recorded as observable wire events (D5); completion disclosures state recorded facts only (D6). |
| VII | Reference architecture: DeepSeek Reasonix | PASS | Part B studies Reasonix concretely (B8: startup injection, append-only loop, compaction-boundary pruning, versioned tool-schema contract, session-forked routing, tool-call repair) and maps each onto MuhiyaCode's existing equivalents; the one adopted delta (salvage hardening as a permanent component) is justified against MuhiyaCode constraints in D5. |
| VIII | Improve, don't rewrite | PASS | Every decision lands at an existing choke point: D1 at the six existing transition sites, D3 at `gatedExecute`/brief assembly, D4 in-place section edits, D5 in the existing provider paths, D6 inside `finalize`, D7 in `toolTarget`/`renderTool`. Rewrite alternatives (multi-plan archive, schema filtering, judge pass, auto-routing) all rejected in research Part C. `plan_state.json` change is additive with legacy derivation. |
| IX | Clean, maintainable, secure, provider-compatible | PASS | No new deps; `go fmt/vet/test` gates in quickstart. Security posture preserved: `readOnlyHint` absent→mutating (fail-closed), permission-denial feedback names the class without weakening any block, no changes to containment/trust/secret layers (not triggered for `docs/security.md` review, but D3.5 wording reviewed against it anyway). Non-DeepSeek providers: salvage/empty-retry/`tool_choice` constraints all degrade gracefully; no V4-only fields required. |
| X | Verified improvements | PASS (planned) | D9 sequence: baseline before any change (including red scripted-suite evidence of current defects) → identical-config after runs → gates on stability/hit-rate/tokens/cost/suite-pass. D5 replay change gated on a live probe. Evidence stored under `specs/004-deepseek-agent-polish/benchmarks/`. |

**Workflow gates** (constitution "Development Workflow & Quality Gates"): prefix-stability check — one bump, fixtures updated in-commit, live-validated (D4/D9); before/after evidence — D9 with red-baseline capture; security-sensitive review — not triggered (no permission/path/secret/command-blocking semantics change; D3's feedback wording additions reviewed against `docs/security.md`); docs sync — D11 tasks update `README.md`, `docs/agent-design.md`, `docs/architecture.md`, `docs/prompt-caching.md` in the same change.

**Post-Phase-1 re-check (2026-07-12)**: design artifacts (data-model.md, contracts/, quickstart.md) introduce no new violations: the phase machine is sidecar-only state; capability statements are mission-message content; the wire contract adds no required V4-only dependency; the one prefix bump remains the only cache-affecting event. PASS.

## Project Structure

### Documentation (this feature)

```text
specs/004-deepseek-agent-polish/
├── plan.md              # This file
├── spec.md              # Feature spec (validated 2026-07-12)
├── research.md          # Phase 0 — current-state evidence A1–A14, DeepSeek research B1–B10, decisions D1–D11
├── data-model.md        # Phase 1 — plan phase machine, capability statement, terminal states, wire events
├── quickstart.md        # Phase 1 — end-to-end validation scenarios and benchmark procedure
├── contracts/
│   ├── plan-lifecycle.md        # Phase enum, transition table, persistence, hint/affordance matrix
│   ├── mode-capability.md       # Capability delivery surfaces + enforcement feedback catalogue + bounds
│   ├── completion-terminal.md   # Terminal-state guarantees + completion-audit reconciliation rules
│   ├── deepseek-wire.md         # Model IDs, thinking/effort mapping, replay verification, salvage, empty-completion class
│   ├── prompt-architecture.md   # Section order, size budget, priority rule, addendum rules, bump procedure
│   └── tool-row-target.md       # apply_patch target extraction + middle-truncation (delta on 003 tool-display)
├── audit.md             # D8 — created at implementation start, seeded from research Part A
├── benchmarks/          # D9 — baseline + after evidence (created during implementation)
└── tasks.md             # Phase 2 (/speckit-tasks — not created by /speckit-plan)
```

### Source Code (repository root)

```text
F:\MuhiyaCode Agent Go\
├── internal/contract/
│   └── types.go             # D1: PlanPhase type + constants; PlanStateSnapshot +Phase (additive)
├── internal/orchestrator/
│   ├── plan.go              # D1: phase accessors/transitions; maybeSignalPlanReady requires incomplete steps;
│   │                        #     ClearPlanState semantics keyed on phase
│   ├── engine.go            # D1: finalize stamps finished/interrupted (uses StopCause); pending-plan injection
│   │                        #     → executing; D2: guaranteed ToolEnd emission; D3: gate feedback upgrades
│   │                        #     (readOnly MCP allowance, first-denial hard close, propose_changes honesty);
│   │                        #     D6: completion audit in finalize
│   ├── classify.go          # D1: continuation matching widened (word-boundary phrase list)
│   ├── goal.go              # D1: plan-mode toggle ↔ phase (drafting) wiring
│   ├── subagent.go          # D3/D10: capability statement + report-contract line in mission briefs;
│   │                        #     isMutation honors MCP readOnlyHint
│   ├── registry.go          # D3: unknown-tool nearest-name suggestion
│   ├── inspection.go        # D3: isMutation MCP readOnlyHint (mirror site)
│   ├── prompt.go            # D4: dedup + priority line (one-time bump); D10 rides tool descriptions (workspace)
│   └── *_test.go            # new: plan_lifecycle_test, mode_scenario_test, completion_audit_test,
│                            #     terminal_state_test; extended: prompt_stability, restart_determinism, cachehit_guard
├── internal/workspace/
│   └── registry.go          # D10: tool-description quality pass (ships inside the D4 bump)
├── internal/state/
│   └── session.go           # D1: plan_state.json read/write with phase field + legacy derivation
├── internal/gateway/
│   ├── provider.go          # D5: replay verification hook/contingency; salvage hardening; zero-token
│   │                        #     empty-completion classification (bounded retry class)
│   └── model.go             # D4: DeepSeek addendum +1 sentence; D5: model-ID hygiene notes
├── internal/mcpclient/
│   └── manager.go           # D3: capture tools/list annotations.readOnlyHint; expose on tool surface
├── internal/command/
│   ├── root.go              # D1: headless resume notice keyed on phase
│   └── application.go       # D1: plan-state restore wiring (phase); /plan clear action plumbing
├── internal/tui/
│   ├── view.go              # D1: progress line phase-keyed; D7: truncateMiddle for target segment
│   ├── model.go             # D2: task-end sweep (running→cancelled for tools/agents); D7: apply_patch target
│   ├── actions.go           # D1: resume notice + modal wiring phase-keyed; /plan clear
│   └── *_test.go            # D7 row-order tests; D1 hint-surface tests; D2 sweep tests
├── benchmarks/cachebench/   # D9: baseline + after runs (harness exists from 001; price flags supplied)
└── docs/                    # D11: README.md, agent-design.md, architecture.md, prompt-caching.md sync
```

**Structure Decision**: Single-repo change, strictly layered so the one cache-affecting event is isolated and independently revertible: (1) all D1/D2/D6 state-and-truthfulness work is prefix-inert by construction (sidecar, finalize, TUI, gate feedback); (2) the D4 prompt edit + D10 description pass land as **one commit** — the only prefix-byte change — with fixtures updated in the same commit and the D9 after-benchmark keyed to it; (3) D5 wire hardening is gateway-layer client code gated behind the live replay probe; (4) D7 is TUI-only. Implementation order follows D9's measurement-first rule: baselines (including red scripted suites) → prefix-inert fixes → the single prefix bump → wire hardening → after-benchmarks.

## Complexity Tracking

No constitution violations to justify. Two scope notes recorded for reviewer awareness (not violations):

| Item | Why needed | Simpler alternative rejected because |
|---|---|---|
| One-time prefix cache invalidation (D4+D10 bundle) | FR-018's dedup/priority edit and B4's description-quality finding both change prefix bytes; bundling them means exactly one invalidation event for the feature | Shipping them separately doubles cache-invalidation events (violates V's spirit); skipping them leaves documented duplication/ambiguity in the launch prompt (FR-018 unmet) |
| D5 replay contingency (store+replay `reasoning_content`) is designed but only built if the live probe demands it | B6's evidence conflicts with the working live system; building preemptively risks unverified complexity and larger payloads | Assuming either branch without probing violates X (unverified change) or risks a launch-blocking 400 class on multi-turn tool chains |
