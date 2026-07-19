package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type mixedScriptedProvider struct {
	mu       sync.Mutex
	mainRuns int
	subRuns  int
	requests []contract.ChatRequest
}

func (p *mixedScriptedProvider) Chat(_ context.Context, request contract.ChatRequest) (contract.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	if request.ModelID == "minimax-m3" {
		p.subRuns++
		if p.subRuns == 1 {
			return contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("read-1", "read_file", `{"path":"a.go"}`)}}, nil
		}
		return contract.ChatResponse{Content: "Changes made: none. Validation performed: inspected a.go. Problems: none. Remaining concerns: none."}, nil
	}
	p.mainRuns++
	if p.mainRuns == 1 {
		return contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("agent-1", "run_subagent", `{"agent":"general","title":"Inspect a.go","task":"Inspect a.go and return a bounded implementation report."}`)}}, nil
	}
	return contract.ChatResponse{Content: "Mixed-provider routing complete."}, nil
}

func (p *mixedScriptedProvider) ListModels(context.Context) ([]contract.Model, error) {
	return nil, nil
}

func TestMixedProviderRoutingAndPerStreamPrefixAffinity(t *testing.T) {
	provider := &mixedScriptedProvider{}
	settings := engineSettings()
	settings.Effort = contract.EffortLow
	settings.Provider.ActiveModelID = "deepseek-v4-pro"
	settings.Provider.SubagentModelID = "minimax-m3"
	settings.Provider.Models = []contract.Model{{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro"}, {ID: "minimax-m3", Name: "MiniMax M3"}}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "mixed", WorkspacePath: t.TempDir()}, Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{Model: "DeepSeek V4 Pro", SubagentModel: "MiniMax M3", HasSubagents: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The test targets stream routing rather than pipeline entry, so start at an
	// already-approved light implementation phase as a resumed session would.
	engine.lifecycle = Lifecycle{State: contract.LifecycleImplementing, Depth: PipelineDepthLight, ResearchCompleted: true, PlanWritten: true, Approved: true}
	if _, _, err := engine.Run(context.Background(), "Inspect a.go with one focused subagent, then report the result."); err != nil {
		t.Fatal(err)
	}

	provider.mu.Lock()
	requests := append([]contract.ChatRequest(nil), provider.requests...)
	provider.mu.Unlock()
	var main, sub []contract.ChatRequest
	for _, request := range requests {
		switch request.ModelID {
		case "deepseek-v4-pro":
			main = append(main, request)
			if !strings.HasSuffix(request.SessionID, ":main") {
				t.Fatalf("main session pin = %q", request.SessionID)
			}
		case "minimax-m3":
			sub = append(sub, request)
			// Feature 011 D4: subagent pins are per-kind (":sub:<kind>") so each
			// kind's distinct prefix keeps its own provider cache identity.
			if !strings.Contains(request.SessionID, ":sub:") {
				t.Fatalf("subagent session pin = %q, want per-kind :sub:<kind>", request.SessionID)
			}
		default:
			t.Fatalf("request routed to unexpected model %q", request.ModelID)
		}
	}
	if len(main) != 2 || len(sub) != 2 {
		t.Fatalf("request counts main=%d sub=%d; all=%+v", len(main), len(sub), requests)
	}
	assertStreamPrefixStable := func(label string, stream []contract.ChatRequest) {
		t.Helper()
		firstSystem, _ := json.Marshal(stream[0].Messages[0])
		firstTools, _ := json.Marshal(stream[0].Tools)
		for _, request := range stream[1:] {
			system, _ := json.Marshal(request.Messages[0])
			tools, _ := json.Marshal(request.Tools)
			if string(system) != string(firstSystem) || string(tools) != string(firstTools) {
				t.Fatalf("%s stable prefix changed\nsystem: %s / %s\ntools: %s / %s", label, firstSystem, system, firstTools, tools)
			}
		}
	}
	assertStreamPrefixStable("DeepSeek main", main)
	assertStreamPrefixStable("MiniMax subagent", sub)
}
