package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestTaskGraphValidationRejectsUnknownDependency(t *testing.T) {
	graph := contract.TaskGraph{
		Version: contract.TaskGraphVersion,
		Nodes: []contract.TaskNode{{
			ID: "one", Title: "One", Status: contract.TaskNodePending,
			Dependencies: []string{"missing"},
		}},
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("unknown dependencies must be rejected")
	}
}

func TestTaskGraphValidationRejectsDependencyCycle(t *testing.T) {
	graph := contract.TaskGraph{
		Version: contract.TaskGraphVersion,
		Nodes: []contract.TaskNode{
			{ID: "one", Title: "One", Status: contract.TaskNodePending, Dependencies: []string{"three"}},
			{ID: "two", Title: "Two", Status: contract.TaskNodePending, Dependencies: []string{"one"}},
			{ID: "three", Title: "Three", Status: contract.TaskNodePending, Dependencies: []string{"two"}},
		},
	}
	if err := graph.Validate(); err == nil {
		t.Fatal("dependency cycles must be rejected")
	}
}

func TestTaskGraphPersistsApprovalAndScopesMutations(t *testing.T) {
	var saved contract.ExecutionEvent
	settings := engineSettings()
	workspace := t.TempDir()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "graph", WorkspacePath: workspace},
		Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{},
		InitialTaskGraph: contract.TaskGraph{
			Version: contract.TaskGraphVersion,
			Nodes:   []contract.TaskNode{{ID: "one", Title: "One", Status: contract.TaskNodePending}},
		},
		Persistence: Persistence{AppendExecutionEvent: func(_ context.Context, event contract.ExecutionEvent) error {
			saved = event
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.setPlanApproval(context.Background(), true, "limited", []string{"allowed"}); err != nil {
		t.Fatal(err)
	}
	if saved.Kind != contract.ExecutionProjectionSaved || saved.Checkpoint == nil || saved.Checkpoint.Name != taskGraphProjection {
		t.Fatalf("approval was not durably projected: %+v", saved)
	}
	allowed := contract.NewToolCall("a", "write_file", `{"path":"allowed/file.go","content":"x"}`)
	if ok, reason := engine.planScopeAllows(allowed); !ok {
		t.Fatalf("approved descendant rejected: %s", reason)
	}
	blocked := contract.NewToolCall("b", "write_file", `{"path":"outside.go","content":"x"}`)
	if ok, _ := engine.planScopeAllows(blocked); ok {
		t.Fatal("mutation outside the approved path was allowed")
	}
	if got := engine.taskGraph.Approval.Scope[0]; got != filepath.Join(workspace, "allowed") {
		t.Fatalf("approval scope was not normalized: %s", got)
	}
}

func TestPlanApprovalPersistenceFailureIsNotHidden(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "graph-fail", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{},
		Persistence: Persistence{AppendExecutionEvent: func(context.Context, contract.ExecutionEvent) error {
			return errors.New("disk full")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.setPlanApproval(context.Background(), true, "scope", []string{"x"}); err == nil {
		t.Fatal("approval must not report success when its durable projection failed")
	}
}

func TestTaskEvidencePersistenceFailureBlocksSuccessfulCompletion(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "graph-evidence-fail", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{},
		InitialTaskGraph: contract.TaskGraph{
			Version: contract.TaskGraphVersion,
			Nodes:   []contract.TaskNode{{ID: "one", Title: "One", Status: contract.TaskNodeInProgress}},
		},
		Persistence: Persistence{AppendExecutionEvent: func(context.Context, contract.ExecutionEvent) error {
			return errors.New("disk full")
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.recordTaskEvidence(contract.NewToolCall("write", "write_file", `{"path":"x","content":"y"}`))
	engine.taskMu.Lock()
	persistenceError := engine.taskEvidenceError
	engine.taskMu.Unlock()
	if persistenceError == "" {
		t.Fatal("task evidence persistence failure was not retained as a completion blocker")
	}
}
