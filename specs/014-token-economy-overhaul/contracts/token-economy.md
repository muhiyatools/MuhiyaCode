# Contract: Token Economy Governor

## 1. Scope

This contract defines classification, phase control, request planning, budgets, overrides, and stop behavior. It is provider-independent and makes no model call.

## 2. Input

```go
type GovernorInput struct {
    SessionID            string
    EpochID              string
    UserEffort           contract.EffortLevel
    Assessment           Assessment
    Phase                ExecutionPhaseState
    Provider             ProviderCacheProfile
    Context              ContextManifestSummary
    Usage                EconomyAggregate
    ChangedFiles         []FileChange
    Verification         VerificationState
    Failures             FailureWindow
    Steering             SteeringState
}
```

All members are runtime facts or local deterministic classifications.

## 3. Output

```go
type RequestPlan struct {
    Phase             ExecutionPhase
    Reasoning         contract.ReasoningTier
    MaxOutputTokens   int
    ToolPolicy        ToolPolicy
    ContextBudget     SegmentBudget
    Recovery          RecoveryPolicy
    StopConditions    []StopCondition
    DecisionCodes     []string
}
```

Identical normalized inputs MUST produce identical output.

## 4. Phase policy

| Phase | Normal expected request | Allowed next states | Default tool policy |
|---|---|---|---|
| orient | direct answer or one batched inspection | inspect, finish | core |
| inspect | retrieve exact missing evidence | inspect, change, verify, finish | core/broker |
| change | one batched mutation | verify, recover | core |
| verify | one smallest proving check | finish, change, recover | core |
| finish | concise answer, no tools | terminal | none |
| recover | materially changed retry or blocker synthesis | prior phase, finish | explicit |

The runtime MUST NOT append a phase nudge on every request. The current objective is rendered once in the newest task control block and replaced, not accumulated, when request assembly uses a task epoch checkpoint.

## 5. Initial request budgets

Budgets are loaded from versioned configuration derived from live baselines. The implementation MUST NOT hard-code the research table as permanent truth. Required profile keys:

```text
class
risk_level
provider_family
phase
main_request_soft/hard
aux_request_soft/hard
cumulative_prompt_soft/hard
cumulative_output_soft/hard
inline_observation_soft/hard
default_reasoning
default_output_cap
truncation_output_cap
```

## 6. Reasoning selection

Start with the lower of user effort and phase default, then apply evidence-backed escalation:

```text
base = min(user_effort, phase_default)
+1 tier if high/critical risk
+1 tier if two distinct evidence-backed hypotheses failed
+1 tier for architecture/migration phase explicitly classified large/epic
cap at user_effort unless correctness/safety override exists
```

De-escalate back to phase default after the hard step ends. Never escalate because the model asked for more effort in prose.

## 7. Output-cap selection

`maxOutputTokens = clamp(profile.phaseCap, provider.safeFloor(phase), context.outputAllowance)`.

If a response is truncated:

1. Do not execute incomplete tool calls.
2. Persist the failed request usage.
3. Retry at most once for the same logical step.
4. New cap is `min(profile.truncationCap, oldCap*2, context.outputAllowance)`.
5. Request-plan reason includes `recover.output_truncated` and `retryOf`.
6. A second truncation exits with an honest blocker or splits a write using the existing chunk protocol; it does not double again.

## 8. Budget evaluation order

Before each request:

1. Apply user cancellation/steering.
2. Check persistence integrity.
3. Check safety/correctness-required work.
4. Recompute exact provider-reported consumption from usage ledger.
5. Evaluate phase completion; finish without another request when a deterministic final can be emitted safely.
6. Evaluate soft budgets and narrow the next plan.
7. Evaluate hard budgets.
8. Accept a valid override or stop with factual status.

## 9. Hard-budget behavior

| Condition | Behavior |
|---|---|
| No mutation, no required work | finish immediately |
| Mutation complete, verification required | allow `required_verification` override for one smallest check |
| Provider/network retry eligible | allow one `provider_recovery` request; keep separate accounting |
| User adds scope | recompute budget with `user_steering`; do not erase earlier spend |
| Safety/correctness action needed | allow minimum required action and report override |
| Model is exploring without new evidence | stop exploration and finish/block |
| Same failure class repeated | recovery must change approach; otherwise stop |

## 10. Useful-action density

A request is productive if it yields at least one of:

- new valid evidence satisfying a requirement
- successful mutation
- successful verification
- required user decision
- final answer

Two consecutive unproductive main requests trigger local convergence. Three in one phase require recovery or finish. The controller records counters; it does not inject repetitive prose.

## 11. Auxiliary-call admission

An auxiliary call is allowed only when all are true:

1. no deterministic/local method can answer the decision,
2. expected decision changes the execution path materially,
3. its configured expected token cost is less than expected main-loop savings,
4. auxiliary budget remains,
5. it is isolated from the main context and separately accounted.

Rules:

- Model selector: at most once before first main request; existing contract preserved.
- Onboarding: disabled for chat/tiny/small clear tasks; local ambiguity detector first; prefer direct `ask_user` without an LLM-generated questionnaire.
- Compaction: only after lossless options and break-even gate.
- No auxiliary call may be created solely to summarize a small tool result.

## 12. Finalization

The runtime may construct a deterministic concise final without a new provider request only when:

- the last assistant response already contains an adequate final, or
- the task is a deterministic mutation/check outcome whose user-facing facts are completely available and template-safe.

It must not fabricate explanation or diagnosis. Otherwise one bounded `finish` request is allowed.

## 13. Observability

Every plan records stable codes, for example:

```text
phase.inspect
reasoning.low.small
output.inspect.1200
budget.soft.prompt
recover.output_truncated
override.required_verification
finish.no_extra_request
```

No hidden automatic budget expansion is permitted.

## 14. Tests

- Table tests for every class/risk/phase/provider family.
- Property: increasing consumed budget never increases remaining budget.
- Property: same input -> same plan.
- Property: output cap never exceeds context allowance/provider ceiling.
- Property: valid transition graph only.
- Fuzz steering, repeated failure, truncation, and overflow arithmetic.
- Golden request traces for typo, CSS tweak, one-file bug, tic-tac-toe, auth bug, migration, provider failure.

