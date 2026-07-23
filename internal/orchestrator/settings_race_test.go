package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestConcurrentLiveSettingsChangesDoNotRace (F-1) fires /effort and /permission
// from a second goroutine while a task runs, exercising the live-settings path
// the TUI drives. Both fields are now read and written under liveSettingsMu, so
// this must be clean under `go test -race` (the acceptance gate; this host has
// no C compiler, so -race runs on CI). Without -race it still proves the
// concurrent path neither panics nor deadlocks.
func TestConcurrentLiveSettingsChangesDoNotRace(t *testing.T) {
	responses := make([]contract.ChatResponse, 0, 12)
	for i := 0; i < 10; i++ {
		responses = append(responses, contract.ChatResponse{
			ToolCalls: []contract.ToolCall{contract.NewToolCall(fmt.Sprintf("c%d", i), "read_file", fmt.Sprintf(`{"path":"f%d.txt"}`, i))},
		})
	}
	responses = append(responses, contract.ChatResponse{Content: "done"})
	provider := &scriptedProvider{responses: responses}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "f1-race", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			engine.SetEffort(contract.EffortHigh)
			engine.SetPermissionMode(contract.PermissionAutoAccept)
			engine.SetEffort(contract.EffortLow)
			engine.SetPermissionMode(contract.PermissionNormal)
		}
	}()
	if _, _, err := engine.Run(context.Background(), "read several files"); err != nil {
		t.Fatal(err)
	}
	<-done
}
