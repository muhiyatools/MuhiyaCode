package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestContextBudgetUsesActualSharedWindowOutputOnce(t *testing.T) {
	settings := advisorSettings()
	settings.Provider.ActiveModelID = "minimax-m3"
	settings.Provider.Models = []contract.Model{{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 100_000, MaxOutput: 20_000}}
	engine, _ := advisorEngine(t, &settings)
	budget := engine.contextBudgetFor("minimax-m3", 3_000)
	if budget.Output != 16_000 || budget.ReservedTokens != 16_000+3_000+protocolMarginTokens {
		t.Fatalf("shared-window budget = %+v", budget)
	}
	if budget.UsableInput+budget.ReservedTokens != budget.Limit {
		t.Fatalf("budget does not reconcile: %+v", budget)
	}
}

func TestContextBudgetDoesNotSubtractSeparateOutput(t *testing.T) {
	settings := advisorSettings()
	settings.Provider.ActiveModelID = "deepseek-v4-pro"
	settings.Provider.Models = []contract.Model{{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 100_000, MaxOutput: 64_000}}
	engine, _ := advisorEngine(t, &settings)
	budget := engine.contextBudgetFor("deepseek-v4-pro", 3_000)
	if budget.ReservedTokens != 3_000+protocolMarginTokens+separateWindowGrowthMarginTokens {
		t.Fatalf("separate-output model reserved output inside input window: %+v", budget)
	}
}

func TestRequestBuildReportsOversizedNewestUnit(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("x", 80_000)})
	built := history.BuildRequestWithMetadata("system", 10_000, 1_000)
	if !built.OverBudget || built.EstimatedTokens <= built.AvailableTokens {
		t.Fatalf("oversized newest unit was not reported: %+v", built)
	}
}

func TestResumePressureIgnoresAuxiliaryPromptMeasurements(t *testing.T) {
	settings := engineSettings()
	mainTokens, auxTokens := 40_000, 50
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Provider: &scriptedProvider{},
		Registry: NewRegistry(),
		History:  history,
		InitialUsageRecords: []contract.UsageRecord{
			{Seq: 1, Stream: contract.UsageStreamMain, PromptTokens: &mainTokens},
			{Seq: 2, Stream: contract.UsageStreamAux, PromptTokens: &auxTokens},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pressure := engine.contextPressure()
	if pressure.Estimated || pressure.Tokens != mainTokens {
		t.Fatalf("pressure used auxiliary measurement: %+v", pressure)
	}
}
