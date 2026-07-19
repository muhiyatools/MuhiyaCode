# Results: Subagent Context Reuse & Cache-First Orchestration

**Feature 012** · Implementation completed 2026-07-19 at 33/41 tasks (all free code tasks; live-cost tasks gated on owner approval). Evidence rules: Constitution VI (provider-reported or labeled) and X (before/after on realistic runs). This file records what is proven now and exactly what the gated runs will settle.

## Build & suite status (offline evidence, complete)

- `go build ./...`, `go vet ./...`, `gofmt` clean; **full test suite green** across every package including arch guards (800-line budget, layering, dead-code), the wiring inventory (read_plan + contextLinking rows), the fault-injection recovery invariant (the read gate joins it), and the instruction audits (stated-in-advance for `rule.phase-read`, capability references for `read_plan`).
- One reviewed cache epoch executed (T010 handoff relocation + T011 `read_plan` + T024 approval-gate notice): instruction/wiring goldens regenerated once via `-update`; the new PH-6 test pins that two dispatches of one kind now put **byte-identical system messages and tool arrays** on the wire.

## What is proven by tests (mock provider, no cost)

| Claim | Test |
|---|---|
| Continuation replays the predecessor's transcript verbatim + exactly one appended user message, on the predecessor's pin | `TestContinuationReplaysPredecessorVerbatim` |
| Review-after-implement continues the implementer's stream; mutations refused harness-side; wire tool array unchanged | `TestReviewContinuationMasksMutations` |
| Self-edit chains are never stale (end-of-run fingerprints, changed-fraction 0) | `TestSelfEditChainNotStale` |
| >50% external changes decline with `stale:NN%` + digest fallback; ≤50% continue with re-read directives | `TestExternalEditMajorityDeclines`, `TestMinorityStalenessRereads` |
| All eight CL-1 criteria produce their machine-readable reasons | `TestDecideLink*` family |
| Provider-verified cache share stamped from paired fields only; absence renders unavailable | `TestContinuationVerificationStampsCacheShare`, `TestPairedCacheShare` |
| Read gate: deny ×2 → recorded waiver; shell-cat denied; grep passes; light-depth/degraded/post-failure/subagent scopes exempt; kill switch disables | `TestPhaseReadGate*` |
| `contextLinking=off` restores pre-012 dispatch behavior (fresh two-message conversations, zero records/links) | `TestLinkingOffRestoresLegacyDispatch` |
| Records round-trip byte-exactly incl. raw `reasoning_details` (compact encoding — a real re-indentation bug was caught and fixed by this test) | `TestAgentRecordRoundTripPreservesBytes` |
| Planning receives the findings briefing in the single-Run flow (R-F15 fix) | `TestResearchTransitionDeliversBriefing` |
| Terminal shapes distinguish clean/wrap-up/ceiling/failed/cancelled | `TestTerminalShapeFor` |
| Family capability: DeepSeek+MiniMax continuation-supported (DeepSeek effort-pinned), others digest-only | `TestContinuationLinkingCapability` |
| Summary roll-up renders continued counts + mean verified share, omits unreported | `TestLinkSummaryPart`, `TestLinkNoticeLine` |

## Pending live evidence (gated — owner approval required)

| SC | What the gated run settles | Vehicle |
|---|---|---|
| SC-001 ≥60% continuation first-request cache share | P1 probe magnitude + cl-001 fixture | `scripts/probe_012_continuation.ps1`, then T003/T039 bench runs |
| SC-002 zero still-current re-reads, ≥30% read reduction | cl-002 fixture ledger | bench `links[]` + read counts |
| SC-004 ≥25% multi-phase cost reduction | baseline (T003, frozen e163506 binary) vs after (T039) | `bench_011_report` comparison |
| SC-005 ≥80% DeepSeek / ≥70% MiniMax steady-state | per-pairing rows; **MiniMax target conditional on P2 routing truth** (production may be direct, not OpenRouter — research open item 1) | P2 owner query + bench |
| SC-006 ≤10% communication overhead | OutboundChars/ReturnChars per link over task tokens | bench records |
| SC-007 completion parity | fixture completion rates baseline vs after | bench records |
| SC-008 100% dispatch link-row coverage | every bench record's `links[]` | bench records |
| P3 effort-flip | whether `EffortPinned` stays true for DeepSeek | probe `-EffortFlip` |

Cold-start vs steady-state will be reported separately (Constitution: Measurement Standards); "linked but cold" incidents count against SC-001 honestly, never silently.
