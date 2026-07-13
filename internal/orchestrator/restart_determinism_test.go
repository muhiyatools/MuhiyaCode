package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestRestartDeterminism(t *testing.T) {
	settings := engineSettings()
	firstProvider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "first"}}}
	registry := NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "mcp__demo__b"}, &recordingTool{name: "mcp__demo__a"})
	prompt := PromptContext{Workspace: t.TempDir(), OS: "windows", Shell: "pwsh", Model: "Test"}
	first, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: prompt.Workspace}, Provider: firstProvider, Registry: registry, Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	resumedProvider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "second"}}}
	resumed, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: prompt.Workspace}, Provider: resumedProvider, Registry: registry, History: NewHistory(first.history.Snapshot(), nil), Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resumed.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	one, two := firstProvider.requests[0], resumedProvider.requests[0]
	toolsOne, _ := json.Marshal(one.Tools)
	toolsTwo, _ := json.Marshal(two.Tools)
	if string(toolsOne) != string(toolsTwo) {
		t.Fatal("R2 changed across restart")
	}
	if len(two.Messages) <= len(one.Messages) {
		t.Fatalf("history did not append across restart: %d <= %d", len(two.Messages), len(one.Messages))
	}
	for index := range one.Messages {
		left, _ := json.Marshal(one.Messages[index])
		right, _ := json.Marshal(two.Messages[index])
		if string(left) != string(right) {
			t.Fatalf("R1/R3 message %d changed across restart", index)
		}
	}
	// T044: resume reloads persisted history bytes (not a re-render) and composes
	// the system prompt deterministically from the same PromptContext, so the
	// whole replayed request prefix (system + tools + settled messages) is
	// byte-identical across save/load. Because no divergence exists, the
	// differs⇒warn+one-time-event rule the plan allowed is not needed here.
	if SystemPrompt(first.prompt) != SystemPrompt(resumed.prompt) {
		t.Fatal("T044: system prompt diverged across restart")
	}
}

// TestProjectContextResumeMustReusePersistedBootBytes proves the cache-safety
// invariant behind the 005 US3 sidecar restore: the boot block materially
// affects the system prompt, so a resume that recomputed it from changed
// workspace memory would diverge the cached SystemHash mid-session. Resume
// therefore must reuse the persisted RenderedBootContext verbatim — asserted by
// showing the persisted bytes reproduce the original prompt while a recomputed
// block (after a memory change) would not.
func TestProjectContextResumeMustReusePersistedBootBytes(t *testing.T) {
	base := PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "m", ProjectMemory: true}
	persisted := RenderProjectContextBlock("H", "notes", "M1", "- one")          // captured at the original session start
	recomputed := RenderProjectContextBlock("H", "notes", "M2", "- one\n- two") // what a live recompute after a MEMORY.md edit would produce
	if persisted == recomputed {
		t.Fatal("test setup: blocks should differ after a memory change")
	}
	orig := base
	orig.ProjectContextBlock = persisted
	reused := base
	reused.ProjectContextBlock = persisted
	recompute := base
	recompute.ProjectContextBlock = recomputed
	if SystemPrompt(orig) != SystemPrompt(reused) {
		t.Fatal("reusing the persisted boot bytes must reproduce the original system prompt")
	}
	if SystemPrompt(orig) == SystemPrompt(recompute) {
		t.Fatal("a recomputed boot block after a memory change would diverge — resume must NOT recompute")
	}
}
