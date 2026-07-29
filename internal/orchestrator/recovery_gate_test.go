package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestRecoveryStateBlocksMutationBeforeDispatch(t *testing.T) {
	settings := engineSettings()
	tool := &recordingTool{name: "write_file"}
	engine, err := NewEngine(EngineConfig{
		Settings:            &settings,
		Session:             contract.Session{ID: "recovery", WorkspacePath: t.TempDir()},
		Provider:            &scriptedProvider{},
		Registry:            NewRegistry(tool),
		Prompt:              PromptContext{},
		RecoveryBlockReason: "an earlier mutation is indeterminate",
	})
	if err != nil {
		t.Fatal(err)
	}
	call := contract.NewToolCall("write", "write_file", `{"path":"x","content":"y"}`)
	outcome := engine.executeCall(context.Background(), call, engine.coreDefinitions(), Profile(contract.EffortLow))
	if !outcome.WasRejected() || !strings.Contains(outcome.Output, "recovery required before mutation") {
		t.Fatalf("outcome=%+v", outcome)
	}
	if tool.calls != 0 {
		t.Fatalf("mutation dispatched %d time(s)", tool.calls)
	}
}
