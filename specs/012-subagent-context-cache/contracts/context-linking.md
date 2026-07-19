# Contract: Context Linking

**Feature 012** · Governs the linkability decision, continuation replay, digest fallback, and verification. Implements FR-001–FR-005, FR-010–FR-013; decisions R-D1–R-D5, R-D9–R-D10, R-D12.

## CL-1 Decision procedure (every dispatch, recorded)

For every subagent dispatch the linker evaluates, in order, and records the **first** failing criterion as the reason:

1. **Candidate exists** — same-task phase lineage first; else the session's most-recent terminal-task chain passing the relatedness predicate (R-D9). None → `fresh` / `no-candidate`.
2. **Kind pair supported** — same-kind, or review-after-implement (Clarification Q3). Else → `digest-seeded` / `kind-pair-unsupported`.
3. **Terminal shape** — predecessor `clean-done` (R-D4). Else → `digest-seeded` / `terminal-shape:<shape>`.
4. **Stream identity intact** — same modelID and pin string still configured (R-F11). Else → `digest-seeded` / `model-changed` or `pin-mismatch`.
5. **Staleness** — changed-fraction of the predecessor's touched set ≤ 0.5 against **end-of-run** fingerprints (R-D5). Else → `digest-seeded` / `stale:<pct>`.
6. **Window fit** — `finalPromptTokens + handoffEstimate + 20% headroom ≤ model window` (R-D10). Else → `digest-seeded` / `window-overflow`.
7. **Provider supports continuation** — `ProviderCacheProfile.ContinuationLinking == supported`. Else → `digest-seeded` / provider reason.
8. All pass → `continued` (form: `same-kind` or `review-after-implement`).

`Settings.ContextLinking == "off"` short-circuits to `fresh` / `disabled`. An explicit model-issued `run_subagent` call is linked by the same rules (linking is a harness optimization, invisible to the calling model beyond the notice line).

## CL-2 Continuation replay

- The request message array = predecessor transcript **verbatim** (stored structs, raw `ReasoningDetails`, complete tool pairings) + one appended user message (PhaseHandoff per contracts/phase-handoff.md).
- System message, tool definitions, model, pin: the predecessor's, byte-identical (R-D1). Never re-render, never re-redact, never repair — a record needing repair is non-linkable at save time.
- Review-after-implement: role instructions ride the appended user message; the harness's dispatchScope masks mutating tools with a bounded denial; the wire tool array is unchanged (R-D2).
- The subagent prefix guard extends across the boundary: the continuation's first `PrefixShape` must equal the shape of the predecessor's final request extended by the appended message — any mismatch aborts the continuation into digest fallback with reason `replay-drift` (never a silent cold write).
- Staleness re-reads: paths in `rereadDirectives` are exempt from the duplicate-read block for this run.

## CL-3 Digest fallback

- Fresh two-message conversation (post-R-D6 shape: stable kind prefix + handoff-in-user-message).
- Handoff `carryForward` = predecessor's structured result digest + banked-knowledge pointer + touched-file list with changed-markers. Bounded by existing briefing caps.
- Recorded as `digest-seeded` with reason; **never displayed as "linked"** (US3-AS2).

## CL-4 Verification (honesty)

- After the continuation's first provider response: `verification.pairedCacheReadShare` = paired cache-read ÷ (read+miss) from provider-reported usage; `reported=false` when the provider sent no cache fields — displayed "unavailable".
- A `continued` link whose verified share is < 20% on a reporting provider raises the notice `linked but cold (provider cache miss)` and records attribution (gateway restart / eviction / replay drift) — the honest path for silent stickiness loss (R-D12).
- Metrics: continuation requests join the predecessor's (model, pin) pairing in `PerPairingRates` — no new cold-write exclusion (R-F12).

## CL-5 Concurrency

- Chains are per-run-lineage, not per-pin: parallel same-kind dispatches form parallel chains on the shared pin (R-F16). A predecessor may head at most one `continued` successor at a time; concurrent claims serialize by dispatch order, losers re-decide (typically digest fallback).

## CL-6 Tests pinned by this contract

- Replay byte-identity per provider family (golden, extends marshal_determinism pattern).
- Decision table: one test per CL-1 criterion producing its reason code.
- Staleness: self-edit chain (implement→implement, predecessor edited N files) links with changed-fraction 0 (end-of-run fingerprints).
- Fallback display: `digest-seeded` never renders as linked; `linked but cold` fires under injected zero-cache usage.
- Legacy: `ContextLinking=off` and absent `agents/` sidecars reproduce today's behavior byte-for-byte on the wire (FR-017).
