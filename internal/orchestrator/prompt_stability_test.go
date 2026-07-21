package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPromptStabilityDynamicDateLivesOnlyInTaskBrief(t *testing.T) {
	prompt := SystemPrompt(PromptContext{Workspace: "workspace", OS: "windows", Shell: "pwsh", Model: "model"})
	if strings.Contains(prompt, "date:") || strings.Contains(prompt, time.Now().Format("2006-01-02")) {
		t.Fatal("system prompt contains a wall-clock date")
	}
	brief := BudgetFor(Classify("hi", ""), Profile(contract.EffortLow)).Brief
	if !strings.Contains(brief, "date="+time.Now().Format("2006-01-02")) {
		t.Fatalf("task brief does not carry current date: %s", brief)
	}
}

// TestSystemPromptByteIdenticalAcrossConstructions (T037 / REV A4.1) guards the
// cache-discipline section: the system prompt must be byte-identical across two
// constructions with identical config and carry no dynamic values, so the added
// section costs one upgrade-time cache break and then rides the cache forever.
func TestSystemPromptByteIdenticalAcrossConstructions(t *testing.T) {
	ctx := PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "m", HasWeb: true}
	a := SystemPrompt(ctx)
	b := SystemPrompt(ctx)
	if a != b {
		t.Fatal("system prompt is not byte-identical across two constructions with identical config")
	}
	if !strings.Contains(a, "CACHE DISCIPLINE") {
		t.Fatal("cache-discipline section (T037/A4.1) is missing from the system prompt")
	}
	if strings.Contains(a, time.Now().Format("2006-01-02")) {
		t.Fatal("system prompt leaked a wall-clock date (dynamic value in the cached prefix)")
	}
}

func TestPromptStabilityMCPToolBlockAcrossRegistrationOrders(t *testing.T) {
	build := func(reverse bool) []byte {
		registry := NewRegistry(&recordingTool{name: "workspace_tool"})
		mcp := []*recordingTool{{name: "mcp__zeta__b"}, {name: "mcp__alpha__a"}, {name: "mcp__zeta__a"}}
		if reverse {
			for index := len(mcp) - 1; index >= 0; index-- {
				registry.Add(mcp[index])
			}
		} else {
			for _, tool := range mcp {
				registry.Add(tool)
			}
		}
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: registry})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(engine.sessionDefinitions())
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	first, second := build(false), build(true)
	if string(first) != string(second) {
		t.Fatalf("tool bytes changed with MCP registration order\nfirst: %s\nsecond:%s", first, second)
	}
}
