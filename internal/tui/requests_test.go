package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

func TestFormatRequestTraceShowsCacheLineage(t *testing.T) {
	read, miss, prompt, completion := 900, 100, 1000, 20
	settings := contract.Settings{Version: 1}
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "trace", WorkspacePath: t.TempDir()},
		Provider: requestTraceProvider{},
		Registry: orchestrator.NewRegistry(),
		Prompt:   orchestrator.PromptContext{},
		InitialUsageRecords: []contract.UsageRecord{{
			Seq: 1, Model: "minimax-m3", Purpose: contract.RequestPurposeMain,
			PromptTokens: &prompt, CompletionTokens: &completion,
			CacheReadTokens: &read, CacheMissTokens: &miss,
			PrefixHash: "0123456789abcdef-extra", Upstream: "Minimax",
			LogID: "request-1", Attribution: contract.CacheAttributionProvider,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := formatRequestTrace(engine)
	for _, value := range []string{"minimax-m3", "cache 90.0%", "0123456789abcdef", "request-1", "route Minimax"} {
		if !strings.Contains(got, value) {
			t.Fatalf("trace missing %q:\n%s", value, got)
		}
	}
}

type requestTraceProvider struct{}

func (requestTraceProvider) Chat(context.Context, contract.ChatRequest) (contract.ChatResponse, error) {
	return contract.ChatResponse{}, nil
}

func (requestTraceProvider) ListModels(context.Context) ([]contract.Model, error) {
	return nil, nil
}
