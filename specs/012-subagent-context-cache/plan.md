# Implementation Plan: Subagent Context Reuse & Cache-First Orchestration

**Branch**: `012-subagent-context-cache` | **Date**: 2026-07-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/012-subagent-context-cache/spec.md`

## Summary

Subagents today are born cold and die silently: every dispatch starts an empty transcript, re-reads what its predecessor already read, and writes a brand-new provider cache prefix; the main model compounds the waste by front-loading file reads and re-pasting that context into handoffs. This feature makes subagent context a durable, linkable asset — a continuation subagent extends its predecessor's conversation stream so the provider serves the shared prefix from cache (digest-seeded fallback when blocked), the main model is structurally confined to orchestration during implementation phases (phase-scoped read gate), handoffs become plan-references instead of context dumps, and every link decision and cache outcome is recorded and provider-verified. Five architecture decisions are already locked by clarification: continuation-first hybrid linking, task + same-session-follow-up eligibility, same-kind chains + review-after-implement, hard read gate with exemptions, and a majority staleness threshold on the predecessor's read-set.

Technical approach (validated by Phase 0 research): a continuation dispatch replays the predecessor's **stored transcript verbatim** (byte-identity is already golden-tested per provider family) on the predecessor's exact pin and appends the new phase handoff as one user message — both DeepSeek (automatic prefix cache, token-0 identity, 64-token blocks) and MiniMax (passive cache, tool-list→system→messages order) bill the shared prefix as cache reads; review-after-implement continues the implementer's stream with harness-side capability masking so the wire tool array never changes; each completed subagent persists a Context Record (verbatim transcript, end-of-run mtime+size fingerprints of its touched set — making self-edits never-stale — and structured result) as an additive session sidecar; the linkability decision runs in the dispatch path as a recorded reviewgate-style decision; the read gate joins the existing shared gate chain keyed on lifecycle state with denylist scoping and a ≤2-then-waive bound. Research also surfaced and this feature fixes two live defects: every dispatch cold-writes its prefix today because the per-run handoff sits inside the system message (relocated to the first user message — R-D6), and planning turns never receive the research findings briefing in the normal single-Run flow (R-D11).

## Technical Context

**Language/Version**: Go 1.25.0 (single module `github.com/muhiya/muhiyacode`)

**Primary Dependencies**: Bubble Tea v2 / Bubbles v2 / Lip Gloss v2 (TUI), modernc.org SQLite (session store), no new dependencies anticipated (Constitution IX: new deps require justification)

**Storage**: existing `~/.muhiya/state` SQLite session store + per-session records (compatibility boundary per Constitution VIII — Subagent Context Records extend the existing store, no new storage system)

**Testing**: `go test ./... -count=1` with race-enabled runs on CGO runners; golden-file prefix tests (`internal/instructions`, `internal/orchestrator` testdata) and the prefix-stability guard extended to continuation replays; benchmark harness from feature 011 (`scripts/bench_011.*`, `MUHIYA_BENCH_JSON=1`) for before/after evidence

**Target Platform**: Windows / Linux / macOS terminals (same as shipped CLI)

**Project Type**: single Go CLI/TUI application with internal packages (`internal/orchestrator`, `internal/gateway`, `internal/instructions`, `internal/state`, `internal/workspace`, `internal/contract`, `internal/tui`, `internal/command`)

**Performance Goals**: SC-001 ≥60% cache-read share on a continuation's first request; SC-004 ≥25% cost reduction on multi-phase benchmark tasks; SC-005 ≥80% (DeepSeek) / ≥70% (MiniMax-via-OpenRouter) steady-state subagent-stream hit rates; SC-006 ≤10% communication overhead

**Constraints**: byte-identical stable prefixes (Constitution III) — a continuation replay MUST reproduce the predecessor's serialized transcript exactly; provider-reported figures only (VI); OpenAI-compatible degradation for generic providers (IX); no session-format break without migration (VIII); correctness outranks reuse everywhere (I) — stale reads re-read, links declined when in doubt

**Scale/Scope**: two provider pairings served first-class (DeepSeek-direct; MiniMax M3 via OpenRouter through the MuhiyaLLM gateway); sessions with up to dozens of sequential dispatches; predecessor transcripts bounded by subagent TokenCeiling (feature 011) so inherited context stays well under the 128K/1M windows

## Constitution Check

*GATE: evaluated against Constitution v1.0.0 before Phase 0; re-evaluated after Phase 1 design — both passes recorded here.*

| # | Principle | Gate question for this feature | Pre-research | Post-design |
|---|-----------|-------------------------------|--------------|-------------|
| I | Correctness Before Optimization | Does any reuse mechanism ever outrank correctness? | PASS — staleness re-reads, majority-threshold link decline, and gate exemptions (post-failure diagnosis) are all specced to resolve toward correctness (FR-005, FR-016, Clarifications Q4/Q5) | PASS — data model records every decision; contracts make decline paths first-class |
| II | Cache Efficiency Without Quality Loss | Does linking remove/truncate content the model needs? | PASS — continuation *adds* context; the digest fallback is a recorded degradation, not silent truncation; FR-016 pins no-quality-regression | PASS — digest contract carries carry-forward facts + banked full report pointer |
| III | Deterministic Stable Prefix | Can continuation replay stay byte-identical? | CONDITIONAL — this is THE central technical risk; Phase 0 must verify replay determinism per provider (marshal determinism tests, ReasoningReplay modes) | PASS — R-F5–R-F9 prove replay determinism is golden-tested per family; R-D1 replays stored structs verbatim; the prefix-stability guard extends across the continuation boundary (contracts/context-linking.md CL-2, abort-to-fallback on drift) |
| IV | Dynamic/Cached Separation | Do handoffs ride the latest message only? | PASS — phase handoffs are the appended user message on an existing stream; nothing dynamic enters the settled prefix | PASS — phase-handoff contract fixes placement |
| V | No Redundant Retransmission | Does the feature reduce re-sends without rewriting settled history? | PASS by design — continuation is append-only; digest fallback avoids replaying transcripts into the main conversation | PASS — ledger records every avoided re-read |
| VI | Honest Measurement | Are all displayed figures provider-reported? | PASS — FR-015/SC-008 mandate provider-reported or labeled-unavailable | PASS — verification outcome is a first-class field of Context Link |
| VII | Reference: DeepSeek Reasonix | Studied before cache-affecting design? | Addressed in Phase 0 — research.md carries the Reasonix reference observations (R-F23) | PASS — recorded; R-D6 is a direct Reasonix conformance fix |
| VIII | Improve, Don't Rewrite | Smallest change; compat boundaries kept? | PASS — extends per-kind pins, session store, gate machinery, handoff contract; no subsystem rewrite; session-store additions are additive | PASS — data model is additive; legacy sessions load unchanged (FR-017) |
| IX | Clean/Secure/Provider-Compatible | Generic-provider degradation? | PASS — linking gates on provider profile support; generic profile → digest fallback, never hard dependence | PASS — Provider Cache Profile contract includes the generic degradation row |
| X | Verified Improvements | Before/after evidence planned? | PASS — feature 011 harness + fixtures are the vehicle; baselines on unchanged build precede behavior changes (execution ordering pinned in tasks) | PASS — quickstart.md defines the exact runs |

**Gate verdict (pre-research)**: PASS with one CONDITIONAL (Principle III replay determinism) that Phase 0 research was required to resolve — and did (see research.md R-D1, R-F5–F9).

**Gate verdict (post-design)**: PASS — no violations; Complexity Tracking empty.

## Project Structure

### Documentation (this feature)

```text
specs/012-subagent-context-cache/
├── plan.md              # This file
├── research.md          # Phase 0 — forensic map + provider cache truth + decisions
├── data-model.md        # Phase 1 — Context Record, Link, Ledger, Handoff, Profiles
├── quickstart.md        # Phase 1 — validation scenarios & measurement runs
├── contracts/
│   ├── context-linking.md    # linkability decision, continuation replay, fallback, verification
│   ├── phase-handoff.md      # lean outbound handoff + structured return (revises 011's subagent-handoff)
│   └── role-gate.md          # phase-scoped main-model read gate + exemptions + telemetry
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
internal/
├── orchestrator/
│   ├── subagent.go          # dispatch path: consult linker, replay continuation, record outcome
│   ├── contextlink.go       # NEW — linkability decision, Context Records, staleness check, ledger
│   ├── phaserunners.go      # phase dispatch: plan-reference handoffs, review-after-implement chain
│   ├── turnloop.go          # main-loop glue: gate wiring, ledger → task stats
│   ├── reviewgate.go        # (pattern source) decision-record style reused by contextlink
│   ├── engine.go            # engine fields for ledger/records lifecycle
│   └── usage.go             # per-pin attribution feeding link verification
├── instructions/
│   ├── pipeline.go          # revised handoff/prelude text (plan-reference, no pasted bodies)
│   └── prompt.go            # main-model role-separation instruction (gate-backed)
├── gateway/
│   ├── model.go             # Provider Cache Profile additions (linking method per family)
│   └── provider.go          # continuation replay assembly (stored-bytes fidelity)
├── state/
│   └── (session store)      # additive: Subagent Context Records + Delegation Ledger persistence
├── contract/
│   ├── types.go             # TaskStats: link/ledger fields; Settings knob
│   └── cache.go             # per-pin verification helpers (extends PerPairingRates)
└── workspace/
    └── files.go             # read-set capture (content hashes) for staleness checks
```

**Structure Decision**: single-project layout (existing repo structure); one new file (`internal/orchestrator/contextlink.go`) following the `reviewgate.go` precedent — every other change extends files that already own the touched behavior, per Constitution VIII. The 800-line file budget (arch guard) may force a `contextlink_store.go` split; acceptable within the same package.

## Complexity Tracking

> No Constitution Check violations — table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
