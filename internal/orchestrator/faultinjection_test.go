package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// Feature 010 US2 (FR-005..008): the fault-injection suite. Every row drives a
// FULL Engine.Run against a scripted provider and passes through ONE shared
// invariant helper, so a new fault CANNOT be added without inheriting the
// three-outcome + liveness assertions (FI-1..4). New incidents become new rows.

// recoveryOutcome is the required resolution class for a fault.
type recoveryOutcome int

const (
	// outcomeAny accepts any of the three valid outcomes (used when a fault may
	// legitimately resolve more than one way depending on scripted timing).
	outcomeAny recoveryOutcome = iota
	outcomeGuidedSuccess
	outcomeRecordedDegradation
	outcomeUserDecision
)

func (o recoveryOutcome) String() string {
	switch o {
	case outcomeGuidedSuccess:
		return "guided-success"
	case outcomeRecordedDegradation:
		return "recorded-degradation"
	case outcomeUserDecision:
		return "user-decision"
	default:
		return "any"
	}
}

// faultCase is one chaos-catalog row.
type faultCase struct {
	name      string
	responses []contract.ChatResponse
	prompt    string
	tools     []contract.Tool
	setup     func(*Engine) // optional: mutate engine state before Run
	want      recoveryOutcome
}

// hardTurnCeiling bounds liveness: no fault may ride the loop up toward the old
// removed ~120 ceiling. Max effort's turn budget is 48; 80 leaves headroom for
// bounded retries while still failing a genuine loop.
const faultTurnCeiling = 80

// recoveryT is the minimal testing surface assertRecoveryInvariant needs.
// *testing.T satisfies it, so every real call site is unaffected; T023's
// mutation-guard self-test (FI-14) substitutes a fake recorder instead, since
// a real *testing.T subtest's failure would propagate to and fail the OUTER
// (real) test that is trying to prove the invariant catches an unbounded case.
type recoveryT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// assertRecoveryInvariant is the single guard every fault row passes through.
// It proves the harness never produces a fourth outcome (error loop, silent
// drop, stall, hard crash) and that the flow stays live.
func assertRecoveryInvariant(t recoveryT, engine *Engine, stats contract.TaskStats, runErr error, answer string, want recoveryOutcome) {
	t.Helper()

	// (1) No hard crash: Run returns nil, or a clean cancellation.
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		t.Fatalf("fault produced a hard error (not a recovery outcome): %v", runErr)
	}

	// (2) Classify by priority into exactly one of the three allowed outcomes.
	degraded := stats.TerminatedReason != "" ||
		engine.Lifecycle().HasAnyDegradation() ||
		historyHasLoopGuard(engine)
	decision := stats.PlanReady || stats.StopCause == contract.StopCauseUserStop
	var got recoveryOutcome
	switch {
	case decision:
		got = outcomeUserDecision
	case degraded:
		got = outcomeRecordedDegradation
	default:
		got = outcomeGuidedSuccess
	}
	if want != outcomeAny && got != want {
		t.Errorf("recovery outcome = %s, want %s (answer=%q terminated=%q stop=%q planReady=%v)",
			got, want, truncateForLog(answer), stats.TerminatedReason, stats.StopCause, stats.PlanReady)
	}

	// (3) Liveness: bounded turns.
	if stats.Turns >= faultTurnCeiling {
		t.Errorf("fault rode the loop to %d turns (ceiling %d) — a gate is not bounded", stats.Turns, faultTurnCeiling)
	}

	// (4) Liveness: no gate rejects forever — no identical substantial message
	// repeats more than 3× verbatim in history (the machine-checkable form of
	// FR-007). This is exactly the loop the H5 breaker and plan-bar waiver exist
	// to prevent.
	if msg, n := mostRepeatedMessage(engine); n > 3 {
		t.Errorf("message repeated %d× verbatim (a gate rejecting forever): %q", n, truncateForLog(msg))
	}
}

func historyHasLoopGuard(engine *Engine) bool {
	for _, m := range engine.history.All() {
		if strings.Contains(m.Content, "[loop guard]") || strings.Contains(m.Content, "[breaker]") ||
			strings.Contains(m.Content, "failed for two turns running") {
			return true
		}
	}
	return false
}

func mostRepeatedMessage(engine *Engine) (string, int) {
	counts := map[string]int{}
	worst, worstN := "", 0
	for _, m := range engine.history.All() {
		if len(m.Content) < 40 {
			continue // ignore short/boilerplate content
		}
		counts[m.Content]++
		if counts[m.Content] > worstN {
			worst, worstN = m.Content, counts[m.Content]
		}
	}
	return worst, worstN
}

func truncateForLog(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func runFaultCase(t *testing.T, tc faultCase) {
	t.Helper()
	provider := &scriptedProvider{responses: tc.responses}
	settings := engineSettings()
	tools := tc.tools
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "fault-" + tc.name, WorkspacePath: seededWorkspace(t)},
		Provider: provider,
		Registry: NewRegistry(tools...),
		Prompt:   PromptContext{},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if tc.setup != nil {
		tc.setup(engine)
	}
	prompt := tc.prompt
	if prompt == "" {
		prompt = "do the work"
	}
	answer, stats, runErr := engine.Run(context.Background(), prompt)
	assertRecoveryInvariant(t, engine, stats, runErr, answer, tc.want)
}

// schemaTool (T020/FI-5) exposes a REQUIRED field and an ENUM field so H1's
// missing-field and bad-enum branches (validateCallArgs) have something to
// validate against — recordingTool's schema declares no required/enum fields,
// so it cannot exercise those two checks.
type schemaTool struct{ name string }

func (t *schemaTool) Definition() contract.ToolDefinition {
	return definition(t.name, "test tool with a required field and an enum field", map[string]any{
		"path": map[string]any{"type": "string"},
		"mode": map[string]any{"type": "string", "enum": []any{"a", "b"}},
	}, []string{"path"})
}

func (t *schemaTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "configured.", nil
}

// TestFaultInjectionCatalog runs the chaos catalog. Rows that depend on the
// unified lifecycle (US1) are added in T009/US2; the rows below are the
// lifecycle-independent MISSING/PARTIAL cells the R5 matrix flagged.
func TestFaultInjectionCatalog(t *testing.T) {
	// FI-9: a REAL workspace.Grep (not a test stub) so the "invalid search
	// pattern" RE2-compile guidance actually fires, per contracts/fault-
	// injection.md's instruction to wire a real workspace registry.
	regexRoot := t.TempDir()
	regexTrust := workspace.NewMemoryTrustStore()
	if err := regexTrust.Trust(context.Background(), regexRoot); err != nil {
		t.Fatal(err)
	}
	regexWorkspace, err := workspace.New(regexRoot, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: regexTrust})
	if err != nil {
		t.Fatal(err)
	}

	cases := []faultCase{
		{
			// FI-5: malformed tool arguments through the main loop — the H1
			// validator guides a re-emit rather than looping or crashing. The
			// registered read_file gives H1 a schema (path:string) to catch the
			// wrong-type and bad-JSON payloads against.
			name:  "malformed-args-main-loop",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_file", `{"path": 123}`)}},
				{Content: "recovered and done"},
			},
			prompt: "read the file",
			want:   outcomeGuidedSuccess,
		},
		{
			// FI-8: oversized plan (13 steps) — update_plan rejects with merge
			// guidance, the model re-emits, no loop.
			name: "oversized-plan-rejected",
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "update_plan", oversizedPlanArgs(13))}},
				{Content: "acknowledged the limit and continued"},
			},
			prompt: "plan the work",
			want:   outcomeGuidedSuccess,
		},
		{
			// FI-10: a tool that keeps failing — the storm breaker / loop guard
			// bounds it and the task ends with the degradation recorded, never a
			// loop up to the ceiling.
			name:  "repeated-tool-failure-degrades",
			tools: []contract.Tool{&failingTool{name: "write_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("b", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("c", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("d", "write_file", `{"path":"x","content":"1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("e", "write_file", `{"path":"x","content":"1"}`)}},
				{Content: "gave up on the tool and finished directly"},
			},
			prompt: "write the file",
			want:   outcomeAny, // storm breaker → degradation, or the model's clean recovery
		},

		// --- T009 (FI-13): the seven pinned 008/009 live incidents, ported so
		// they inherit the invariant. Two are natural full-Run rows here
		// (content-bar deadlock, exit-plan desync); the other five need direct
		// engine-API calls (DiscardPlan/ApprovePipeline/restore) that are not
		// model tool calls, so they are dedicated Test functions below that
		// still call assertRecoveryInvariant.

		{
			// Pinned incident: "Tool exit_plan_mode failed: pipeline plan step 1
			// is missing an observable Acceptance:/Verify: check" repeating
			// forever (009), then later succeeding only on a second attempt. The
			// content bar is now NON-BLOCKING: the FIRST exit_plan_mode accepts with
			// a recorded plan-phase degradation and the plan reaches the human
			// approval pause — no rejection, no loop.
			name: "pinned-content-bar-deadlock-waives",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
				e.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "Run gofmt and go vet across the repository", Status: contract.PlanPending}}}
			},
			// Each attempt uses DIFFERENT arguments — identical verbatim repeats
			// would bounce off H2 (the failed-call short-circuit) instead of
			// re-reaching the content bar, which is a different (already-tested)
			// gate than the one this row locks in.
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("e1", "exit_plan_mode", `{"summary":"ready 1"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("e2", "exit_plan_mode", `{"summary":"ready 2"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("e3", "exit_plan_mode", `{"summary":"ready 3"}`)}},
			},
			want: outcomeUserDecision,
		},
		{
			// Pinned incident: the legacy planMode flag and the pipeline phase
			// desyncing stranded exit_plan_mode as a no-op live stall (009/010).
			// The unified lifecycle makes the desync unrepresentable; this locks
			// in the straightforward planning→approval exit through a FULL Run.
			name: "pinned-exit-plan-mode-escapes-planning",
			setup: func(e *Engine) {
				knowledge := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
				knowledge.AddPhaseReport("explore", "research", "research-scope", "finding", "scope", strings.Repeat("grounded finding ", 8))
				e.knowledge = knowledge
				e.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true}
				e.plan = contract.Plan{
					Steps: []contract.PlanStep{{Title: "[serial] internal/orchestrator/engine.go function Run [F1] Verify: go test ./internal/orchestrator", Status: contract.PlanPending}},
					Note:  "Verification:\n- go test ./...\nRisks:\n- none",
				}
			},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"research-backed plan ready"}`)}},
			},
			want: outcomeUserDecision,
		},

		// --- T020 (FI-5): malformed tool arguments through the untested
		// validateCallArgs branches — wrong primitive type (already covered by
		// malformed-args-main-loop above), missing required field, and a bad
		// enum value — through BOTH the main loop and the subagent scope.

		{
			name:  "malformed-args-missing-required-main-loop",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"mode":"a"}`)}},
				{Content: "Re-emitted with the required path and finished."},
			},
			prompt: "configure the widget",
			want:   outcomeGuidedSuccess,
		},
		{
			name:  "malformed-args-bad-enum-main-loop",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"path":"x","mode":"z"}`)}},
				{Content: "Re-emitted with a valid enum value and finished."},
			},
			prompt: "configure the widget",
			want:   outcomeGuidedSuccess,
		},
		{
			// "Delegate ... to a subagent" bumps classification to standard
			// (agentRE), giving the task a real subagent allowance — without it
			// run_subagent would be denied outright (agents=0) before ever
			// reaching H1 inside the subagent's own dispatch gate.
			name:  "malformed-args-wrong-type-subagent-scope",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"general","task":"configure the widget for this task"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"path":123}`)}},
				{Content: "Configured the widget correctly after the earlier malformed attempt."},
				{Content: "Delegated work complete."},
			},
			prompt: "Use a subagent to configure the widget.",
			want:   outcomeGuidedSuccess,
		},
		{
			name:  "malformed-args-missing-required-subagent-scope",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"general","task":"configure the widget for this task"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"mode":"a"}`)}},
				{Content: "Configured the widget correctly after the earlier malformed attempt."},
				{Content: "Delegated work complete."},
			},
			prompt: "Use a subagent to configure the widget.",
			want:   outcomeGuidedSuccess,
		},
		{
			name:  "malformed-args-bad-enum-subagent-scope",
			tools: []contract.Tool{&schemaTool{name: "configure_widget"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"general","task":"configure the widget for this task"}`)}},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "configure_widget", `{"path":"x","mode":"z"}`)}},
				{Content: "Configured the widget correctly after the earlier malformed attempt."},
				{Content: "Delegated work complete."},
			},
			prompt: "Use a subagent to configure the widget.",
			want:   outcomeGuidedSuccess,
		},

		// --- T021 (FI-6): per-phase subagent budget driven past the cap.

		{
			// The default "do the work" prompt classifies chat (agents=0), so the
			// floored allowance (pipeline mode guarantees >=1) is exactly 1 —
			// consumed by one explore call, then exit_plan_mode's content bar
			// finds the phase allowance exhausted and records a real research
			// degradation (pipeline.go's phaseAgentAllowanceExhausted branch)
			// before waiving the research-grounding gap and reaching approval.
			name: "budget-exhausted-research-degrades-then-decides",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true}
				e.plan = contract.Plan{
					Steps: []contract.PlanStep{{Title: "[serial] internal/orchestrator/engine.go function Run [F1] Verify: go test ./internal/orchestrator", Status: contract.PlanPending}},
					Note:  "Verification:\n- go test ./...\nRisks:\n- none",
				}
			},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"explore","task":"investigate the missing research gap"}`)}},
				{}, // explore subagent returns nothing — unusable, but still spends the floored 1-run Planning-phase allowance
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"ready"}`)}},
			},
			prompt: "do the work",
			want:   outcomeUserDecision, // decision takes priority in the classifier; HasAnyDegradation() is also true here
		},
		{
			// The auto-run validation battery (Depth=full) spends the floored
			// 1-run Validating-phase allowance on a single review pass that
			// fails; with no headroom left for the rescope-once retry, the
			// degradation records immediately. The model then confirms
			// validation directly (a real check call), which is the only way to
			// escape LifecycleValidating's "blocked until confirmed" gate.
			name:  "budget-exhausted-validate-degrades",
			tools: []contract.Tool{&recordingTool{name: "run_shell"}},
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
				e.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "ship the fix", Status: contract.PlanCompleted}}}
			},
			responses: []contract.ChatResponse{
				{Content: "Checked but could not confirm every acceptance criterion. VERDICT: FAIL more verification needed."},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("c", "run_shell", `{"command":"go test ./..."}`)}},
				{Content: "Verification passed directly after the subagent's inconclusive pass."},
			},
			prompt: "do the work",
			want:   outcomeRecordedDegradation,
		},

		// --- T022 (FI-7): interrupt/restart at every lifecycle state, driven
		// through a full Run to one of the three outcomes. Every row starts with
		// its plan step already Completed so hasIncompletePlan() never forces
		// the bounded "[continue]"/"[validation gate]" nudge loop — that
		// mechanism is exercised on its own terms by other rows.

		{
			name: "resume-research-state-reaches-decision",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecycleResearch, Depth: PipelineDepthFull}
				e.plan = contract.Plan{
					Steps: []contract.PlanStep{{Title: "[serial] internal/orchestrator/engine.go function Run [F1] Verify: go test ./internal/orchestrator", Status: contract.PlanCompleted}},
					Note:  "Verification:\n- go test ./...\nRisks:\n- none",
				}
			},
			responses: []contract.ChatResponse{
				{Content: "Findings: internal/orchestrator/engine.go function Run owns the main loop; verified against the source. Risks: none identified."},
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"ready"}`)}},
			},
			want: outcomeUserDecision,
		},
		{
			name: "resume-planning-state-reaches-decision",
			setup: func(e *Engine) {
				knowledge := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
				knowledge.AddPhaseReport("explore", "research", "research-scope", "finding", "scope", strings.Repeat("grounded finding ", 8))
				e.knowledge = knowledge
				e.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true}
				e.plan = contract.Plan{
					Steps: []contract.PlanStep{{Title: "[serial] internal/orchestrator/engine.go function Run [F1] Verify: go test ./internal/orchestrator", Status: contract.PlanCompleted}},
					Note:  "Verification:\n- go test ./...\nRisks:\n- none",
				}
			},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"ready"}`)}},
			},
			want: outcomeUserDecision,
		},
		{
			name: "resume-approval-state-reaches-guided-success",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecycleApproval, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
				e.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "saved step", Status: contract.PlanCompleted}}}
			},
			responses: []contract.ChatResponse{
				{Content: "Awaiting your approval before implementing."},
			},
			want: outcomeGuidedSuccess,
		},
		{
			name: "resume-implementing-state-reaches-guided-success",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecycleImplementing, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true}
				e.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "ship it", Status: contract.PlanCompleted}}}
			},
			responses: []contract.ChatResponse{
				{Content: "Checked the completed implementation. VERDICT: PASS."},
				{Content: "Pipeline complete: implementation and validation both confirmed."},
			},
			want: outcomeGuidedSuccess,
		},
		{
			name: "resume-validating-state-reaches-guided-success",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
				e.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "ship it", Status: contract.PlanCompleted}}}
			},
			responses: []contract.ChatResponse{
				{Content: "Checked the completed implementation. VERDICT: PASS."},
				{Content: "Pipeline complete: validation confirmed."},
			},
			want: outcomeGuidedSuccess,
		},

		{
			// FI-9: invalid regex through a FULL Run against a REAL workspace grep
			// tool — regexp.Compile fails and the harness returns the literal/
			// escape guidance rather than crashing or looping.
			name:  "invalid-regex-through-real-grep",
			tools: regexWorkspace.Tools(),
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("g1", "grep", `{"pattern":"[","path":"."}`)}},
				{Content: "The pattern was invalid RE2 syntax; used a literal search instead and confirmed the result."},
			},
			prompt: "search the code for TODO markers",
			want:   outcomeGuidedSuccess,
		},

		// --- T022 (FI-10): blocked capabilities — bounded escalation, never a
		// retry loop.

		{
			// A mutating shell call inside a manual (non-orchestrated) plan-mode
			// state is blocked with plan-mode-specific guidance; the model backs
			// off instead of retrying.
			name: "blocked-mutating-shell-in-plan-mode",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecyclePlanning} // Depth "" = manual plan mode, not orchestrated
			},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_shell", `{"command":"npm install"}`)}},
				{Content: "npm install is blocked in plan mode; noting the dependency for the implementation phase instead."},
			},
			prompt: "set up the project",
			want:   outcomeGuidedSuccess,
		},
		{
			// H8/T034: an unknown tool name gets a nearest-match suggestion
			// instead of a bare failure, and the model self-corrects in one step.
			name:  "blocked-unknown-tool-nearest-suggestion",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_fiel", `{"path":"a.txt"}`)}},
				{Content: "Corrected the tool name and confirmed the read."},
			},
			prompt: "read the file",
			want:   outcomeGuidedSuccess,
		},
		{
			// H8: a dead/disconnected MCP tool call is named clearly so the model
			// does not waste turns retrying it.
			name: "blocked-dead-mcp-tool",
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "mcp__deploy__run", `{}`)}},
				{Content: "The MCP deploy tool is disconnected; falling back to a manual status report."},
			},
			prompt: "deploy the service",
			want:   outcomeGuidedSuccess,
		},

		{
			// FI-11: one implementation group succeeds via subagent, the other's
			// subagent returns nothing (unusable) — the degradation records and
			// the SUCCESSFUL step's completion is preserved (never rolled back)
			// while the model absorbs the failed step directly, and validation
			// still completes the pipeline.
			name: "implementation-partial-preserved-after-subagent-failure",
			setup: func(e *Engine) {
				e.lifecycle = Lifecycle{State: contract.LifecycleImplementing, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true}
				e.plan = contract.Plan{
					Steps: []contract.PlanStep{
						{Title: "Fix the login token validator", Status: contract.PlanPending},
						{Title: "Update the dashboard error banner", Status: contract.PlanPending},
					},
					Note: "Verification:\n- go test ./...\nRisks:\n- none",
				}
			},
			responses: []contract.ChatResponse{
				{Content: "Changes made: fixed the login token validator. Validation performed: ran the focused unit test. Problems: none. Remaining concerns: none."},
				{}, // second group's subagent returns nothing — an unusable report
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("u", "update_plan", `{"steps":[{"title":"Fix the login token validator","status":"completed"},{"title":"Update the dashboard error banner","status":"completed"}]}`)}},
				{Content: "Checked the recovered change directly. VERDICT: PASS."},
				{Content: "Pipeline complete: step 1 via subagent, step 2 recovered directly after its subagent returned nothing; validation passed."},
			},
			prompt: "Fix the authentication login flow bug in the dashboard module.",
			want:   outcomeRecordedDegradation,
		},

		{
			// FI-12/FR-004b: a turn that announces its next action without
			// calling a tool ("Let me fix:") is bounded to 2 retries, then
			// accepted as the final answer rather than looping forever.
			name:  "trailing-intent-bounded-retry",
			tools: []contract.Tool{&recordingTool{name: "read_file"}},
			responses: []contract.ChatResponse{
				{ToolCalls: []contract.ToolCall{contract.NewToolCall("a", "read_file", `{"path":"a.txt"}`)}},
				{Content: "Found the issue. Now let me fix the remaining validation gap:"},
				{Content: "Still need to patch the edge case. Let me handle that next:"},
				{Content: "One more pass needed. I'll wrap up the fix now:"},
			},
			prompt: "investigate and fix the bug",
			want:   outcomeGuidedSuccess,
		},

		{
			// T021/FI-6: the turn-governor's escalation-then-final-step notices
			// fire (bounding a task that outgrows its brief), and when the model
			// still does not converge afterward, the H5 distinct-failure
			// terminator (not the raw 120-turn ceiling) is what actually stops
			// it — the composite proves both bounded-recovery mechanisms compose
			// into a single recorded-degradation outcome rather than a runaway
			// loop.
			name:      "governor-escalation-then-failure-terminator-degrades",
			tools:     []contract.Tool{&recordingTool{name: "read_file"}, &failingTool{name: "write_file"}},
			responses: governorEscalationResponses(),
			want:      outcomeRecordedDegradation,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runFaultCase(t, tc) })
	}
}

// governorEscalationResponses (T021/FI-6) scripts five harmless successful
// turns — enough for the ClassChat default prompt's turnCap=6 to trigger both
// the "[governor] Two steps remain" (turn 4) and "[governor] Final step: do
// not call tools" (turn 6) notices — followed by repeated distinct-tool
// failures (reusing the H5 fixture from h5_circuit_breaker_test.go) that the
// model keeps making in defiance of the final-step notice, until the H5
// failure-terminator fires and force-finalizes the task.
func governorEscalationResponses() []contract.ChatResponse {
	var out []contract.ChatResponse
	for i := 0; i < 5; i++ {
		out = append(out, contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("r", "read_file", `{"path":"f.txt"}`)}})
	}
	out = append(out, repeatedFailingCalls(4)...)
	return out
}

func oversizedPlanArgs(n int) string {
	var b strings.Builder
	b.WriteString(`{"steps":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"title":"step `)
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString(`","status":"pending"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// --- T009 (FI-13): the remaining pinned 008/009 incidents. Each of these
// exercises an engine API a user action drives directly (a TUI slash command,
// the approve button, a process restart) rather than a model tool call, so a
// full Engine.Run cannot express the regression itself — the task's own
// contract permits a focused scenario here. Each still finishes by driving a
// real Engine.Run through assertRecoveryInvariant, proving the engine stays
// live and bounded on the very next task after the pinned incident's gate.

// TestFaultPlanClearReleasesPipelineThenStaysLive locks in the /plan-clear
// deadlock fix (009): discarding the plan releases the pipeline gating that
// guarded it (previously a hard deadlock — pipeline stayed at approve while
// proceed was guarded against Discarded).
func TestFaultPlanClearReleasesPipelineThenStaysLive(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "Hello."}}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "fault-plan-clear", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleApproval, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "saved step", Status: contract.PlanPending}}}

	engine.DiscardPlan()
	if engine.LifecycleState() != contract.LifecycleDiscarded || engine.pipelineActive() {
		t.Fatalf("discard did not release the pipeline: %+v", engine.Lifecycle())
	}
	if block := engine.pipelineBlock(); block != "" {
		t.Fatalf("discarded plan must not keep gate text, got %q", block)
	}

	answer, stats, runErr := engine.Run(context.Background(), "hi")
	assertRecoveryInvariant(t, engine, stats, runErr, answer, outcomeGuidedSuccess)
}

// TestFaultStaleApprovePipelineParksThenStaysLive locks in the stale-approve
// leak fix (009): a pipeline stranded at the approval pause is parked when an
// unrelated direct task arrives — its gate text and mutation block must not
// leak into the new task — while the saved plan stays pending and resumable.
func TestFaultStaleApprovePipelineParksThenStaysLive(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "4"}}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "fault-stale-approve", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleApproval, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "saved step", Status: contract.PlanPending}}}

	answer, stats, runErr := engine.Run(context.Background(), "what's 2+2?")
	assertRecoveryInvariant(t, engine, stats, runErr, answer, outcomeGuidedSuccess)
	if !engine.PendingPlan() || engine.LifecycleState() != contract.LifecyclePending {
		t.Fatalf("stale approval leaked instead of parking: pending=%v state=%s", engine.PendingPlan(), engine.LifecycleState())
	}
}

// TestFaultTerminalPlanApprovalNoOpsThenStaysLive locks in the revival guard
// (009): a terminal (superseded/discarded) plan can never be approved into
// execution. With the unified lifecycle the stale-approve desync is
// unrepresentable — a terminal plan is simply not in the approval state — so
// ApprovePipeline no-ops and the state stays terminal. A genuinely new,
// unrelated task afterward still starts a fresh pipeline cleanly.
func TestFaultTerminalPlanApprovalNoOpsThenStaysLive(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleSuperseded, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}

	if err := engine.ApprovePipeline(context.Background()); err != nil {
		t.Fatalf("terminal-plan approval must no-op, got error: %v", err)
	}
	if engine.LifecycleState() != contract.LifecycleSuperseded {
		t.Fatalf("terminal plan was approved/overwritten: %s", engine.LifecycleState())
	}
	// Terminal states have no outgoing edge in the shared table either.
	if err := engine.transitionLifecycle(context.Background(), contract.LifecycleImplementing); err == nil {
		t.Fatal("terminal plan accepted an illegal transition")
	}

	// FI-13: a brand-new, unrelated, plan-worthy task must still start a fresh
	// pipeline cleanly — the stale terminal state must not leak into it.
	// EffortLow floors the auto-research battery to exactly one scope so the
	// script below stays a fixed, small size regardless of class assessment.
	settings2 := engineSettings()
	settings2.Effort = contract.EffortLow
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{}, // auto research subagent — empty/unusable is fine, only liveness matters here
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("u", "update_plan", `{"steps":[{"title":"[serial] internal/orchestrator/engine.go function Run [F1] Verify: go test ./internal/orchestrator","status":"pending"}],"note":"Verification:\n- go test ./...\nRisks:\n- none"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"ready"}`)}},
	}}
	engine2, err := NewEngine(EngineConfig{Settings: &settings2, Session: contract.Session{ID: "fault-terminal-revival-2", WorkspacePath: seededWorkspace(t)}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	answer, stats, runErr := engine2.Run(context.Background(), "Fix the authentication login flow bug in the dashboard module.")
	assertRecoveryInvariant(t, engine2, stats, runErr, answer, outcomeUserDecision)
}

// TestFaultPipelineDepthPersistsAndResumesLive locks in the depth-loss fix
// (009): a light pipeline resumes light (previously restore hardcoded full and
// spawned subagents the task never scoped). Beyond the structural persistence
// check, this drives the restored snapshot through a real Engine.Run.
func TestFaultPipelineDepthPersistsAndResumesLive(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleImplementing, Depth: PipelineDepthLight, ResearchCompleted: true, PlanWritten: true, Approved: true}
	snapshot := engine.planStateSnapshot()
	if snapshot.PipelineDepth != PipelineDepthLight {
		t.Fatalf("snapshot depth = %q, want light", snapshot.PipelineDepth)
	}
	restoredPlan := contract.Plan{Steps: []contract.PlanStep{{Title: "s", Status: contract.PlanCompleted}}}
	restored, _ := restoreLifecycle(snapshot, restoredPlan)
	if restored.Depth != PipelineDepthLight || !restored.Orchestrated() || restored.State != contract.LifecycleImplementing {
		t.Fatalf("light pipeline must resume orchestrated light at implementing, got %+v", restored)
	}

	// FI-13: a FRESH engine restarted from exactly this snapshot must be live
	// and bounded, not just structurally correct.
	settings2 := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Implemented directly per the light-depth plan; verification already passed."},
	}}
	resumedSnapshot := contract.PlanStateSnapshot{State: contract.LifecycleImplementing, PipelineDepth: PipelineDepthLight}
	engine2, err := NewEngine(EngineConfig{
		Settings: &settings2, Session: contract.Session{ID: "fault-depth-resume", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(),
		InitialPlan: restoredPlan, InitialPlanState: &resumedSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, stats, runErr := engine2.Run(context.Background(), "continue")
	assertRecoveryInvariant(t, engine2, stats, runErr, answer, outcomeGuidedSuccess)
}

// TestFaultPrematureFinishInterruptsThenStaysLive locks in the premature-
// finish fix (004 US2 T10/T11): an orchestrated task that exits while still at
// implementing/validating (never reaching validated completion) is stamped
// interrupted, never silently "finished". Interrupted is resumable
// (InvitesProceed), so this also proves the very next task stays live.
func TestFaultPrematureFinishInterruptsThenStaysLive(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "Hello."}}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "fault-premature-finish", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "done", Status: contract.PlanCompleted}}}
	engine.stampPlanCompletionPhase()
	if engine.LifecycleState() != contract.LifecycleInterrupted {
		t.Fatalf("premature finish state = %s, want interrupted", engine.LifecycleState())
	}

	answer, stats, runErr := engine.Run(context.Background(), "hi")
	assertRecoveryInvariant(t, engine, stats, runErr, answer, outcomeGuidedSuccess)
}

// --- T023 (FI-14): the mutation-guard self-test.
//
// The bounded-recovery limits (pipeline.go) are `const`s, not variables — they
// cannot be flipped at runtime by a test-only hook without editing production
// code, which this suite must not do just to exercise itself. Per the task's own
// fallback, these two tests instead prove the INVARIANT ITSELF bites: they
// feed assertRecoveryInvariant a deliberately-unbounded history/turn-count —
// exactly what an unbounded strike limit or a runaway turn-governor would
// produce — through a fake recoveryT recorder (a real *testing.T subtest's
// failure would propagate to and fail the outer, real test proving this), and
// assert the recorder observed a failure. If either of these ever reports
// "did not catch," the shared invariant has silently stopped enforcing FI-3
// and every other row in this file is only as strong as its own `want`.

// recordingRecoveryT is a fake recoveryT that records whether the invariant
// reported a failure, instead of failing the real test that invokes it.
// Fatalf only records (does not runtime.Goexit) — neither meta-test below
// exercises the hard-crash branch, which is the only one that calls Fatalf.
type recordingRecoveryT struct{ failed bool }

func (r *recordingRecoveryT) Helper()                           {}
func (r *recordingRecoveryT) Errorf(format string, args ...any) { r.failed = true }
func (r *recordingRecoveryT) Fatalf(format string, args ...any) { r.failed = true }

// TestFaultInvariantCatchesUnboundedRejectionLoop proves assertRecoveryInvariant
// fails a scenario where the same gate-rejection text repeats more than 3x —
// the machine-checkable form of "no gate rejects forever" (FR-007) that every
// bounded-recovery constant (and the non-blocking plan content bar) exists to
// prevent in production.
func TestFaultInvariantCatchesUnboundedRejectionLoop(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	denial := "plan not accepted yet — fix ALL of the following with ONE update_plan call, then call exit_plan_mode again: pipeline plan step 1 is missing an observable Acceptance:/Verify: check"
	for i := 0; i < 4; i++ { // 4 identical rejections is what an UNBOUNDED strike limit would produce
		engine.history.Append(contract.Message{Role: contract.RoleUser, Content: denial})
	}
	stats := contract.TaskStats{Turns: 10}
	recorder := &recordingRecoveryT{}
	assertRecoveryInvariant(recorder, engine, stats, nil, "", outcomeAny)
	if !recorder.failed {
		t.Fatal("assertRecoveryInvariant did not catch a >3x identical gate rejection — the liveness bar would silently accept an unbounded strike limit")
	}
}

// TestFaultInvariantCatchesTurnCeilingBreach is the sibling liveness check:
// riding the loop up to the ceiling — what an unbounded turn-governor or
// failure-terminator would do — must also fail the shared invariant.
func TestFaultInvariantCatchesTurnCeilingBreach(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	stats := contract.TaskStats{Turns: faultTurnCeiling}
	recorder := &recordingRecoveryT{}
	assertRecoveryInvariant(recorder, engine, stats, nil, "", outcomeAny)
	if !recorder.failed {
		t.Fatal("assertRecoveryInvariant did not catch a turn count at the ceiling — the liveness bar would silently accept a runaway loop")
	}
}

// FI-15 row template: every new incident becomes a PERMANENT catalog row by
// filling this shape and appending it to the `cases` slice in
// TestFaultInjectionCatalog (or, when the incident is triggered by a direct
// engine-API call rather than a model tool call, a dedicated Test function
// like the ones above that still ends by calling assertRecoveryInvariant).
// The shared helper makes the three-outcome + liveness assertions
// unavoidable by construction — no row can opt out of them.
//
//	{
//	    name:      "descriptive-fault-name",              // required, unique
//	    tools:     []contract.Tool{...},                  // optional
//	    setup:     func(e *Engine) { ... },                // optional: pre-Run engine state (lifecycle/plan/knowledge)
//	    prompt:    "the user prompt",                      // optional, defaults to "do the work"
//	    responses: []contract.ChatResponse{...},           // the scripted provider script
//	    want:      outcomeGuidedSuccess,                   // or outcomeRecordedDegradation / outcomeUserDecision / outcomeAny
//	},
