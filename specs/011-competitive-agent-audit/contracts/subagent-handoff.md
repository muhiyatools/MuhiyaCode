# Contract: Subagent Handoff, Session Pins & Returns

**Consumers**: `runSubagentInput`/`executeSubagent` (`subagent.go`), pipeline phase runners (`phaserunners.go`), knowledge banking (`knowledge.go`), usage attribution (`usage.go`).

## 1. Brief (main → sub) — PRESERVED contract (F14)

A subagent receives exactly two messages and NEVER the main transcript:

1. **System**: `"You are the <kind> subagent inside MuhiyaCode. " + <kind role text> + workspace line + capability statement (sorted tool list) + HandoffContract.Render()`
2. **User**: the task string.

`HandoffContract` fields (unchanged): Role · Scope (≤ 1,200 chars) · Context (≤ 1,500 chars, scope-filtered knowledge digest — max 6 facts × ~320-char digests + ≤ 12 paths) · Deliverable · OutputFormat. Review-tier dispatches additionally carry the diff file list + change summary and the tier's scope instruction (contracts/review-gating.md §4).

**Invariant tests**: brief size bound; no-transcript assertion; empty-knowledge fallback note.

## 2. Session pins (cache identity) — CHANGED (D4)

| Stream | Pin (old) | Pin (new) |
|---|---|---|
| Main loop | `<session>:main` | unchanged |
| Compaction/aux | `<session>:aux` | unchanged |
| Subagent, any kind | `<session>:sub` | **`<session>:sub:<kind>`** (`:sub:explore`, `:sub:plan`, `:sub:review`, `:sub:general`) |
| Onboarding | `<session>:sub` | **`<session>:sub:onboarding`** |

Rationale: each kind has a distinct stable prefix; per-kind pins let provider prefix caches stay warm per kind instead of churning one identity (research F5). Wire-compatible: the gateway hashes any opaque pin (gateway `stickysession.go`). **Conformance**: extend `mixed_provider_test.go` — two different-kind dispatches carry different pins; two same-kind dispatches share one.

## 3. Token ceiling behavior — NEW (D3)

`subagentInput` gains `TokenCeiling` (0 = uncapped during rollout). Enforcement loop: after each subagent turn, add provider-reported total tokens; on `Consumed ≥ Ceiling`, inject one wrap-up instruction ("stop; report findings so far, what you covered, and what remains"), accept the next response as final, mark the result `partial`. Turn caps remain as today (secondary bound).

## 4. Return package (sub → main) — CHANGED for model-invoked path (D7)

| Path | Behavior |
|---|---|
| Pipeline launches | unchanged: success detection only; full report banks to Knowledge; phases re-enter findings as briefings; validation completion returns the fixed body |
| Model-invoked `run_subagent` | **new**: full report banks to Knowledge (same `AddPhaseReport` path); the tool result carries `Status (ok/partial/failed)` + digest (≤ existing digest bound) + follow-ups + truncation note. Reports ≤ the bound pass through verbatim. |

Invariant: the main history never receives more than the digest bound from any subagent (Constitution V). Existing `Reusable` zero-token repeat detection is preserved untouched.

## 5. Model & reasoning assignment — PRESERVED

`SubagentModelID` (fallback `ActiveModelID`) for all kinds; `AgentReasoning` one tier below main per effort profile (`effort.go`). Per-kind model assignment is explicitly OUT of scope for this feature (recorded as a possible future roadmap item, not a requirement).
