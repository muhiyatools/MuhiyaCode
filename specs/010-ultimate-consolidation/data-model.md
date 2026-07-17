# Data Model — Ultimate Consolidation (feature 010)

**Date**: 2026-07-15. From [spec.md](spec.md) Key Entities + [research.md](research.md)
R1–R5. Only entities this feature creates or reshapes are modeled.

## 1. Lifecycle (new — replaces both existing machines)

`internal/orchestrator/lifecycle.go`, owned under the existing `modeMu`.

| Field | Definition |
|---|---|
| `State` | one of 11 `contract.LifecycleState` values: `direct, research, planning, awaiting-approval, pending, implementing, validating, interrupted, finished, superseded, discarded` |
| `Depth` | `light` / `full` (unchanged semantics from 009) |
| `Why` | the recorded verdict reason (dynamic surface only) |
| Gate facts | `ResearchCompleted, PlanWritten, Approved, StepsComplete, Validated` (rebuilt on restore from the durable plan, as today) |
| `Degradations []` | `{State, Reason}` ledger (absorbs PipelineDegradation) |
| `planBarStrikes` | bounded-guidance counter (unchanged behavior) |

**Predicates** (the only consumer API — no raw-state comparisons outside the
type): `IsReadOnly` ≡ `BlocksMutation` = {research, planning,
awaiting-approval}; `InvitesProceed` = {pending, interrupted};
`IsApprovalPause`; `IsPipelineResumable` = {research, planning, implementing,
validating}; `IsTerminal` = {finished, superseded, discarded}; `IsActive` =
≠ direct; `HasAnyDegradation`.

**Transitions** (single legal-edge table; terminal states exit only into a
fresh planning/research):

```text
direct → research|planning        research → planning
planning → awaiting-approval      awaiting-approval → implementing|planning|pending|direct(park)
pending → implementing|superseded implementing → validating|interrupted
validating → finished|interrupted interrupted → implementing|superseded
any → discarded (/plan clear)     non-terminal → superseded (new plan over old)
```

**State invariants**: exactly one authoritative value; every display, gate,
persistence, and resume consumer reads it through predicates; the plan⇄goal
exclusion (goal active ⇒ not read-only-state) is preserved as a transition
guard, not a second flag.

## 2. LifecycleSnapshot (changed — additive persistence)

`contract.PlanStateSnapshot` gains `State LifecycleState \`json:"state,omitempty"\``
(+ reuses `pipeline_depth` as `depth`). Legacy fields (`planMode`,
`pendingPlan`, `phase`, `pipeline_phase`, `pipeline_depth`) become read-only
compat inputs. **One migration loader** in `internal/state` is the only code
that reads them: state → pipeline_phase mapping → legacy-flag derivation, with
the two truthfulness corrections preserved verbatim. Malformed-file-is-absent,
terminal-written-not-deleted, idle-clear rules unchanged.

## 3. InstructionText (new — the audited registry)

`internal/instructions` (foundation layer; imports only `contract`):

| Field | Definition |
|---|---|
| `ID` | stable identifier (e.g. `gate.plan-bar`, `tool.update_plan.desc`) |
| `Audience` | MainStatic / MainDynamic / Subagent / Gate |
| `Cache` | Prefix / Tail / Sidecar |
| `Body` | the text or template |
| `StatesRule` / `EnforcesRule` | rule-ID linkage for the stated-in-advance audit |
| `Example` | worked-example ID (mandatory for formatted deliverables) |
| `MentionsTools` / `AllowlistCtx` | for the capability-reference audit |

Relationships: every Gate text's `EnforcesRule` must match ≥1 visible
`StatesRule` text; every `Example` must pass the enforcing validator; every
`MentionsTools` entry must be in `AllowlistCtx`'s real allowlist; Prefix-class
bodies must contain no dynamic sentinels. The rendered Prefix golden is the
recorded epoch artifact.

## 4. FaultCase (new — the chaos catalog)

`internal/orchestrator/faultinjection_test.go` table rows:

| Field | Definition |
|---|---|
| `Name` / `Class` | catalog identity (FR-005 classes) |
| `Setup` / `Responses` / `Prompt` | scripted-provider scenario |
| `WantOutcome` | exactly one of `guided-success` / `recorded-degradation` / `user-decision` |

Every row passes through `assertRecoveryInvariant`: exactly-one-outcome +
liveness (bounded turns; no identical denial >3×; no hard crash).

## 5. WiringEntry (new — the advertised-surface inventory)

`contracts/wiring-inventory.md` rows: `Surface | Identifier | Advertised
behavior | Verified-by (test:line or manual ref) | Status(wired|removed)`.
Guard tests enumerate the LIVE surface (registry tools, synthetic tools,
palette + runSlash cases, handleKey cases, Settings fields, AgentEvent kinds,
Callbacks members) and fail on any divergence in either direction; no third
status exists.

## 6. RemovalRecord (new — the removal ledger)

`RL-###` rows: Symbol, Location, Class (dead / test-only / write-only-field /
injected-unread / duplicate-merged), Proof (zero-reference command + deadcode
output), Migration note (mandatory when Class ≠ dead), Verification (green
suite). Commit rule: proof OR migration-note+green — never neither.

## 7. Relationships

```text
Assessment ──▶ verdict ──▶ Lifecycle.State (one truth)
Lifecycle ──predicates──▶ gates / TUI / persistence / resume (no raw flags)
LifecycleSnapshot(legacy fields) ──migration loader──▶ Lifecycle.State
InstructionText registry ──audit tests──▶ zero contradictions / stated-in-advance / examples pass validators
FaultCase table ──assertRecoveryInvariant──▶ three-outcome + liveness property
WiringEntry ⟷ live surface enumeration (bidirectional guard)
RemovalRecord ──▶ every deletion (proof or migration note)
```

## 8. Explicitly unchanged (compat boundaries)

Wire protocol client↔gateway; `~/.muhiya` layout (one additive field);
effort→allowance numbers and classAgents; the four subagent kinds; pinned
denial/notice texts byte-for-byte; approval always-pause; DeepSeek replay and
conformance captures; no hard token/turn failure ceilings.
