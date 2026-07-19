# Contract: Phase Handoff (lean outbound / structured return)

**Feature 012** · Revises feature 011's subagent-handoff contract. Implements FR-006–FR-009, FR-014; decisions R-D6, R-D7; fixes R-F3 and R-F15.

## PH-1 Placement (the cache-first change)

- The subagent **system message is per-kind-per-session stable**: kind identity + spec.System + workspace + capability statement. Nothing per-run may enter it (R-D6; Reasonix conformance R-F23).
- The HANDOFF CONTRACT moves to the **first user message** (fresh dispatch) or the **appended user message** (continuation). One reviewed cache epoch: handoff/wiring goldens regenerate once; the instructions registry entries keep their Prefix/Sidecar cache classes accordingly.
- Repeated dispatches of a kind therefore share the provider-cached system+tools prefix (DeepSeek token-0 identity; MiniMax tool-list→system order, R-F19/R-F20).

## PH-2 Outbound fields

`role`, `scope`, `deliverable`, `outputFormat` (unchanged semantics) plus:

- `phaseRef` — plan phase id + the group's step titles + the plan Note's Verification/Risks digest (bounded). Closes the step-titles-only gap (R-F14).
- `carryForward` — non-discoverable facts only (decisions, constraints, prior-phase results not yet in the plan). Existing briefing bounds apply.
- `rereadDirectives` — changed-since-predecessor paths (continuations; CL-2).
- `roleOverride` — continuation cross-kind instructions (review-after-implement).

**Prohibited content** (test-enforced): file bodies; plan prose beyond the phase digest; any text duplicating the stable prefix; findings the subagent can read via `read_plan` or its own tools (FR-008).

## PH-3 The `read_plan` tool

- Read-only registry tool available to all four kinds; returns the rendered plan (whole, or a named phase section). Served from the session store by the harness — no filesystem path, no outside-workspace approval prompt (R-D7, closing the critic's plan-access gap).
- Added in the same one-time cache epoch as PH-1.

## PH-4 Structured return

- Report format per kind unchanged; adds a machine-readable trailer: `Changed: <paths>` / `Verified: <status>` / `CarryForward: <facts>` (bounded lines, parsed leniently — absence degrades to today's prose handling, never an error).
- Oversized reports: existing banking behavior (knowledge Full + digest note) unchanged; the ledger records return sizes for SC-006.

## PH-5 Main-model briefing fix

- The research→planning mid-Run transition appends the findings briefing (existing 4000-char bound) to the research status line so planning turns receive the findings in the normal single-Run flow (R-D11, fixing verified gap R-F15). Delivered once; rides the user-message tail.

## PH-6 Tests pinned by this contract

- System-message stability: two sequential dispatches of one kind produce byte-identical system messages and tool arrays (new golden).
- Prohibited-content guard: handoff assembly rejects embedded file bodies (test with a poisoned carryForward).
- `read_plan`: returns rendered plan sections; denied nothing; absent plan → guided empty result.
- Briefing fix: single-Run research→planning delivers findings exactly once (regression for R-F15).
- Overhead accounting: handoff+return sizes recorded per dispatch (SC-006 input).
