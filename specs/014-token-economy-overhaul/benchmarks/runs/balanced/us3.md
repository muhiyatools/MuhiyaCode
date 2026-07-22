# US3 evidence virtualization — offline acceptance

Date: 2026-07-22
Provider requests: none
Result: PASS for deterministic SC-007 and prompt-growth checks

The 12-case `evidence-results.json` matrix passed locally across Go, npm, pytest, timeout, ANSI, invalid UTF-8, diff, file range, incomplete search, MCP JSON, and malformed JSON. Each raw payload was stored before reduction and round-tripped byte-for-byte by its content hash. Status, completeness, protected fixture facts, artifact identity, and configured card caps were asserted.

Commands:

```text
go test ./internal/evidence -run TestFeature014EvidenceFixtureMatrix -count=1 -v
go test ./internal/orchestrator -run TestBalancedVirtualizesVerboseOutputAndRawArtifactRoundTrips -count=1 -v
```

The verbose-result integration fixture measured:

| Model-visible result | Bytes |
|---|---:|
| Full raw result | 16,000 |
| Observation card | 3,973 |
| Reduction | 75.17% |

This exceeds the 50% prompt-growth reduction target for the verbose tool-turn fixture. It is deterministic local evidence, not provider token or credit evidence.
