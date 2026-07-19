package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 011 D3/D4/D7 conformance (contracts/subagent-handoff.md §2–§4).

// A subagent whose provider-reported consumption crosses its TokenCeiling gets
// exactly one wrap-up instruction, its next response is accepted as the final
// report, and the result is labeled partial — never silent truncation, never a
// continued spend (contract §7.3 / SC-004 mechanism).
func TestSubagentTokenCeilingWrapsUpAndLabelsPartial(t *testing.T) {
	// Turn 1 burns 5000 tokens on a tool call (over the 4000 ceiling); the next
	// turn MUST be the wrap-up. The scripted turn-2 response returns the report.
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}, Usage: contract.Usage{PromptTokens: 4000, CompletionTokens: 1000, TotalTokens: 5000}},
		{Content: "Covered: a.go. Not covered: everything else.", Usage: contract.Usage{PromptTokens: 100, CompletionTokens: 50}},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "ceiling", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := engine.subagentSpecs()["review"]
	result := engine.executeSubagent(context.Background(), "r1", subagentInput{Agent: "review", Title: "t", Task: "review the diff", TokenCeiling: 4000}, spec)

	if !strings.HasPrefix(result.Status, "partial") {
		t.Fatalf("status = %q, want partial (token ceiling)", result.Status)
	}
	if !strings.Contains(result.Report, "Covered: a.go") {
		t.Fatalf("wrap-up report lost: %q", result.Report)
	}
	// The wrap-up instruction must have been injected after the breach.
	sawWrapUp := false
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "Token budget reached") {
				sawWrapUp = true
			}
		}
	}
	if !sawWrapUp {
		t.Fatal("token-ceiling wrap-up instruction never sent")
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2 (breach turn + wrap-up)", result.Turns)
	}
}

// TokenCeiling 0 means uncapped (the pre-baseline rollout default): no wrap-up
// fires from token accounting alone.
func TestSubagentZeroCeilingIsUncapped(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}, Usage: contract.Usage{TotalTokens: 500_000}},
		{Content: "Findings: none.", Usage: contract.Usage{TotalTokens: 100}},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "uncapped", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := engine.subagentSpecs()["explore"]
	result := engine.executeSubagent(context.Background(), "r2", subagentInput{Agent: "explore", Title: "t", Task: "survey"}, spec)
	if result.Status != "done" {
		t.Fatalf("status = %q, want done (uncapped)", result.Status)
	}
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "Token budget reached") {
				t.Fatal("uncapped run must never see the token wrap-up instruction")
			}
		}
	}
}

// D4: each subagent kind carries its own session pin so different kinds never
// churn one shared provider cache identity (contract §2).
func TestSubagentPerKindSessionPins(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "explore findings that are long enough to matter here."},
		{Content: "review findings that are long enough to matter here."},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pins", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	specs := engine.subagentSpecs()
	engine.executeSubagent(context.Background(), "p1", subagentInput{Agent: "explore", Title: "t", Task: "survey"}, specs["explore"])
	engine.executeSubagent(context.Background(), "p2", subagentInput{Agent: "review", Title: "t", Task: "review"}, specs["review"])
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d", len(provider.requests))
	}
	if got := provider.requests[0].SessionID; got != "pins:sub:explore" {
		t.Fatalf("explore pin = %q", got)
	}
	if got := provider.requests[1].SessionID; got != "pins:sub:review" {
		t.Fatalf("review pin = %q", got)
	}
}

// D7: a report above the digest bound returns truncated WITH an explicit
// banked-report note — content above the bound never enters main history, and
// never silently disappears (Constitution V).
func TestSubagentOversizedReportReturnsDigestNote(t *testing.T) {
	long := strings.Repeat("finding line with real content in it. ", 120) // ~4500 chars
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: long}}}
	settings := engineSettings()
	settings.Effort = contract.EffortHigh
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "digest", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.taskAgentCap = 1
	out, err := engine.runSubagentInput(context.Background(), subagentInput{Agent: "explore", Task: "survey the whole config surface"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "banked to knowledge") {
		t.Fatalf("oversized report missing the banked-digest note: %q", out[len(out)-200:])
	}
	if len(out) > 2800 {
		t.Fatalf("parent handoff exceeded the digest bound: %d chars", len(out))
	}
}
