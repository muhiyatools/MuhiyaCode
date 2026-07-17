package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPipelineFastPathStaysDirect(t *testing.T) {
	for _, prompt := range []string{"hello", "Fix the typo in README.md", "What is dependency injection?"} {
		t.Run(prompt, func(t *testing.T) {
			provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "direct answer"}}}
			settings := engineSettings()
			agentStarts, planWrites := 0, 0
			engine, err := NewEngine(EngineConfig{
				Settings: &settings, Session: contract.Session{ID: "fast", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(),
				Callbacks: contract.Callbacks{Agent: func(event contract.AgentEvent) {
					if event.Kind == "start" {
						agentStarts++
					}
				}},
				Persistence: Persistence{WritePlan: func(context.Context, string) error { planWrites++; return nil }},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := engine.Run(context.Background(), prompt); err != nil {
				t.Fatal(err)
			}
			if engine.LifecycleState() != contract.LifecycleDirect || agentStarts != 0 || planWrites != 0 || len(provider.requests) != 1 {
				t.Fatalf("fast path changed: state=%s agents=%d writes=%d requests=%d", engine.LifecycleState(), agentStarts, planWrites, len(provider.requests))
			}
			for _, message := range provider.requests[0].Messages {
				if message.Role == contract.RoleSystem {
					continue
				}
				if strings.Contains(message.Content, "[orchestration-pipeline]") || strings.Contains(message.Content, "[plan-mode]") {
					t.Fatalf("direct request carried pipeline state: %q", message.Content)
				}
			}
		})
	}
}

func TestPipelineStaticPrefixStableAcrossPhases(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "main", HasSubagents: true, SubagentModel: "sub"}})
	if err != nil {
		t.Fatal(err)
	}
	basePrompt := SystemPrompt(engine.prompt)
	baseTools, _ := json.Marshal(engine.sessionDefinitions())
	// UL-12: the cache-stable prefix (system prompt + tools) is byte-identical
	// across ALL 11 lifecycle states — the state rides dynamic tail blocks only.
	for _, state := range []contract.LifecycleState{
		contract.LifecycleDirect, contract.LifecycleResearch, contract.LifecyclePlanning,
		contract.LifecycleApproval, contract.LifecyclePending, contract.LifecycleImplementing,
		contract.LifecycleValidating, contract.LifecycleInterrupted, contract.LifecycleFinished,
		contract.LifecycleSuperseded, contract.LifecycleDiscarded,
	} {
		engine.lifecycle = Lifecycle{State: state, Depth: PipelineDepthFull}
		if got := SystemPrompt(engine.prompt); got != basePrompt {
			t.Fatalf("system prompt changed in state %s", state)
		}
		tools, _ := json.Marshal(engine.sessionDefinitions())
		if string(tools) != string(baseTools) {
			t.Fatalf("tool prefix changed in state %s", state)
		}
	}
	if strings.Contains(basePrompt, "phase=research") || strings.Contains(basePrompt, "pipeline_phase") {
		t.Fatal("dynamic pipeline state leaked into the stable prefix")
	}
}
