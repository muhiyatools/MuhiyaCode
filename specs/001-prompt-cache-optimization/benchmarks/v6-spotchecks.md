# V6 controlled integration spot-checks

Executed 2026-07-11 using local stdio/mock boundaries and the real MCP manager, orchestrator,
state, and TUI paths.

| V6 item | Evidence | Outcome |
|---|---|---|
| MCP session and pinned surface | `TestManagerStdioDiscoveryExecutionAndRefresh`, `TestPinnedSurfaceAppearsAtBoundaryThenLoadsBeforeHandshake` | PASS: stdio discovery/execution works; cached definitions load before handshake and refresh at a boundary. |
| `/mcp` list/test | `TestMCPModalAndSkillsSelection` plus manager boundary tests | PASS: viewing MCP state does not mutate the pinned registry. |
| Cross-day resume | `TestPromptStabilityDynamicDateLivesOnlyInTaskBrief`, `TestRestartDeterminism` | PASS: date is absent from the system prefix and restart rebuilds byte-identically. |
| `/compact` | `TestUserCompactRecordsInvalidation`, `TestContextReportShowsRatesAndInvalidations` | PASS: one `user-compact` event is recorded and rendered. |
| No cache reporting | `TestDisplayHonestyUnavailableAndReportedZero` | PASS: absent reporting renders `unavailable`; reported zero remains distinct. |
| External file edit | `TestInspectionFreshnessAndInvalidation` | PASS: changed fingerprints revoke stale coverage. |

All selected tests passed. Live provider acceptance remains separately measured under V5.
