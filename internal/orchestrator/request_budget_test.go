package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestBuildRequestReportsIrreducibleNewestUnitOverflow(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("x", 20_000)})

	built := history.BuildRequestWithMetadata("system", 4_000, 1_000)

	if !built.OverBudget {
		t.Fatalf("newest unit exceeds the prompt budget and must be reported: %+v", built)
	}
	if built.EstimatedPromptTokens <= built.PromptBudgetTokens {
		t.Fatalf("overflow accounting is inconsistent: %+v", built)
	}
	if len(built.Messages) < 2 {
		t.Fatal("overflow reporting must retain the newest unit for diagnostics")
	}
}

func TestBuildRequestUsesAndPersistsProviderCalibration(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("a", 1_000)})
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("b", 1_000)})

	heuristic := history.BuildRequestWithMetadata("system", 700, 0)
	if heuristic.EstimateSource != "heuristic" || len(heuristic.Messages) != 3 {
		t.Fatalf("unexpected heuristic window: %+v", heuristic)
	}

	history.Calibrate(1_000, 2_000)
	calibrated := history.BuildRequestWithMetadata("system", 700, 0)
	if calibrated.EstimateSource != "provider-calibrated" || len(calibrated.Messages) != 2 {
		t.Fatalf("calibration did not tighten the request window: %+v", calibrated)
	}
	if calibrated.SerializedMessageBytes == 0 {
		t.Fatal("serialized message byte measurement was not reported")
	}

	resumed := NewHistory(history.Snapshot(), nil)
	restored := resumed.BuildRequestWithMetadata("system", 700, 0)
	if restored.EstimateSource != "provider-calibrated" ||
		restored.EstimatedPromptTokens != calibrated.EstimatedPromptTokens ||
		len(restored.Messages) != len(calibrated.Messages) {
		t.Fatalf("calibration was not stable across restart: before=%+v after=%+v", calibrated, restored)
	}
}

func TestRequestCompilerUpdatesUnitsIncrementally(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: "first"})

	initial := history.BuildRequestWithMetadata("system", 4_000, 0)
	cached := history.BuildRequestWithMetadata("system", 4_000, 0)
	if initial.CompilerCacheHit || !cached.CompilerCacheHit || cached.CompiledUnits != 1 {
		t.Fatalf("compiler cache did not warm: initial=%+v cached=%+v", initial, cached)
	}

	history.Append(contract.Message{Role: contract.RoleAssistant, Content: "same unit"})
	extended := history.BuildRequestWithMetadata("system", 4_000, 0)
	if !extended.CompilerCacheHit || extended.CompiledUnits != 1 ||
		extended.EstimatedPromptTokens <= cached.EstimatedPromptTokens {
		t.Fatalf("assistant append did not extend the cached unit: %+v", extended)
	}

	history.Append(contract.Message{Role: contract.RoleUser, Content: "next unit"})
	next := history.BuildRequestWithMetadata("system", 4_000, 0)
	if !next.CompilerCacheHit || next.CompiledUnits != 2 {
		t.Fatalf("user append did not create a cached unit: %+v", next)
	}

	history.Calibrate(30, requestCalibrationChars(next.Messages, nil))
	recalibrated := history.BuildRequestWithMetadata("system", 4_000, 0)
	if recalibrated.CompilerCacheHit || recalibrated.EstimateSource != "provider-calibrated" {
		t.Fatalf("calibration did not invalidate compiled token costs: %+v", recalibrated)
	}
}

func TestProviderCalibrationDoesNotDoubleCountFraming(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("a", 400)})
	definitions := []contract.ToolDefinition{definition("lookup", "lookup", map[string]any{
		"query": map[string]any{"type": "string"},
	}, []string{"query"})}
	initial := history.BuildRequestWithMetadata("system", 10_000, 0)
	sentChars := requestCalibrationChars(initial.Messages, definitions)
	reported := 173
	history.Calibrate(reported, sentChars)

	calibrated := history.BuildRequestWithMetadata("system", 10_000, 0)
	wireEstimate := calibrated.EstimatedPromptTokens + history.TokensForChars(mustJSONSize(t, definitions))
	delta := wireEstimate - reported
	if delta < 0 {
		delta = -delta
	}
	errorRate := float64(delta) / float64(reported)
	if errorRate > 0.05 {
		t.Fatalf("calibrated estimate error %.2f%% exceeds 5%%: estimate=%d reported=%d", errorRate*100, wireEstimate, reported)
	}
}

func mustJSONSize(t *testing.T, value any) int {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}

func TestExactTokenPreflightPrunesBeforeSend(t *testing.T) {
	settings := engineSettings()
	settings.Provider.ActiveModelID = "deepseek-test"
	settings.Provider.Models = []contract.Model{{ID: "deepseek-test", Name: "DeepSeek Test", ContextLimit: 1_000}}
	provider := &exactCountingProvider{
		scriptedProvider: &scriptedProvider{},
		count: func(request contract.ChatRequest) int {
			if len(request.Messages) > 2 {
				return 1_200
			}
			return 300
		},
	}
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("a", 400)})
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("b", 400)})
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "exact", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(),
		History:  history,
	})
	if err != nil {
		t.Fatal(err)
	}
	initial := history.BuildRequestWithMetadata("system", 1_000, 0)
	built, effectiveReserve, err := engine.exactTokenPreflight(tokenPreflightInput{System: "system", Built: initial, SessionPin: "exact:main"})
	if err != nil {
		t.Fatalf("exact preflight: %v", err)
	}
	if built.EstimateSource != "provider-tokenizer" || built.EstimatedWireTokens != 300 {
		t.Fatalf("exact token diagnostics missing: %+v", built)
	}
	if !built.WindowDropped || len(built.Messages) > 2 || effectiveReserve == 0 {
		t.Fatalf("exact overflow did not force pruning: reserve=%d build=%+v", effectiveReserve, built)
	}
	if provider.countCalls != 3 {
		t.Fatalf("counter called %d times, want 3 bounded pruning passes", provider.countCalls)
	}
}

type exactCountingProvider struct {
	*scriptedProvider
	count      func(contract.ChatRequest) int
	countCalls int
}

func (p *exactCountingProvider) CountRequestTokens(request contract.ChatRequest) (int, error) {
	p.countCalls++
	return p.count(request), nil
}

var _ contract.Provider = (*exactCountingProvider)(nil)
var _ requestTokenCounter = (*exactCountingProvider)(nil)
