package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// 004 US3: mode awareness + enforcement. Blocked attempts must teach the model
// what IS allowed, and delegated subagents must be told their exact boundaries.

// T034: an unknown tool name is answered with the closest real one so the model
// self-corrects in a single step.
func TestUnknownToolSuggestsNearest(t *testing.T) {
	reg := NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "write_file"}, &recordingTool{name: "grep"})
	_, err := reg.Execute(context.Background(), "read_fil", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Closest available: read_file") {
		t.Fatalf("expected a nearest-name suggestion, got %v", err)
	}
	// A name with no close match gets the plain unknown-tool error.
	_, err = reg.Execute(context.Background(), "zzzzzzzzz", nil, nil)
	if err == nil || strings.Contains(err.Error(), "Closest available") {
		t.Fatalf("a far-off name should not get a suggestion, got %v", err)
	}
}

// T038: a delegated subagent's mission includes an explicit capability statement
// naming its exact toolset and boundary.
func TestSubagentCapabilityStatement(t *testing.T) {
	spec := subagentSpec{Allowed: map[string]bool{
		"list_files": true, "read_file": true, "grep": true, "search_text": true,
		"glob": true, "git_status": true, "git_diff": true,
	}}
	statement := capabilityStatement(spec)
	// Deterministic, sorted toolset.
	if !strings.Contains(statement, "Tools available to you: git_diff, git_status, glob, grep, list_files, read_file, search_text") {
		t.Fatalf("capability statement toolset wrong or unsorted: %q", statement)
	}
	if !strings.Contains(statement, "Anything not listed is unavailable to you") {
		t.Fatalf("capability statement missing the boundary line: %q", statement)
	}
	if !strings.Contains(statement, "report it back to the caller") {
		t.Fatalf("capability statement missing the report contract: %q", statement)
	}
}

// T036: a propose_changes verdict produced without an interactive client is
// labelled honestly — the model must not read it as human approval.
func TestProposeChangesNonInteractiveIsHonest(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "pc", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{}})
	if err != nil {
		t.Fatal(err)
	}
	verdict, err := engine.proposeChanges(context.Background(), []byte(`{"summary":"x","files":[],"testPlan":"y","estimatedSteps":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verdict, "no human reviewed") || strings.Contains(verdict, `"verdict":"approved"`) {
		t.Fatalf("non-interactive verdict must not read as human approval: %q", verdict)
	}
}
