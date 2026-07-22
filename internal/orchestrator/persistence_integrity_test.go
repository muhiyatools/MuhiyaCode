package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestHistoryPersistenceFailureStopsBeforeProviderRequest(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "must not run"}}}
	history := NewHistory(HistorySnapshot{Version: 1}, func(HistorySnapshot) error {
		return errors.New("disk unavailable")
	})
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "persist-failure", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(),
		History:  history,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, runErr := engine.Run(context.Background(), "do work")
	if runErr == nil || !strings.Contains(runErr.Error(), "persist session history") {
		t.Fatalf("Run error = %v", runErr)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider received %d request(s) after persistence failed", len(provider.requests))
	}
}

func TestPrefixShapePersistenceFailureRetries(t *testing.T) {
	settings := engineSettings()
	attempts := 0
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Provider: &scriptedProvider{},
		Registry: NewRegistry(),
		Persistence: Persistence{WritePrefixShape: func(context.Context, contract.PrefixShapeSnapshot) error {
			attempts++
			if attempts == 1 {
				return errors.New("temporary failure")
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	shape := PrefixShape{SystemHash: "s", ToolsHash: "t", ModelID: "main"}
	if err := engine.persistPrefixShapeOnce(context.Background(), shape); err == nil {
		t.Fatal("first persistence attempt unexpectedly succeeded")
	}
	if err := engine.persistPrefixShapeOnce(context.Background(), shape); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if attempts != 2 || !engine.prefixShapeSaved {
		t.Fatalf("attempts=%d saved=%v", attempts, engine.prefixShapeSaved)
	}
}
