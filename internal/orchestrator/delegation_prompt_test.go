package orchestrator

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 008 T011 (contract DG-1..6): the delegation guidance and the enriched
// run_subagent description are byte-stable prefix content — deterministic across
// constructions — and carry the criteria the research phase found missing.

func delegationPromptContext() PromptContext {
	return PromptContext{
		Workspace: "/w", OS: "linux", Shell: "bash", Model: "deepseek-v4-flash",
		HasWeb: true, HasSubagents: true, SubagentModel: "deepseek-v4-flash",
		ProjectMemory: true,
	}
}

func TestSystemPromptByteStableAcrossConstructions(t *testing.T) {
	a := SystemPrompt(delegationPromptContext())
	b := SystemPrompt(delegationPromptContext())
	if a != b {
		t.Fatal("SystemPrompt must be byte-identical for identical inputs (DG-5)")
	}
}

func TestDelegationSectionContent(t *testing.T) {
	prompt := SystemPrompt(delegationPromptContext())
	// DG-1..3: dedicated section, positive criteria, negative criteria, brief
	// semantics, cache reconciliation.
	for _, want := range []string{
		"DELEGATION",
		"agents<=N",                              // brief semantics taught statically
		"independent",                            // positive criteria
		"one linear thread",                      // negative criteria
		"NEW broad reading",                      // CACHE DISCIPLINE reconciliation
		"cannot see this conversation",           // deliverable focus
		"never re-explore a scope you delegated", // report consumption (DG-13)
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("DELEGATION guidance missing %q", want)
		}
	}
	// The old hedged one-liner must be gone (research F1).
	if strings.Contains(prompt, "delegate only independent exploration, review, or isolated work") {
		t.Fatal("the old hedged delegation clause must be replaced")
	}
	// FR-018 edit discipline lives in the same epoch.
	if !strings.Contains(prompt, "never rewrite a whole existing file") {
		t.Fatal("edit-discipline guidance missing (FR-018)")
	}
	// MT-3: memory saves route through the tool now.
	if !strings.Contains(prompt, "save_memory") || strings.Contains(prompt, "record it by editing MEMORY.md with your normal file tools") {
		t.Fatal("memory instruction must route through save_memory (MT-3)")
	}
}

func TestRunSubagentDefinitionRichAndDeterministic(t *testing.T) {
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "008-def", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Prompt: PromptContext{},
	})
	specs := engine.subagentSpecs()
	first, err := json.Marshal(runSubagentDefinition(specs))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(runSubagentDefinition(specs))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("run_subagent definition must marshal byte-identically (DG-5)")
	}
	// Check the description text pre-marshal (json.Marshal HTML-escapes "<").
	description := runSubagentDefinition(specs).Function.Description
	// The 'plan' kind is gone; only the surviving three are asserted. (The
	// description text itself is rewritten in a later phase.)
	for _, want := range []string{"Example:", "explore", "review", "general", "agents<=N"} {
		if !strings.Contains(description, want) {
			t.Fatalf("run_subagent description missing %q (DG-4)", want)
		}
	}
	if !strings.Contains(string(first), "does not see this conversation") {
		t.Fatal("task-field guidance missing from the marshaled schema (DG-4)")
	}
}
