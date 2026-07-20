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
	// DG-1..3: dedicated section, brief semantics, per-kind criteria, cache
	// reconciliation. v1.1.0: the negative criterion is no longer "do it
	// yourself when it is one linear thread" — under the plan/execute split
	// every workspace change is delegated, so the guidance now bounds WHICH
	// extra agents earn a run and insists on chaining to keep the cache warm.
	for _, want := range []string{
		"DELEGATION",
		"no run limit",                           // budgets are gone; judgment is the limit
		"executes every workspace change",        // the split, stated positively
		"Chain each follow-up",                   // continuation is the cheap path
		"One agent at a time",                    // the serial directive
		"NOT already in context",                 // cache reconciliation
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
	// The 'plan' kind is gone; only the surviving three are asserted. The
	// allowance clause is gone too — delegation scale is the model's judgment.
	for _, want := range []string{"Example:", "explore", "review", "general", "no run limit"} {
		if !strings.Contains(description, want) {
			t.Fatalf("run_subagent description missing %q (DG-4)", want)
		}
	}
	if !strings.Contains(string(first), "does not see this conversation") {
		t.Fatal("task-field guidance missing from the marshaled schema (DG-4)")
	}
}

// TestPlanningSkillContent pins the internal planning skill (Phase 4): the
// discipline the model applies when work is genuinely large or the user asks
// for a plan. Its memory clauses are what makes it "deeply connected to
// memory" — it recalls before designing and records after settling; a
// consult-only integration would forget every decision at session end.
func TestPlanningSkillContent(t *testing.T) {
	prompt := SystemPrompt(delegationPromptContext())
	for _, want := range []string{
		"PLANNING",
		"or the user asks for a plan",      // explicit-request trigger
		"not planned",                      // small tasks are executed, not ceremonied
		"Recall project memory first",      // memory READ
		"Save the durable decisions",       // memory WRITE — the deep half
		"never plan against assumption",    // grounded in real code
		"INTO tasks.md",                    // the plan is always written, never just spoken
		"WITHOUT further design decisions", // the executor-ready standard
		"the file is the deliverable",      // plan-only requests stop at the WRITTEN plan
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("PLANNING section missing %q", want)
		}
	}
	// Planning must not resurrect a mode: no approval pause, no lifecycle.
	for _, forbidden := range []string{"exit_plan_mode", "plan mode", "awaiting approval", "proceed now"} {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(forbidden)) {
			t.Fatalf("the prompt reintroduces planning ceremony: %q", forbidden)
		}
	}
}
