package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Cache economics of a model switch. Provider caches are per-model, so moving a
// long conversation to a model this session has never used re-reads every token
// at full price — the single most expensive decision the harness can make.

func TestColdSwitchOnALargeConversationIsRefused(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"stronger coder"}`},
		contract.ChatResponse{Content: "done"},
	)
	// 50k comfortably FITS deepseek-v4-pro's 128k window, so the fit gate cannot
	// be what stops this — only the cost gate can. And deepseek-v4-pro has never
	// run in this session, so the switch would re-send all 50k uncached.
	engine.latestPromptTokens, engine.latestPromptAvailable = 50_000, true

	if _, _, err := engine.Run(context.Background(), "keep going"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatalf("paid a 90k cold start for a switch: model = %q", settings.Provider.ActiveModelID)
	}
}

func TestColdSwitchEarlyInASessionIsAllowed(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"needs deeper reasoning"}`},
		contract.ChatResponse{Content: "done"},
	)
	// A small conversation: the cold start is a rounding error, so capability wins.
	engine.latestPromptTokens, engine.latestPromptAvailable = 4_000, true

	if _, _, err := engine.Run(context.Background(), "refactor the loader"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "deepseek-v4-pro" {
		t.Fatalf("a cheap early switch was refused: model = %q", settings.Provider.ActiveModelID)
	}
}

// Returning to a model this session already warmed is cheap at ANY size: its
// provider cache may still hold the prefix.
func TestSwitchBackToAWarmModelIsAllowedEvenWhenLarge(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"back to the coder"}`},
		contract.ChatResponse{Content: "done"},
	)
	// The same 50k that the cold case refuses: identical size, identical window
	// fit — the ONLY difference is that this model is already warm here.
	engine.latestPromptTokens, engine.latestPromptAvailable = 50_000, true
	warmUp(engine, "deepseek-v4-pro")

	if _, _, err := engine.Run(context.Background(), "continue the refactor"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "deepseek-v4-pro" {
		t.Fatalf("refused to return to a warm model: %q", settings.Provider.ActiveModelID)
	}
}

func TestSwitchCostReportsWarmthAndSize(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings)
	engine.latestPromptTokens, engine.latestPromptAvailable = 50_000, true

	cold := engine.switchCost("deepseek-v4-pro")
	if cold.Warm || cold.Affordable || cold.ColdStartTokens != 50_000 {
		t.Fatalf("a cold 50k switch should be unaffordable and sized: %+v", cold)
	}
	warmUp(engine, "deepseek-v4-pro")
	warm := engine.switchCost("deepseek-v4-pro")
	if !warm.Warm || !warm.Affordable {
		t.Fatalf("a warm model should be affordable at any size: %+v", warm)
	}
}

// warmUp books a main-stream request on a model at the CURRENT history
// revision, exactly as recordMainUsage does after a real turn.
func warmUp(engine *Engine, modelID string) {
	engine.taskMu.Lock()
	defer engine.taskMu.Unlock()
	engine.usageRecords = append(engine.usageRecords, contract.UsageRecord{Model: modelID, Stream: contract.UsageStreamMain, Pin: ":main"})
	engine.noteModelWarm(modelID, engine.history.RewriteVersion())
}

// An aux call — the advisor itself, a compaction summary — runs a tiny one-shot
// prompt on a cheap model and never sends the conversation. If that counted as
// warmth, the advisor would be handed the utility model as its cheapest option
// and every switch to it would cold-start the whole session.
func TestAuxTrafficDoesNotWarmAModel(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings)
	if err := engine.recordUsageAndEmit(func() error {
		return engine.recordAuxUsage(context.Background(), "deepseek-v4-flash", ":aux", contract.Usage{PromptTokens: 200, PromptTokensAvailable: true}, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if engine.modelWarmThisSession("deepseek-v4-flash") {
		t.Fatal("an aux request marked the utility model warm")
	}
	if warm := engine.warmModelsThisSession(); len(warm) != 0 {
		t.Fatalf("aux traffic leaked into the warm list: %v", warm)
	}
}

// Compaction and pressure trims rewrite earlier messages in place, so every
// model's cached prefix describes a conversation that no longer exists. Warmth
// has to retire with it — otherwise the advisor prices a full cold start as
// free and does the one thing this gate exists to prevent.
func TestRewritingHistoryRetiresWarmth(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings)
	warmUp(engine, "deepseek-v4-pro")
	if !engine.modelWarmThisSession("deepseek-v4-pro") {
		t.Fatal("the model should be warm before the rewrite")
	}

	engine.history.CompactTo("summary of everything so far", 2)

	if engine.modelWarmThisSession("deepseek-v4-pro") {
		t.Fatal("warmth survived a compaction — the cached prefix no longer exists")
	}
	if warm := engine.warmModelsThisSession(); len(warm) != 0 {
		t.Fatalf("the warm list survived a compaction: %v", warm)
	}
	engine.latestPromptTokens, engine.latestPromptAvailable = 50_000, true
	if engine.switchCost("deepseek-v4-pro").Affordable {
		t.Fatal("a 50k switch was priced as affordable against a dead cache")
	}
}

// The advisor must be TOLD the cost, or it optimizes capability in a vacuum.
func TestAdvisorPromptCarriesSwitchEconomics(t *testing.T) {
	settings := advisorSettings()
	engine, provider := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"keep":true}`},
		contract.ChatResponse{Content: "done"},
	)
	engine.latestPromptTokens, engine.latestPromptAvailable = 30_000, true
	if _, _, err := engine.Run(context.Background(), "do a thing"); err != nil {
		t.Fatal(err)
	}
	var advisorPrompt string
	for _, request := range provider.requests {
		if strings.Contains(request.SessionID, ":sub:advisor") {
			for _, message := range request.Messages {
				if message.Role == contract.RoleUser {
					advisorPrompt = message.Content
				}
			}
		}
	}
	if advisorPrompt == "" {
		t.Fatal("the advisor never ran")
	}
	for _, want := range []string{"SWITCH COST", "uncached", "Already warm"} {
		if !strings.Contains(advisorPrompt, want) {
			t.Fatalf("advisor prompt missing %q:\n%s", want, advisorPrompt)
		}
	}
}
