# US5 deterministic simulator matrix

Date: 2026-07-22
Network/provider calls: none
Result: PASS

Command:

```text
go test ./internal/orchestrator ./internal/gateway -run 'TestRequestPlanReasoningScales|TestRequestPlanDeescalates|TestPhaseOutputBudget|TestBalancedTruncationRetry|TestMiniMaxReplayPreserves|TestDeepSeekReplay|TestUnknownFamily' -count=1 -v
```

Validated matrix:

| Contract | Generic | DeepSeek | MiniMax |
|---|---|---|---|
| Phase output floor/default/ceiling and context clamp | pass | shared contract pass | pass with 1,024 thinking-safe floor |
| Low reasoning for chat/tiny and bounded user envelope | pass | pass through canonical request plan | pass through canonical request plan |
| Risk/failure escalation with later de-escalation | pass | pass | pass |
| Complete thinking/signature/tool-chain replay | strip legacy reasoning | deterministic required empty tool-turn key | preserve full `reasoning_details` and signature |
| One doubled-cap retry with `retryOf` | pass | pass by common controller | pass by common controller |
| Second truncation/partial tool-call safety | bounded stop; partial call never dispatched | same | same |

This is deterministic protocol evidence, not a paid performance result. SC-003/SC-004/SC-005 remain subject to paired provider-truth runs when the user authorizes paid testing.
