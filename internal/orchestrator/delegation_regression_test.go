package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 008 T012+T013: bounded recovery for the edit-fumble loop, and
// regression guards over the delegation surfaces that must NOT change
// (denial texts, brief format, class caps) plus the AutoReview nudge.

// mismatchEditTool mimics the workspace's reclassified near-miss edit outcome
// (registry.go editMismatchError): the note text rides inside a real error, so
// the orchestrator marks the outcome Failed. The end-to-end classification
// itself is covered by internal/workspace/edit_mismatch_test.go.
type mismatchEditTool struct{ calls int }

func (t *mismatchEditTool) Definition() contract.ToolDefinition {
	return definition("edit_file", "test edit tool", map[string]any{"path": map[string]any{"type": "string"}, "oldString": map[string]any{"type": "string"}, "newString": map[string]any{"type": "string"}}, nil)
}

func (t *mismatchEditTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	t.calls++
	return "", fmt.Errorf("edit failed: No changes needed.\noldString not found; closest region: func real() {}")
}

// nearMissCalls builds n responses, each carrying two DISTINCT near-miss edits
// (different oldString per call) so neither the verbatim-repeat limiter nor the
// failed-call cache can dedupe them — the pre-008 loop shape.
func nearMissCalls(n int) []contract.ChatResponse {
	responses := make([]contract.ChatResponse, n)
	for i := range responses {
		responses[i] = contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall(fmt.Sprintf("a%d", i), "edit_file", fmt.Sprintf(`{"path":"main.go","oldString":"miss %d a","newString":"x"}`, i)),
			contract.NewToolCall(fmt.Sprintf("b%d", i), "edit_file", fmt.Sprintf(`{"path":"main.go","oldString":"miss %d b","newString":"x"}`, i)),
		}}
	}
	return responses
}

// TestNearMissEditLoopIsBounded (DG-9/DG-11, quickstart §3): repeated near-miss
// edits now count as failures and the distinct-failure terminator stops the task
// within its existing bound — with the recovery note intact and NO token-ceiling
// message anywhere (FR-005).
func TestNearMissEditLoopIsBounded(t *testing.T) {
	provider := &scriptedProvider{responses: nearMissCalls(20)}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-editloop", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&mismatchEditTool{}),
		Prompt: PromptContext{},
	})
	_, stats, err := engine.Run(context.Background(), "fix the bug in main.go")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Turns >= hardTurnCeiling {
		t.Fatalf("near-miss edit loop was not bounded: %d turns", stats.Turns)
	}
	if stats.TerminatedReason == "" || !strings.Contains(stats.TerminatedReason, "repeated") {
		t.Fatalf("expected the repeated-failure terminator, got %q", stats.TerminatedReason)
	}
	if strings.Contains(stats.TerminatedReason, "token budget") {
		t.Fatal("no token-ceiling message may exist (FR-005)")
	}
}

// TestChatTurnDelegationSucceeds (T013, revised v1.1.0): what used to be the
// zero-budget denial path. A conversational prompt that nonetheless asks for
// work must be able to dispatch — the denial texts it once pinned are gone,
// because a refusal there left the agent with no legal action at all.
func TestChatTurnDelegationSucceeds(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "run_subagent", `{"agent":"explore","task":"look around"}`)}},
		{Content: "a report from the explore agent describing what it found in the workspace layout"},
		{Content: "done"},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-denial", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(),
		Prompt: PromptContext{},
	})
	_, stats, err := engine.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if stats.AgentRuns != 1 {
		t.Fatalf("a chat-class turn could not delegate: AgentRuns=%d", stats.AgentRuns)
	}
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "no subagent budget") {
				t.Fatal("a budget denial reached the model")
			}
		}
	}
}

// TestBriefCarriesNoAgentAllowance (T013, revised v1.1.0): the tail brief no
// longer advertises an agent count. A cap plus the plan/execute split could
// starve a turn into having no legal action at all, so delegation scale is now
// the model's judgment and the brief says nothing about it.
func TestBriefCarriesNoAgentAllowance(t *testing.T) {
	for _, prompt := range []string{"hi", "please delegate parts of this refactor across the api and web packages"} {
		brief := BudgetFor(Classify(prompt, ""), Profile(contract.EffortMax)).Brief
		if strings.Contains(brief, "agents") {
			t.Fatalf("brief still carries an agent allowance: %q", brief)
		}
		if !strings.Contains(brief, "turns<=") || !strings.Contains(brief, "verify=") {
			t.Fatalf("brief lost its surviving fields: %q", brief)
		}
	}
}

// TestAutoReviewNudgeFiresOnceAtMax (DG-7): at max effort, a task that changed
// two files gets exactly ONE review rider before finalizing; the rider fits the
// ~50-token tail budget.
func TestAutoReviewNudgeFiresOnceAtMax(t *testing.T) {
	// Feature 011: the nudge now consults the review gate first, so the changed
	// files must be genuinely review-worthy — two auth-path files trip hard rule
	// H4 (risk outranks size). Two trivial .txt writes would correctly SKIP under
	// the gate (that suppression is covered by reviewgate_test.go).
	// Under the plan/execute split the MAIN model cannot write files, so the
	// changed files arrive through the execution agent — which is exactly the
	// path the nudge must still see (the shared gate tallies both scopes).
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("d1", "run_subagent", `{"agent":"general","title":"auth files","task":"Update the two auth config files for the deploy."}`),
		}},
		// The execution agent's own stream: apply the edits, then report.
		{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("w1", "write_file", `{"path":"internal/auth/handler.go","content":"x"}`),
			contract.NewToolCall("w2", "write_file", `{"path":"internal/auth/session.go","content":"y"}`),
		}},
		{Content: "Updated internal/auth/handler.go and internal/auth/session.go for the deploy configuration change."},
		{Content: "all done"},
		{Content: "final answer after review nudge"},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortMax
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-autoreview", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "write_file"}),
		Prompt: PromptContext{},
	})
	// "delegate" keeps the class ≥ standard so the agent cap is non-zero.
	answer, _, err := engine.Run(context.Background(), "please delegate and update the two config files for the deploy")
	if err != nil {
		t.Fatal(err)
	}
	nudges := 0
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if message.Role == contract.RoleUser && strings.Contains(message.Content, "[review]") {
				nudges++
			}
		}
	}
	// The rider appears in every request AFTER injection (history is resent), so
	// count distinct occurrences in the LAST request only.
	last := provider.requests[len(provider.requests)-1]
	inLast := 0
	for _, message := range last.Messages {
		if message.Role == contract.RoleUser && strings.Contains(message.Content, "[review]") {
			inLast++
		}
	}
	if inLast != 1 {
		t.Fatalf("AutoReview rider must fire exactly once, found %d in the final request (total sightings %d)", inLast, nudges)
	}
	if !strings.Contains(answer, "final answer") {
		t.Fatalf("task must still finalize after the nudge, got %q", answer)
	}
	rider := "[review] Before finishing: run one review subagent over the files you changed and fix only verified findings, then give the final answer."
	if EstimateTokens(rider) > 50 {
		t.Fatalf("AutoReview rider exceeds the ~50-token tail budget: %d", EstimateTokens(rider))
	}
}

// TestTrailingIntentNarrationDoesNotFinalize (FR-004b): a mid-task turn that
// ANNOUNCES the next action with no tool call ("Let me fix:") must be nudged to
// act, not accepted as the final answer — the exact real-session stop where a
// capcut-clone task ended mid-thought after "…handleAddToTimeline. Let me fix:".
func TestTrailingIntentNarrationDoesNotFinalize(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "write_file", `{"path":"a.txt","content":"x"}`)}},
		{Content: "The Toolbar still uses addTrack in the JSX buttons. Let me fix:"},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c2", "write_file", `{"path":"b.txt","content":"y"}`)}},
		{Content: "Fixed the Toolbar; both files updated."},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-intent", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "write_file"}),
		Prompt: PromptContext{},
	})
	answer, stats, err := engine.Run(context.Background(), "fix the toolbar wiring in the two files")
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("narration must be nudged past, expected 4 requests, got %d", len(provider.requests))
	}
	if !strings.Contains(answer, "Fixed the Toolbar") {
		t.Fatalf("task finalized on the narration instead of the real answer: %q", answer)
	}
	if stats.Turns >= hardTurnCeiling {
		t.Fatalf("intent guard must stay bounded, ran %d turns", stats.Turns)
	}
	nudged := false
	for _, message := range provider.requests[2].Messages {
		if message.Role == contract.RoleUser && strings.Contains(message.Content, "made no tool call") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("the intent nudge was not delivered on the following request")
	}
}

// TestTrailingIntentGuardsDoNotLoopOrMisfire (FR-004b bounds): a genuine final
// summary (even mid-work) finalizes normally, pure chat is untouched, and a
// stubborn narrator is bounded to two nudges.
func TestTrailingIntentGuardsDoNotMisfire(t *testing.T) {
	if trailingIntent("All four modules were fixed and verified.") {
		t.Fatal("a completed sentence must not read as intent")
	}
	if trailingIntent("Changed files:\n- a.txt\n- b.txt") {
		t.Fatal("a list-style summary must not read as intent")
	}
	if !trailingIntent("The import is stale. Let me fix:") {
		t.Fatal("the canonical narration shape must read as intent")
	}
	if !trailingIntent("Now I'll update the CSS and HTML:") {
		t.Fatal("first-person imperative narration must read as intent")
	}
	// A stubborn narrator: two nudges, then the narration is accepted so the
	// task can never loop on the guard.
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "write_file", `{"path":"a.txt","content":"x"}`)}},
		{Content: "Let me fix:"},
		{Content: "Let me fix:"},
		{Content: "Let me fix:"},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-intent-bound", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "write_file"}),
		Prompt: PromptContext{},
	})
	if _, _, err := engine.Run(context.Background(), "fix the toolbar wiring"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("intent guard must cap at two nudges (4 requests total), got %d", len(provider.requests))
	}
}

// TestAutoReviewNudgeSkippedBelowMax (DG-7): high effort has no AutoReview.
func TestAutoReviewNudgeSkippedBelowMax(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("w1", "write_file", `{"path":"a.txt","content":"x"}`),
			contract.NewToolCall("w2", "write_file", `{"path":"b.txt","content":"y"}`),
		}},
		{Content: "done"},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortHigh
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-noreview", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "write_file"}),
		Prompt: PromptContext{},
	})
	if _, _, err := engine.Run(context.Background(), "please delegate and update the two config files for the deploy"); err != nil {
		t.Fatal(err)
	}
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "[review] Before finishing") {
				t.Fatal("AutoReview rider must not fire below max effort")
			}
		}
	}
}
