# Wiring Inventory Data — Ultimate Consolidation (feature 010, US5)

This is the checked-in inventory [wiring-inventory.md](contracts/wiring-inventory.md)
(WI-1) requires: every row of the ENTIRE advertised surface — registry tools,
synthetic tools, conditional/dynamic tools, slash commands (+ aliases),
keybindings, `contract.Settings` fields, `contract.AgentEvent` kinds (+ the
`Handoff` field), and `contract.Callbacks` members — with its advertised
behavior, how it is verified, and a status of exactly `wired` or `removed`
(WI-2, no third status).

**Format contract for the guard test** (`internal/tui/wiring_inventory_test.go`,
T040): every data row is `| Surface | Identifier | Advertised behavior |
Verified-by | Status |` with exactly 5 cells. The `##` sections below exist
for human readability only — the parser groups rows by the `Surface` column,
not by heading, so a row placed under the wrong heading would still be
checked correctly (don't rely on that; keep them aligned). Cell text must
never contain a literal `|` (it would be parsed as a column break) — use `/`
or `,` instead. `Status` must be exactly the lowercase word `wired` or
`removed`.

The guard test enumerates the LIVE surface at runtime — a real
`workspace.New(...).Tools()` + `Engine.SessionToolNames()`, a `go/ast` parse
of `runSlash`'s and `handleKey`'s actual switch statements, `reflect` over
the actual `contract.Settings`/`contract.Callbacks` structs, and a regex scan
of the actual `AgentEvent{Kind: "..."}` emission sites — and fails if the
live surface and this table diverge in EITHER direction (WI-3). It is not a
hardcoded mirror: run `go test ./internal/tui/... -run
TestWiringInventoryMatchesLiveSurface -v` after editing either the code or
this file.

## Tools — registry (12)

`internal/workspace/registry.go`'s `Workspace.Tools()`, wired into every
session via `orchestrator.NewRegistry(ws.Tools()...)`
(`internal/command/runtime_build.go`). Schema wiring for all 12 is proven
together by the session prefix golden.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Tool | list_files | List a directory (recursive optional, capped at maxEntries); use once to map a workspace | internal/workspace/workspace_test.go:181 (TestListGlobWriteAndGitToolBehaviors, recursive List) + internal/orchestrator/instructions_wiring_test.go:162 (TestWiring_PrefixBytesGolden, schema wiring) | wired |
| Tool | read_file | Read a UTF-8 file with line numbers; offset/limit page large files | internal/workspace/workspace_test.go:73 (TestReadEditPatchAndCheckpoint) + internal/workspace/read_suggest_test.go:36 (TestReadNotFoundSuggestsBasenameMatch) | wired |
| Tool | grep | Regex or literal search; file, line, and text results, capped at maxResults | internal/workspace/workspace_test.go:125 (TestGrepInvalidPatternGuidance) + internal/orchestrator/faultinjection_test.go:494 (FI-9, invalid-regex-through-real-grep, full Run) | wired |
| Tool | search_text | Fast literal text search (execSearch forces Literal=true onto the same Grep path) | internal/workspace/workspace_test.go:147 (TestGrepInvalidPatternGuidance's literal=true assertion, the exact code path execSearch uses) | wired |
| Tool | glob | Find files by doublestar glob pattern, capped at maxResults | internal/workspace/workspace_test.go:188 (TestListGlobWriteAndGitToolBehaviors) | wired |
| Tool | edit_file | Replace exact text in a file already read; returns a compact diff; requires a prior read for existing files | internal/workspace/workspace_test.go:91 (TestReadEditPatchAndCheckpoint) + internal/workspace/edit_mismatch_test.go:49,64,75,87 (not-found/ambiguous/idempotent classification) | wired |
| Tool | multi_edit | Apply several ordered exact replacements to one read file (max 30) | internal/workspace/edit_mismatch_test.go:94 (TestMultiEditPartialApplicationStaysSuccess) + edit_mismatch_test.go:114 (TestMultiEditAllMissesIsFailure) | wired |
| Tool | write_file | Write a file; overwriting an existing file requires a prior read (same permission-rule sentence as the system prompt, IS-5) | internal/workspace/workspace_test.go:196 (TestListGlobWriteAndGitToolBehaviors) + internal/orchestrator/cachehit_guard_test.go (full-Run write_file dispatch) | wired |
| Tool | apply_patch | Apply a standard unified diff to files already read; atomic rollback on partial failure | internal/workspace/workspace_test.go:99-107 (TestReadEditPatchAndCheckpoint's ApplyPatch section) | wired |
| Tool | run_shell | Run a shell command with streaming output and context-based cancellation | internal/workspace/workspace_test.go:255 (TestShellStreamingAndCancellation) | wired |
| Tool | git_status | Show concise `git status --short` | internal/workspace/workspace_test.go:207 (TestListGlobWriteAndGitToolBehaviors) | wired |
| Tool | git_diff | Show the workspace `git diff` (optionally staged/path/context-scoped) | internal/workspace/workspace_test.go:215 (TestListGlobWriteAndGitToolBehaviors) | wired |

## Tools — synthetic main-loop (8)

Wired directly in `sessionDefinitions()` (`internal/orchestrator/definitions.go`,
`memory.go`), dispatched via `executeOne`'s synthetic-tool switch
(`toolhandlers.go`) — never through `registry.Execute`, so none of the eight
ever appear in a subagent's real `Allowed` map (pinned by
`TestWiring_GeneralSubagentRealAllowedNeverIncludesSyntheticTools`).

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Tool | update_plan | Create/update the task plan; in orchestrated planning every step needs a target, an [F#] citation, and a Verify: check; max 12 steps | internal/orchestrator/plan_lifecycle_test.go:57 (TestPlanLifecycleProceedThenFinished) + instructions_wiring_test.go:108 (TestWiring_PlanStepExamplePassesRealValidator) + faultinjection_test.go (FI-8, 13-step rejection) | wired |
| Tool | ask_user | Ask 1-3 blocking multiple-choice questions | internal/tui/askuser_safety_test.go:45 (TestAskEmptyChoicesDoesNotPanic, the Callbacks.Ask consumer e.askUser dispatches to) + instructions_wiring_test.go:162 (schema wiring) | wired |
| Tool | propose_changes | Request approval before broad or risky edits | internal/orchestrator/mode_scenario_test.go:51 (TestProposeChangesNonInteractiveIsHonest) | wired |
| Tool | save_memory | Save one durable project fact to the memory-store index, or (with topic) into a topic file plus a one-line index pointer; 500-char cap; repeats dedupe as already known | internal/orchestrator/memory_test.go:148 (TestSaveMemoryToolApprovalGateParity) + memory_test.go:191 (TestSaveMemoryToolAutoAcceptSaves) + memory_test.go (TestSaveMemoryTopicRouting) | wired |
| Tool | recall_memory | Read one project-memory topic file whose pointer appears in the always-loaded index (on-demand detail) | internal/orchestrator/memory_test.go (TestRecallMemoryRoundTrip) | wired |
| Tool | edit_memory | Update or delete exactly one saved memory entry (index or topic file) by matching enough of its line; empty new deletes; deleting a topic's last entry dissolves the file and its index pointer; same approval gate as save_memory | internal/orchestrator/memory_test.go (TestEditMemoryReplaceAndDelete + TestEditMemoryDissolvesEmptyTopic + TestEditMemoryApprovalGateParity) | wired |
| Tool | run_subagent | Run an isolated subagent (explore/plan/review/general); only its final report returns | internal/orchestrator/subagent_budget_test.go:15 (TestLowEffortGrantsOneSubagentRun) + instructions_wiring_test.go:58 (TestWiring_CapabilityReferenceMatchesRealSubagentSpecs) | wired |
| Tool | exit_plan_mode | Signal the plan is complete and ready for execution handoff; a harmless no-op outside plan mode | internal/orchestrator/plan_mode_phase3_test.go:101 (TestExitPlanModeFinalizesPlanTask) + pipeline_polish_test.go:192 (no-op outside plan mode) | wired |

## Tools — conditional and dynamic (2)

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Tool | web_search | Search the live web for current/niche facts; gated on a one-time, fingerprint-cached provider probe at session build (internal/command/runtime_build.go's webSearchAvailable, gated on BaseURL+APIKey both being set); verifying interface is the gateway's WebSearchTool, not a live network call in CI | internal/tui/wiring_inventory_test.go (TestWiringInventoryMatchesLiveSurface's structural gateway.WebSearchTool{}.Definition() check) + internal/gateway/cost_and_web_test.go:11 (TestWebSearchNeverLeaksProvider, Search behavior) + manual: the probe/fingerprint gate itself has no automated test and is verified by a live session with a provider that does/does not advertise web_search | wired |
| Tool | mcp__* (dynamic) | Any number of mcp__<server>__<tool> definitions, discovered per configured MCP server at session start; BaseDefinitions excludes them (byte-stable session prefix) while Definitions/MCPDefinitions includes them | internal/tui/wiring_inventory_test.go (TestWiringInventoryMatchesLiveSurface's mcpRegistrySupportsDynamicTools structural round-trip) + internal/mcpclient/manager_test.go:63 (TestManagerStdioDiscoveryExecutionAndRefresh) + manager_test.go:156 (TestPinnedSurfaceAppearsAtBoundaryThenLoadsBeforeHandshake) | wired |

## Slash commands — canonical (16)

The command palette (`var commands` in `internal/tui/model.go`); every entry
is driven end to end through the real Enter-key path by one shared test.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Slash Command | /context | Inspect context usage (opens the context-report modal) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + context_panel_test.go:57-176 (formatContextReport content) | wired |
| Slash Command | /errors | Show this session's harness-friction events (opens the harness-events modal) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + harness_events_test.go (formatHarnessEvents content) | wired |
| Slash Command | /compact | Compact the conversation (summarize earlier turns) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) | wired |
| Slash Command | /rewind | Restore the latest checkpoint | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree, Actions.Rewind wired) | wired |
| Slash Command | /reasoning | Set reasoning effort (low-max); bare command opens a choice modal | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + slash_alias_test.go:22 (TestEffortAliasMatchesCanonicalCommand) | wired |
| Slash Command | /goal | Set/inspect/clear an autonomous goal | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) | wired |
| Slash Command | /permissions | Change permission mode (normal/auto-accept); bare command opens a choice modal | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + slash_alias_test.go:44 (TestModeAliasMatchesCanonicalCommand) | wired |
| Slash Command | /model | Choose or refresh main/subagent models; mid-session switch warns first | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + model_switch_test.go:56,76,93,128,146 (chooseModel behaviors) | wired |
| Slash Command | /login | Store the API key (interactive builds only) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + command_visibility_test.go:12 (TestCommandVisibilityTracksSignIn) | wired |
| Slash Command | /logout | Clear the stored API key | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + command_visibility_test.go:12 (TestCommandVisibilityTracksSignIn) | wired |
| Slash Command | /usage | View account usage (signed-in only) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + command_visibility_test.go:12 (TestCommandVisibilityTracksSignIn) | wired |
| Slash Command | /diff | Summarize the current git diff (submits a fixed read-only prompt) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) | wired |
| Slash Command | /new | Start a new session | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) | wired |
| Slash Command | /resume | Resume a workspace session (lists sessions, or resumes by id) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + slash_alias_test.go:66 (TestSessionAliasesMatchCanonicalCommand) | wired |
| Slash Command | /skills | Assign skills to the next prompt (multi-select modal) | internal/tui/tui_test.go:298 (TestMCPModalAndSkillsSelection) | wired |
| Slash Command | /mcp | Manage MCP servers (live status modal) | internal/tui/tui_test.go:298 (TestMCPModalAndSkillsSelection) + tui_test.go:563 (TestMCPModalRefreshesUntilTerminal) | wired |
| Slash Command | /paste | Inspect or remove pasted blocks (only visible when a paste is active) | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + commandVisible's len(m.activePastes())>0 gate | wired |

## Slash commands — aliases (4)

Not in the palette (`commandMatches` never surfaces them, so
`TestEverySlashCommandFlowIsCrashFree` cannot reach them), but recognized by
`runSlash`'s switch as the same case as their canonical name.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Slash Command | /effort | Alias of /reasoning (`case "/reasoning", "/effort":`) | internal/tui/slash_alias_test.go:22 (TestEffortAliasMatchesCanonicalCommand) | wired |
| Slash Command | /mode | Alias of /permissions (`case "/permissions", "/mode":`) | internal/tui/slash_alias_test.go:44 (TestModeAliasMatchesCanonicalCommand) | wired |
| Slash Command | /sessions | Alias of /resume (`case "/resume", "/sessions", "/session":`) | internal/tui/slash_alias_test.go:66 (TestSessionAliasesMatchCanonicalCommand) | wired |
| Slash Command | /session | Alias of /resume (`case "/resume", "/sessions", "/session":`) | internal/tui/slash_alias_test.go:66 (TestSessionAliasesMatchCanonicalCommand) | wired |

## Keybindings (14)

`handleKey`'s switch on `key.String()` (`internal/tui/keys.go`), plus the
alt+&lt;digit&gt; direct-agent-select block matched by prefix outside the
switch.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Keybinding | ctrl+c | Copy the active transcript selection, else stop the running task, else clear the composer, else quit | internal/tui/selection_test.go:116 (Ctrl+C copies a selection) + keys.go's busy/composer fallthrough (manual: exercised in every interactive session) | wired |
| Keybinding | ctrl+d | Quit, but only when the composer is empty and no task is running | internal/tui/keys_test.go:19 (TestCtrlDQuitsOnlyWhenIdleAndEmpty) | wired |
| Keybinding | esc | Clear a selection, else close the agent view, else stop the running task, else clear the composer | internal/tui/selection_test.go:158 (Esc dismisses a selection) + tui_test.go's agent-view/busy Esc paths (manual: exercised across the modal/agent-view tests) | wired |
| Keybinding | ctrl+p | Open command search (fills the composer with "/") | internal/tui/keys_test.go:52 (TestCtrlPOpensCommandSearch) | wired |
| Keybinding | ctrl+s | Open /resume when idle; blocked with a notice while a task is running | internal/tui/keys_test.go:75 (TestCtrlSOpensResumeWhenIdleAndBlocksWhenBusy) | wired |
| Keybinding | shift+tab | Cycle permission mode (normal <-> auto-accept) | internal/tui/keys_test.go:101 (TestShiftTabCyclesPermissionMode) | wired |
| Keybinding | ctrl+t | Toggle the live to-do checklist panel | internal/tui/render_todos_test.go (TestCtrlTTogglesTodoPanel) | wired |
| Keybinding | tab | Complete the highlighted palette command when the composer holds a slash command, else cycle the viewed subagent | internal/tui/keys_test.go:121 (TestTabCompletesCommandWhenPaletteOpen) + keys_test.go:136 (TestTabCyclesAgentWhenPaletteClosed) | wired |
| Keybinding | enter | Run the resolved slash command, or submit plain text as a prompt | internal/tui/tui_test.go:496 (TestEverySlashCommandFlowIsCrashFree) + tui_test.go:234 (TestSlashCommandResolvesHighlightedMatch) | wired |
| Keybinding | up | Browse the command palette upward, else recall input history, else scroll the transcript up | internal/tui/tui_test.go:339,440 (palette/history navigation) | wired |
| Keybinding | down | Browse the command palette downward, else recall input history, else scroll the transcript down | internal/tui/tui_test.go:339,444 (palette/history navigation) | wired |
| Keybinding | pgup | Page the transcript up | internal/tui/tui_test.go:227 (TestScrollUpFromBottomSticks) | wired |
| Keybinding | pgdown | Page the transcript down | internal/tui/keys_test.go:164 (TestPageDownScrollsViewportDown) | wired |
| Keybinding | alt+<1-9> | Jump directly to the Nth subagent's view (out-of-range digit is a no-op) | internal/tui/keys_test.go:191 (TestAltDigitSelectsAgentByIndex) | wired |

## Settings fields (16)

`contract.Settings`, dotted paths follow the real `json` tags. `ui.density`
was removed in this feature (WI-5) — see the "removed" row below and
`specs/010-ultimate-consolidation/removal-ledger.md` RL-026.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Settings Field | version | Settings schema version; only 1 is accepted | internal/state/state_test.go:53 (TestLegacySettingsAndSecrets) + state/config.go's ValidateSettings version check | wired |
| Settings Field | provider.type | Provider API shape; only "openai-compatible" is accepted | internal/state/config.go's ValidateSettings provider.type check (manual: no negative-value test exists; the DefaultSettings/positive path is exercised by every state test) | wired |
| Settings Field | provider.baseUrl | The gateway base URL | internal/state/state_test.go:53 (TestLegacySettingsAndSecrets) | wired |
| Settings Field | provider.activeModelId | The main-agent model id | internal/state/config_defaults_test.go:19 (TestAutoAssignPrefersProMainFlashSub) + internal/tui/model_switch_test.go (chooseModel "main") | wired |
| Settings Field | provider.subagentModelId | The subagent model id | internal/state/config_defaults_test.go:19 (TestAutoAssignPrefersProMainFlashSub) + internal/tui/model_switch_test.go:56 (chooseModel "subagent") | wired |
| Settings Field | provider.models | The configured model catalog | internal/state/config_defaults_test.go (AutoAssign family) + internal/tui/model_switch_test.go | wired |
| Settings Field | permissionMode | normal (confirm mutations) or auto-accept | internal/tui/keys_test.go:101 (TestShiftTabCyclesPermissionMode) + slash_alias_test.go:44 (TestModeAliasMatchesCanonicalCommand) | wired |
| Settings Field | effort | Reasoning effort: low/medium/high/max | internal/tui/slash_alias_test.go:22 (TestEffortAliasMatchesCanonicalCommand) + internal/state/config_defaults_test.go:10 (TestDefaultEffortIsHigh) | wired |
| Settings Field | reviewGating | Review-gate mode (feature 011): off/conservative/default; explicit review requests always run | internal/orchestrator/reviewgate_test.go:202 (TestNormalizeReviewGating) + reviewgate_test.go:26 (TestDecideHardRules) | wired |
| Settings Field | theme | The color theme name | internal/tui/theme_test.go:38 (TestThemePaletteIsSingleSource) | wired |
| Settings Field | shell.preferred | Preferred shell: auto/pwsh/powershell/cmd/sh | internal/workspace/workspace_test.go:255 (TestShellStreamingAndCancellation, ShellRunner.Preferred) | wired |
| Settings Field | shell.timeoutMs | Shell command timeout in milliseconds | internal/workspace/workspace_test.go:255 (TestShellStreamingAndCancellation) | wired |
| Settings Field | shell.outputLimit | Shell output byte cap | internal/workspace/workspace_test.go:255 (TestShellStreamingAndCancellation) | wired |
| Settings Field | rtl.mode | Bidi rendering mode: auto/visual/native/off | internal/tui/rtl_fuzz_test.go:58 (TestRTLLiveSettingChange) | wired |
| Settings Field | rtl.align | Bidi alignment: auto/right/left | internal/tui/rtl_fuzz_test.go:58 (TestRTLLiveSettingChange) | wired |
| Settings Field | ui.borderMode | Border glyph set: auto/unicode/ascii | internal/tui/theme_test.go:70 (TestThemeCarriesResolvedTables, newTheme reads UI.BorderMode into asciiGlyphs) | wired |
| Settings Field | ui.density | UI density (compact/comfortable); had a default, a validator, and a config-set case but zero renderer consumers anywhere in internal/tui | specs/010-ultimate-consolidation/removal-ledger.md RL-026 (field, default, validator clause, and "uiDensity" SetConfig case all removed) | removed |

## AgentEvent kinds (6)

`contract.AgentEvent.Kind` values emitted by `internal/orchestrator/subagent.go`,
consumed by `internal/tui/ingest.go`'s `applyAgent` switch.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| AgentEvent Kind | start | A subagent run begins; registers its agentView chip | internal/tui/application_state_test.go:62 (TestSubagentRunningState) + ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |
| AgentEvent Kind | tool_start | The subagent begins a tool call; appends a running toolView | internal/tui/ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |
| AgentEvent Kind | tool_end | The subagent's tool call finished; finalizes the toolView (ok/fail) | internal/tui/ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |
| AgentEvent Kind | text | The subagent produced assistant text; appends an assistant item | internal/tui/ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |
| AgentEvent Kind | usage | The subagent's token usage snapshot | internal/tui/ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |
| AgentEvent Kind | done | The subagent run finished; sets status and appends its report | internal/tui/ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |

## AgentEvent fields (1)

`CallID`/`Turns`/`ToolCalls` were removed in US4 (RL-020/RL-021, no
consumer). `Handoff` is the one field that is NOT part of the `Kind` switch
above — it carries the rendered launch contract for an audit consumer the
TUI itself never reads.

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| AgentEvent Field | Handoff | The rendered subagent launch contract, for delegation-benchmark auditing only (the TUI does not render it) | benchmarks/delegationbench/audit.go:133 (handoffCompliant(event.Handoff)) + specs/010-ultimate-consolidation/removal-ledger.md RL-021 | wired |

## Callbacks members (15)

`contract.Callbacks`; producers in `internal/orchestrator`, consumed by
`internal/tui/bridge.go`'s `Callbacks()` (interactive) or
`internal/command/root.go`'s `newConsoleCallbacks` (the `--simple`/headless
interface — the subset it wires is the "verifying interface" WI-4 asks
conditional surfaces to name).

| Surface | Identifier | Advertised behavior | Verified-by | Status |
|---|---|---|---|---|
| Callbacks Member | Status | One-line status text (e.g. "Thinking...") | internal/orchestrator/turnloop.go:413 (producer) + internal/command/root.go's newConsoleCallbacks Status (--simple interface) | wired |
| Callbacks Member | Notice | A transient user-facing announcement that outlives the next status update | internal/orchestrator/pipeline.go:90,109,145,318 (producer) + internal/tui/bridge.go's Notice->noticeMsg | wired |
| Callbacks Member | Token | Streamed assistant answer tokens | internal/tui/bridge_test.go:7 (TestBridgeCoalescesAssistantChunks) | wired |
| Callbacks Member | ReasoningToken | Streamed reasoning/thinking tokens | internal/tui/bridge_test.go:7 (TestBridgeCoalescesAssistantChunks, cb.ReasoningToken) | wired |
| Callbacks Member | ToolStart | A tool call is starting (name + raw arguments) | internal/orchestrator/dispatch.go:100,182 (producer) + internal/tui/bridge_test.go:56 (TestBridgeFlushClearsBuffers) | wired |
| Callbacks Member | ToolOutput | Streaming tool/shell output chunks | internal/tui/bridge_test.go:28 (TestBridgeCoalescesToolOutputPerTool) | wired |
| Callbacks Member | ToolEnd | A tool call finished (name + final output) | internal/orchestrator/dispatch.go:242 (producer) + internal/tui/bridge_test.go:56 (TestBridgeFlushClearsBuffers) | wired |
| Callbacks Member | PlanUpdate | The plan changed (update_plan tool, or a pipeline phase writing it) | internal/orchestrator/phaserunners.go:310, toolhandlers.go:178 (producer) + internal/tui/ingest_test.go:75 (TestContextPlanAndMCPStatusMessagesUpdateModel) | wired |
| Callbacks Member | Usage | A token-usage delta for the in-flight request | internal/orchestrator/usage.go:118 (producer) + internal/tui/usage_display_test.go:10 (TestMixedProviderUsageRowsAndUnavailableCache) | wired |
| Callbacks Member | Context | The current context-window usage snapshot | internal/orchestrator/contextreport.go:152-153 (producer) + internal/tui/ingest_test.go:75 (TestContextPlanAndMCPStatusMessagesUpdateModel) | wired |
| Callbacks Member | Agent | A subagent lifecycle event (see AgentEvent kinds above) | internal/orchestrator/subagent.go:398-399 (producer) + internal/tui/ingest_test.go:18 (TestApplyAgentEventKindsUpdateAgentView) | wired |
| Callbacks Member | MCPStatus | An MCP server status change | internal/command/mcp_actions.go:179 (producer) + internal/tui/ingest_test.go:75 (TestContextPlanAndMCPStatusMessagesUpdateModel) | wired |
| Callbacks Member | TaskComplete | The task finished; carries the full TaskStats summary | internal/orchestrator/turnloop.go:189-190 (producer) + internal/tui/sweep_test.go:26 (TestTaskEndSweepCancelsRunningWork) | wired |
| Callbacks Member | Confirm | A blocking yes/no approval request (permission gate) | internal/command/runtime_build.go:333,388 (producer) + internal/tui/tui_test.go:537 (TestBridgeFallbackAndToolStreaming) | wired |
| Callbacks Member | Ask | A blocking multiple-choice question set (ask_user, onboarding) | internal/orchestrator/toolhandlers.go:213 (producer) + internal/tui/askuser_safety_test.go:45,66 (TestAskEmptyChoicesDoesNotPanic, TestAskMixedChoicesHandlesEmptySafely) | wired |

## Acceptance

- Row count: 92 (91 `wired` + 1 `removed`), matching WI-1's enumerated
  surface exactly (12 registry + 6 synthetic + 1 conditional + 1 dynamic
  tools = 20; 17 canonical + 4 alias slash commands = 21; 13 keybindings; 16
  Settings fields; 6 AgentEvent kinds; 1 AgentEvent field; 15 Callbacks
  members).
- Zero unresolved entries: every row has a non-empty Verified-by and a
  Status of exactly `wired` or `removed` (WI-2), checked mechanically by
  `internal/tui/wiring_inventory_test.go`.
- `go test ./internal/tui/... -run TestWiringInventoryMatchesLiveSurface -v`
  is the live proof this table is current.
