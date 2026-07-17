# Contract: The Instruction System

Feature 010 (US3; FR-009..011, FR-018). Evidence: [research.md](../research.md) R4;
shape in [data-model.md](../data-model.md) §3.

## 1. Single artifact

| ID | Requirement |
|---|---|
| IS-1 | Every model-facing text (system-prompt sections, all 19 tool descriptions + property descriptions, subagent system/handoff/capability texts, phase preludes, mode blocks, ALL gate/block/denial/loop-guard texts, model-facing tool errors, gateway per-family addenda) lives in `internal/instructions` (foundation layer, imports only `contract`) as a named constant plus a registered `Text` record |
| IS-2 | Each `Text` carries Audience, Cache class (Prefix/Tail/Sidecar), rule linkage (StatesRule/EnforcesRule), worked-example ID, and MentionsTools/AllowlistCtx; composers are pure functions of session-invariant inputs for Prefix-class output |
| IS-3 | Fallback (only if a layering cycle emerges): per-package instructions files with the SAME registry and audits — the single review surface is then the golden dump |

## 2. Audit suite (automated, fails on reintroduction)

| ID | Requirement |
|---|---|
| IS-4 | Stated-in-advance: every enforced rule has a matching rule text visible in the reader's context BEFORE violation, with a worked example when the rule governs a format (fixes the shell-allowlist-only-on-rejection class) |
| IS-5 | Contradiction check: canonical single copies referenced by ID — the plan-step example exists ONCE (kills the 4× duplication + "Risks"/"Risks/unknowns" drift); the `write_file` permission sentence is token-identical between tool description and system prompt |
| IS-6 | Capability-reference check: every tool a text names must be in the reader's real allowlist (wired from subagentSpecs); the `general` subagent's advertised schema is reconciled with its executor (trim the schema or route the synthetic tools) |
| IS-7 | Worked-example conformance: every example must PASS the validator its gate enforces (e.g. the plan-step example satisfies the content bar; a plan-note example with Verification:/Risks: is added and satisfies the section parser) |
| IS-8 | Gate-message quality: every gate/denial lists ALL unmet requirements at once, names the exact expected shape, reflects the reader's real remaining budget, and names an affordable next action (FR-011) — audited via required template fields, applied to the terse gates (repeat limiter, duplicate-read, plan-mode mutation block) |

## 3. Cache discipline

| ID | Requirement |
|---|---|
| IS-9 | A golden renders ALL texts (fixed inputs) into one reviewable dump; a SEPARATE Prefix-bytes golden covers exactly the cached prefix (system prompt + serialized tool JSON). The single update of the Prefix golden in the release PR IS the recorded epoch (FR-018) |
| IS-10 | Prefix-class bodies contain no dynamic sentinels (`[task-brief`, `phase=`, `agents<=`, `[F`, `[goal:`) — asserted, so dynamic state can never be refactored into the prefix |
| IS-11 | The move is byte-preserving at the wire: DeepSeek conformance captures and the prompt-budget/prefix-stability tests pass with at most the ONE sanctioned epoch delta (FR-019) |

## Acceptance

- SC-003: audit reports zero contradictions, zero unavailable-capability
  references, examples present + validator-passing for every formatted
  deliverable.
- The five ranked incoherences from R4 (general-subagent schema, shell
  allowlist disclosure, D1/D4 duplication, C1 write_file mismatch, missing
  plan-note example) are each closed and pinned by an audit assertion.
