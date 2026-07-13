# Implementation Plan: Reasonix-Aligned Agent Flow Overhaul

**Branch**: `002-reasonix-agent-overhaul` | **Date**: 2026-07-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-reasonix-agent-overhaul/spec.md`

## Summary

Close every defect in `IMPLEMENTATION_REVIEW.md` (Parts A–E, six shipping blockers first), then align MuhiyaCode's agent flow with the Reasonix reference architecture mechanism-by-mechanism so that the entire request pipeline — system prompt, tool surface, history lifecycle, context reclamation, compaction, and per-turn dynamics — is built for byte-prefix cache hits as its first-order design constraint. The Reasonix analysis (Phase 0, `research.md`) produces a complete Mechanism Inventory with an explicit adopt/adapt/reject/already-present decision per mechanism (FR-007/FR-008); the design artifacts (Phase 1) freeze the request-assembly contract so future changes cannot silently regress cache behavior. Validation is a matched-workload benchmark against Reasonix plus the existing cachebench suite, with append-only evidence.

**The one-sentence thesis from the reference analysis**: Reasonix's 98–99% hit rate is not one trick but a single invariant enforced everywhere — *the prefix (system + tools + settled history) is composed once and never re-rendered, every dynamic byte rides the newest user turn, and every deliberate rewrite is rare, high-yield, and accounted* — plus four supporting algorithms MuhiyaCode still lacks (persisted environment/probe snapshots ✅ already present, two-tier stale-result reclamation ❌, digest-accumulating compaction geometry ❌, static cache-discipline prompting ❌, resume byte-identity ⚠️ to verify).

## Technical Context

**Language/Version**: Go 1.22+ (module `github.com/muhiya/muhiyacode`)

**Primary Dependencies**: Bubble Tea + lipgloss (TUI), standard library `net/http` + SSE parsing (provider I/O), no new third-party dependencies permitted by this feature (H1 validator stays hand-rolled)

**Storage**: file-based session state under the user state dir (session JSON, `plan.md`/`tasks.md`, `goal.json` sidecar, tool-surface snapshots, probe store; this feature adds `pruned.jsonl` archives and a plan-state sidecar field)

**Testing**: `go test ./...`, `go vet`, `CGO_ENABLED=1 go test -race` (orchestrator + mcpclient), `benchmarks/cachebench` against the live MuhiyaLLM gateway, matched-workload script vs Reasonix

**Target Platform**: Windows/macOS/Linux terminal (developer workstation); provider = DeepSeek-compatible OpenAI wire through the MuhiyaLLM gateway (implicit byte-prefix caching, per-model namespace, `X-Muhiya-Session` stickiness already deployed)

**Project Type**: single Go CLI/TUI application with internal packages (`internal/orchestrator`, `internal/gateway`, `internal/contract`, `internal/state`, `internal/mcpclient`, `internal/tui`, `internal/command`, `internal/workspace`)

**Performance Goals**: prefix-stability ≥ 99.5% (SC-001); matched-workload steady-state hit rate within 0.5 pp of Reasonix and cost/task within 15% (SC-002); cache-read monotonicity outside recorded invalidations (SC-003); ≤ ~50 tokens system-added tail on plain follow-ups (SC-004)

**Constraints**: prefix byte-stability is a hard invariant (Constitution III/IV); tools array immutable within a session regardless of mode (FR-015); no gateway changes; improve-in-place — no rewrite (Constitution VIII); evidence files append-only (FR-019); no new heavy dependencies; Reasonix is reference-only (patterns re-implemented, no code copied verbatim)

**Scale/Scope**: ~8 internal packages touched; 20 functional requirements; 6 shipping blockers + ~24 review fix items + 4 adoption algorithms + pipeline determinism guards; 3 design contracts; one matched-workload validation campaign

## Constitution Check

*GATE: evaluated against Constitution v1.0.0 before Phase 0; re-checked after Phase 1 design.*

| # | Principle | Gate result | How this plan complies |
|---|---|---|---|
| I | Correctness before optimization | ✅ PASS | Phase A (blockers: session hard-fail trap, usage-frame hard error, goal data race, protocol orphaning, dead reconnect, broken modal) lands before any efficiency work; every optimization phase depends on Phase A's green gate. |
| II | Cache efficiency without quality loss | ✅ PASS | Reclamation archives every pruned original (`pruned.jsonl`) and floors tiny results; compaction pins the task statement and digests; the cache-discipline prompt section adds guidance without removing content; every cache-affecting change states what moved and why quality holds (research.md inventory). |
| III | Deterministic stable prefix | ✅ PASS | Core of the feature: request-assembly contract freezes section order and byte-determinism for system prompt, tools, and settled history; automated guards (prefix-shape, tail-budget, prompt-stability tests) enforce it continuously. |
| IV | Dynamic/cached separation | ✅ PASS | All per-turn content (briefs, goal/plan blocks, notices, pending-plan injection) stays on the newest user turn; the contract enumerates the complete allowed tail-tag set, mirroring Reasonix `compose()`. |
| V | No redundant retransmission | ✅ PASS | Two-tier reclamation + compaction geometry exist precisely to stop re-sending stale tool output; dedupe/failed-call gates stop redundant tool calls at the source. |
| VI | Honest measurement | ✅ PASS | Matched-workload comparison only (SC-002); raw vs prefix-stability metrics kept distinct; evidence append-only (FR-019); degraded guard paths must be loud (FR-017). |
| VII | Reasonix as reference | ✅ PASS | Phase 0 is a dedicated forensic analysis producing the Mechanism Inventory with file:line evidence; every mechanism gets an explicit decision (FR-008). |
| VIII | Improve, don't rewrite | ✅ PASS | Every adoption lands inside existing packages/mechanisms (e.g., reclamation extends `history.Maintain`, not a parallel system); no package is rewritten from scratch; deviations from Reasonix recorded with reasons. |
| IX | Clean, secure, provider-compatible code | ✅ PASS | Shared dispatch gate centralizes validation; no new deps; wire compatibility guarded by marshal-determinism tests; archives stay in the session dir (no new data exposure). |
| X | Before/after verification | ✅ PASS | cachebench before/after + matched workload + scenario walkthroughs are mandatory acceptance (SC-001..008); a change without its measurement is not done. |

**Post-Phase-1 re-check (2026-07-12)**: design artifacts introduce no violations — contracts codify the gates above; no Complexity Tracking entries required.

## Project Structure

### Documentation (this feature)

```text
specs/002-reasonix-agent-overhaul/
├── spec.md              # Feature specification (done)
├── plan.md              # This file
├── research.md          # Phase 0: Reasonix forensic analysis + Mechanism Inventory (adopt/adapt/reject)
├── data-model.md        # Phase 1: entities (StablePrefix, DynamicTail, InvalidationRecord, ReclamationTier, …)
├── quickstart.md        # Phase 1: validation guide (build → tests → race → cachebench → matched workload → scenarios)
├── contracts/
│   ├── request-assembly.md      # THE contract: prefix composition order, byte-determinism, tail tag set
│   ├── context-lifecycle.md     # Reclamation tiers, compaction geometry, archival, invalidation pairing
│   └── dispatch-gate.md         # Shared tool-execution gate semantics (validation, modes, dedupe, breakers)
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created by this command)
```

### Source Code (repository root)

```text
internal/
├── orchestrator/        # PRIMARY SURFACE
│   ├── engine.go        # loop; A1 maintenance gate order; B2 exit_plan_mode handling; B6 gatedExecute; B7 streak
│   ├── history.go       # EstimateMaintainYield (A1); reclamation geometry + archival (A4.2); compaction geometry (A4.3)
│   ├── goal.go          # B1 single modeMu; B3 marker scan completeness + StripGoalMarkers wiring; B4 idle semantics
│   ├── plan.go          # pendingPlan persistence already landed; B2(a) conditional exit
│   ├── prompt.go        # A4.1 static cache-discipline section (only sanctioned prefix change, one-time)
│   ├── subagent.go      # B6 shared gate adoption; per-run counters
│   ├── prefixshape.go   # unchanged mechanism; covered by new degraded-path + subagent tests (A3)
│   ├── classify.go      # unchanged (pending-plan continuation already lands here)
│   └── registry.go      # H8 message (done); definitions ordering guard test
├── gateway/
│   └── provider.go      # A2 usage-frame tolerance; marshal determinism guards (existing tests extended)
├── contract/
│   ├── types.go         # callCounters/TaskStats additions as needed by B6/B9
│   └── cache.go         # session hit-rate aggregation feeding A4.5 readout (exists; exposed to TUI)
├── state/
│   └── session.go       # pruned.jsonl archive I/O (A4.2); goal/plan sidecars (done, verified)
├── mcpclient/
│   └── manager.go       # D1 tools-map cleanup on markServerError; D3 30s bound; D4 tick gating
├── tui/
│   ├── mcp.go           # D2 modal-open refresh + terminal-state semantics
│   ├── model.go / view.go # E1 reasoningFull + verbose + spacing; E2 contentWidth unification; A4.5 hit-rate readout
│   └── actions.go       # B5 /goal busy guard
└── command/
    └── application.go   # D2 refresh trigger on modal open; (M5 lazy-connect conditional already landed)

benchmarks/cachebench/   # matched-workload scenario addition; append-only evidence outputs
docs/prompt-caching.md   # updated invariants (V4-style documentation updates)
```

**Structure Decision**: single-project layout is retained (existing repo shape). No new packages are introduced; every adoption extends the owning package listed above, honoring Constitution VIII. The feature's only sanctioned stable-prefix change is the one-time addition of the static cache-discipline section in `prompt.go` (Constitution III allows deterministic, session-invariant content; the change is an upgrade-boundary cache reset, documented in the request-assembly contract).

## Implementation Phases (executor-facing overview)

Ordering encodes the constitutional gate: correctness (A/B/D) → determinism guards (C) → adopted efficiency algorithms (E) → validation (F). Detailed per-task breakdown with test names lands in `tasks.md` (/speckit-tasks); the authoritative defect specs remain `IMPLEMENTATION_REVIEW.md` items (referenced as REV-xx below).

- **Phase A — Shipping blockers (REV Part A/B/D blockers)**: A1 maintenance estimate-before-mutate + 5% yield floor + event pairing (REV A1); A2 usage-frame tolerance (REV A2); B1 single `modeMu` (REV B1); B2 conditional `exit_plan_mode` + atomic batch pairing (REV B2); D1 reconnect tools-map fix + one-attempt bound (REV D1); D2 modal-open refresh + terminal "not connected" (REV D2). Gate: named regression tests + `-race` clean.
- **Phase B — Harness completion (REV Part B remainder)**: B3 goal-marker completeness + display stripping + intercept-once; B4 consecutive-idle semantics + missing-marker streak; B5 `/goal` busy guard; B6 `gatedExecute` shared gate + per-scope counters + subagent adoption; B7 all-failed-turns detector; B8 enum-membership fix + full H1 test suite; B9 subagent tokens into breaker + strengthened breaker test.
- **Phase C — Pipeline determinism guards (FR-013..017)**: request-assembly contract enforcement tests (section order, tools ordering, zero-variance steady-state diff, tail tag allow-list, ≤50-token tail budget guard, degraded-guard loudness, C2/C3 missing tests from REV A3). One audit sweep of every request-producing path (main, subagent, compaction, onboarding, classification) against the contract.
- **Phase D — MCP/TUI completion (REV Parts D/E remainder)**: D3 30s blocking-refresh bound; D4 tick gating; E1 thinking-section completion (reasoningFull, verbose, spacing, RTL); E2 width unification; E3 strengthened tests + cosmetic cleanups.
- **Phase E — Reasonix adoption (research.md inventory, adopt/adapt items)**: E1 static cache-discipline prompt section (A4.1); E2 two-tier reclamation with per-kind geometry + `pruned.jsonl` archival (A4.2); E3 compaction geometry alignment (A4.3); E4 steady-state zero-overhead tail guard (A4.4); E5 session hit-rate + cost readout (A4.5); E6 resume byte-identity verification (+fix if research finds re-rendering); E7 any additional adopt-decisions from the final Mechanism Inventory.
- **Phase F — Validation campaign (Group D FRs, SC-001..008)**: cachebench before/after; matched-workload script executed on both agents; scenario walkthroughs; evidence appended under `benchmarks/`; docs updated.

## Complexity Tracking

No constitutional violations to justify — table intentionally empty.
