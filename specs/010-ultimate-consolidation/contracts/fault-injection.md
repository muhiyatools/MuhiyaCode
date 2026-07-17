# Contract: Fault-Injection Suite & the Recovery Invariant

Feature 010 (US2; FR-005..008). Evidence: [research.md](../research.md) R5;
shape in [data-model.md](../data-model.md) §4.

## 1. The invariant

| ID | Requirement |
|---|---|
| FI-1 | Every catalog row runs a FULL `Engine.Run` against the scripted provider (never a unit stub) and passes through ONE shared `assertRecoveryInvariant` helper |
| FI-2 | The helper asserts exactly one of three outcomes — guided-success, recorded-degradation, user-decision — and fails on any fourth (error loop, silent drop, stall, hard crash) |
| FI-3 | Liveness: turns strictly below the hard ceiling; no identical denial text appears more than 3× in history (the machine-checkable form of "no gate rejects forever", FR-007); runErr is nil or context.Canceled |
| FI-4 | Limits never destroy work (FR-008): any row ending at a bound must show partial results preserved (banked findings, saved plan, wrap-up report) |

## 2. The catalog (seed rows; grows with every future incident)

| ID | Requirement |
|---|---|
| FI-5 | Malformed tool arguments: truncated JSON, wrong primitive type, missing required field, bad enum — through the main loop (the untested schema branches) AND the subagent scope → guided-success |
| FI-6 | Budget exhaustion at every phase: per-phase subagent allowance in research and validate → recorded-degradation; turn-governor and failure-terminator paths → recorded-degradation or user-decision |
| FI-7 | Interrupt/restart at every lifecycle state → resumes then reaches one of the three outcomes (reuses the resume-from-every-state machinery) |
| FI-8 | Oversized plan (13+ steps) → guided-success (the >12 rejection message then a valid re-emit); the currently untested rejection branch gets covered |
| FI-9 | Invalid search pattern through a full Run (regex compile failure, then the literal/escape guidance) → guided-success |
| FI-10 | Blocked capabilities (mutating shell in read-only state; unknown tool; dead MCP) → guided-success or recorded-degradation with bounded escalation |
| FI-11 | Empty/failed subagent results and the wrap-up path → recorded-degradation with partial findings preserved |
| FI-12 | The FR-004b trailing-intent narration retry → guided-success within its bounded retries |
| FI-13 | All seven pinned 008/009 live incidents (content-bar deadlock, exit-plan desync, /plan-clear deadlock, stale-approve leak, terminal revival, depth loss, premature finish) are ported into the table so they inherit the invariant |

## 3. Self-protection

| ID | Requirement |
|---|---|
| FI-14 | A mutation-guard check proves the invariant bites: flipping a bounded-recovery constant (e.g. the plan-bar strike limit) via a test-only hook MUST fail the suite |
| FI-15 | New incidents become permanent rows, not one-off fixes (spec edge case); the row template makes the invariant unavoidable by construction |

## Acceptance

- SC-002: 100% of catalog rows pass with the invariant asserted; zero
  unrecoverable states.
- Coverage matrix cells marked MISSING/PARTIAL in R5 are all COVERED.
