package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 011 D4/D7 conformance (contracts/subagent-handoff.md §2–§4), plus the
// v1.1.0 bounds: the token BUDGET is gone (a quota), replaced by the context
// WINDOW (physics) and a progress ladder.

// windowEngine builds an engine whose subagent model has a deliberately tiny
// context window, so a scripted prompt-token count can cross the pressure
// threshold without fabricating a 100k-token fixture.
func windowEngine(t *testing.T, id string, window int, responses ...contract.ChatResponse) (*Engine, *scriptedProvider) {
	t.Helper()
	provider := &scriptedProvider{responses: responses}
	settings := engineSettings()
	settings.Provider.Models = []contract.Model{{ID: "main", Name: "Test", ContextLimit: window}}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: id, WorkspacePath: t.TempDir()},
		// grep is registered alongside read_file because it is a SUCCESSFUL tool
		// call that touches no path — the shape a run takes when it is busy but no
		// longer gathering durable evidence, which is what the ladder must not
		// reward with another rung.
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "grep"}),
		Prompt: PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, provider
}

// A run whose provider-reported prompt tokens cross the window-pressure
// threshold gets exactly one wrap-up instruction, its next response is accepted
// as the final report, and the result is labeled partial so the parent
// dispatches a continuation instead of mistaking bounded coverage for complete
// coverage. This is a PHYSICAL bound — the alternative is dying on a
// context-length 400 with no report at all.
func TestSubagentWindowPressureWrapsUpAndLabelsPartial(t *testing.T) {
	// Window 10000 ⇒ threshold 8500. Turn 1 reports 9000 prompt tokens.
	engine, provider := windowEngine(t, "window", 10_000,
		contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}, Usage: contract.Usage{PromptTokens: 9000, CompletionTokens: 100}},
		contract.ChatResponse{Content: "Covered: a.go. Not covered: everything else.", Usage: contract.Usage{PromptTokens: 100, CompletionTokens: 50}},
	)
	spec := engine.subagentSpecs()["review"]
	result := engine.executeSubagent(context.Background(), "r1", subagentInput{Agent: "review", Title: "t", Task: "review the diff"}, spec)

	if result.Status != "partial (context window)" {
		t.Fatalf("status = %q, want partial (context window)", result.Status)
	}
	if !strings.Contains(result.Report, "Covered: a.go") {
		t.Fatalf("wrap-up report lost: %q", result.Report)
	}
	sawWrapUp := false
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "Context window nearly full") {
				sawWrapUp = true
			}
		}
	}
	if !sawWrapUp {
		t.Fatal("window-pressure wrap-up instruction never sent")
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2 (pressure turn + wrap-up)", result.Turns)
	}
}

// Below the threshold nothing fires, however many tokens the run has spent in
// TOTAL: consumption is not a budget any more, only the live prompt size against
// the window matters.
func TestSubagentBelowWindowPressureIsUnbounded(t *testing.T) {
	engine, provider := windowEngine(t, "nopressure", 1_000_000,
		contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}, Usage: contract.Usage{PromptTokens: 20_000, CompletionTokens: 500_000}},
		contract.ChatResponse{Content: "Findings: none.", Usage: contract.Usage{PromptTokens: 21_000}},
	)
	spec := engine.subagentSpecs()["explore"]
	result := engine.executeSubagent(context.Background(), "r2", subagentInput{Agent: "explore", Title: "t", Task: "survey"}, spec)
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "Context window nearly full") {
				t.Fatal("a run well inside its window must never see the pressure wrap-up")
			}
		}
	}
}

// THE LADDER (G0.1). A run still touching new files when it reaches its first
// rung earns another rung instead of being wrapped up. Before this, a healthy
// executor was stopped mid-build purely for having taken N turns.
func TestSubagentLadderExtendsWhileWorkLands(t *testing.T) {
	spec := subagentSpec{Name: "general", Allowed: map[string]bool{"read_file": true}, MaxTurns: 2, System: "test"}
	// Every turn reads a DISTINCT file, so touchedCount keeps growing and the
	// ladder keeps extending past the 2-turn first rung.
	var responses []contract.ChatResponse
	for i := 0; i < 8; i++ {
		responses = append(responses, contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall(fmt.Sprintf("c%d", i), "read_file", fmt.Sprintf(`{"path":"file%d.go"}`, i)),
		}})
	}
	responses = append(responses, contract.ChatResponse{Content: "Done reading. STATUS: COMPLETE"})
	engine, provider := windowEngine(t, "ladder", 1_000_000, responses...)
	result := engine.executeSubagent(context.Background(), "r3", subagentInput{Agent: "general", Title: "t", Task: "read everything"}, spec)

	if result.Turns <= 2+2 { // first rung + wrapUpTurns
		t.Fatalf("turns = %d: the ladder did not extend for a run that kept landing work", result.Turns)
	}
	sawExtension := false
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "[governor] Run extended") {
				sawExtension = true
			}
		}
	}
	if !sawExtension {
		t.Fatal("no extension rider was sent")
	}
}

// The ladder's other half, and the liveness proof: a run that has STOPPED
// touching anything new does not earn rungs. Without this the ladder would be
// an unbounded loop rather than a progress-gated one.
func TestSubagentLadderDoesNotExtendWithoutProgress(t *testing.T) {
	spec := subagentSpec{Name: "general", Allowed: map[string]bool{"grep": true}, MaxTurns: 2, System: "test"}
	// Every turn is a successful grep with a distinct pattern: real calls, no
	// repeat-limiter trigger, but no file ever opened — touchedCount stays 0, so
	// the run never earns a rung and must wrap up at its first one.
	var responses []contract.ChatResponse
	for i := 0; i < 12; i++ {
		responses = append(responses, contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall(fmt.Sprintf("c%d", i), "grep", fmt.Sprintf(`{"pattern":"p%d"}`, i)),
		}})
	}
	responses = append(responses, contract.ChatResponse{Content: "Nothing new. STATUS: COMPLETE"})
	engine, provider := windowEngine(t, "noladder", 1_000_000, responses...)
	result := engine.executeSubagent(context.Background(), "r4", subagentInput{Agent: "general", Title: "t", Task: "spin"}, spec)

	if result.Turns > 2+2 {
		t.Fatalf("turns = %d: a run making no progress must wrap up at its first rung", result.Turns)
	}
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "[governor] Run extended") {
				t.Fatal("a stalled run must never earn another rung")
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
