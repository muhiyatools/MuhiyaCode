package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// advisorSettings builds a two-model catalog so the advisor has something to
// choose between.
func advisorSettings() contract.Settings {
	settings := engineSettings()
	settings.Provider.Advisor = "auto" // this suite is about the advisor
	settings.Provider.ActiveModelID = "minimax-m3"
	settings.Provider.SubagentModelID = "deepseek-v4-pro"
	settings.Provider.Models = []contract.Model{
		{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 128_000},
		{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextLimit: 128_000},
	}
	return settings
}

func advisorEngine(t *testing.T, settings *contract.Settings, responses ...contract.ChatResponse) (*Engine, *scriptedProvider) {
	t.Helper()
	provider := &scriptedProvider{responses: responses}
	engine, err := NewEngine(EngineConfig{
		Settings: settings, Session: contract.Session{ID: "advisor", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, provider
}

// TestAdvisorAppliesThroughEngineRun is the regression that matters most: the
// advisor runs inside Engine.Run's prologue, and Run claims the task slot
// BEFORE the prologue. An advisor built on the busy-checked SwitchModel would
// be refused on every session and — under silent fallback — switch nothing,
// with no visible symptom. Exercising it end-to-end through Run is the only
// way that failure mode cannot hide.
func TestAdvisorAppliesThroughEngineRun(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"main":"deepseek-v4-pro","sub":"deepseek-v4-flash","why":"small session"}`},
		contract.ChatResponse{Content: "done"},
	)
	if _, _, err := engine.Run(context.Background(), "please update the widget configuration"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "deepseek-v4-pro" {
		t.Fatalf("advisor did not apply the main model through Run: %q", settings.Provider.ActiveModelID)
	}
	if settings.Provider.SubagentModelID != "deepseek-v4-flash" {
		t.Fatalf("advisor did not apply the sub model through Run: %q", settings.Provider.SubagentModelID)
	}
	// A switch must be accompanied by its invalidation event, or per-model cache
	// attribution silently lies.
	switches := 0
	for _, event := range engine.InvalidationEvents() {
		if event.Cause == contract.InvalidationModelSwitch {
			switches++
		}
	}
	if switches != 2 {
		t.Fatalf("expected one invalidation event per role change, got %d", switches)
	}
}

// TestAdvisorKeepIsTheQuietPath: the expected answer changes nothing and says
// nothing.
func TestAdvisorKeepIsTheQuietPath(t *testing.T) {
	settings := advisorSettings()
	var notices []string
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: `{"keep": true}`},
		{Content: "done"},
	}}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "advisor-keep", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Callbacks: contract.Callbacks{Notice: func(text string) { notices = append(notices, text) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "please update the widget configuration"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "minimax-m3" || settings.Provider.SubagentModelID != "deepseek-v4-pro" {
		t.Fatalf("keep changed the pairing: main=%q sub=%q", settings.Provider.ActiveModelID, settings.Provider.SubagentModelID)
	}
	for _, notice := range notices {
		if strings.Contains(notice, "Models for this session") {
			t.Fatalf("keep emitted a switch notice: %q", notice)
		}
	}
}

// TestAdvisorFailuresKeepConfiguredModels: every failure mode is silent and
// non-fatal. A session that starts beats a marginally better model.
func TestAdvisorFailuresKeepConfiguredModels(t *testing.T) {
	for _, row := range []struct{ name, answer string }{
		{"malformed json", "sorry, I cannot help with that"},
		{"unknown model id", `{"main":"gpt-9-turbo","sub":"gpt-9-turbo"}`},
		{"empty answer", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			settings := advisorSettings()
			engine, _ := advisorEngine(t, &settings,
				contract.ChatResponse{Content: row.answer},
				contract.ChatResponse{Content: "done"},
			)
			if _, _, err := engine.Run(context.Background(), "please update the widget configuration"); err != nil {
				t.Fatalf("a bad advisor answer broke the task: %v", err)
			}
			if settings.Provider.ActiveModelID != "minimax-m3" || settings.Provider.SubagentModelID != "deepseek-v4-pro" {
				t.Fatalf("configured pairing was disturbed: main=%q sub=%q", settings.Provider.ActiveModelID, settings.Provider.SubagentModelID)
			}
		})
	}
}

// TestModelsAreFrozenAfterTheFirstRequest is the headline caching invariant:
// once a session has made a request, nothing changes its models. The advisor
// gate is the first line; applyModelSwitch's own guard would be the second if
// the gate ever regressed.
func TestModelsAreFrozenAfterTheFirstRequest(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"keep": true}`},
		contract.ChatResponse{Content: "first done"},
		// If the advisor ran again on task two, THIS is the answer it would get.
		contract.ChatResponse{Content: `{"main":"deepseek-v4-flash","sub":"deepseek-v4-flash","why":"should never apply"}`},
		contract.ChatResponse{Content: "second done"},
	)
	if _, _, err := engine.Run(context.Background(), "please update the widget configuration"); err != nil {
		t.Fatal(err)
	}
	mainAfterFirst, subAfterFirst := settings.Provider.ActiveModelID, settings.Provider.SubagentModelID
	if _, _, err := engine.Run(context.Background(), "now update the other widget configuration"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != mainAfterFirst || settings.Provider.SubagentModelID != subAfterFirst {
		t.Fatalf("models changed mid-session: main %q→%q sub %q→%q", mainAfterFirst, settings.Provider.ActiveModelID, subAfterFirst, settings.Provider.SubagentModelID)
	}
}

func TestAdvisorSkippedWhenOffOrPinned(t *testing.T) {
	for _, row := range []struct {
		name  string
		apply func(*contract.Settings)
	}{
		{"advisor off", func(s *contract.Settings) { s.Provider.Advisor = "off" }},
		{"roles pinned by the user", func(s *contract.Settings) { s.Provider.RolesPinned = true }},
	} {
		t.Run(row.name, func(t *testing.T) {
			settings := advisorSettings()
			row.apply(&settings)
			engine, provider := advisorEngine(t, &settings,
				contract.ChatResponse{Content: "done"},
			)
			if _, _, err := engine.Run(context.Background(), "please update the widget configuration"); err != nil {
				t.Fatal(err)
			}
			// One request total means the advisor never fired.
			if len(provider.requests) != 1 {
				t.Fatalf("advisor ran despite %s: %d requests", row.name, len(provider.requests))
			}
			if settings.Provider.ActiveModelID != "minimax-m3" {
				t.Fatalf("pairing changed: %q", settings.Provider.ActiveModelID)
			}
		})
	}
}

// TestUtilityModelPrefersFlashWithFallback pins the instructing-model
// resolution: a Flash-class model when the catalog has one, the configured
// executor when it does not — never a hardcoded id that could vanish.
func TestUtilityModelPrefersFlashWithFallback(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings, contract.ChatResponse{Content: "done"})
	if got := engine.utilityModelID(); got != "deepseek-v4-flash" {
		t.Fatalf("utility model = %q, want the flash-class model", got)
	}
	bare := advisorSettings()
	bare.Provider.Models = bare.Provider.Models[:2] // no flash in the catalog
	bareEngine, _ := advisorEngine(t, &bare, contract.ChatResponse{Content: "done"})
	if got := bareEngine.utilityModelID(); got != "deepseek-v4-pro" {
		t.Fatalf("fallback utility model = %q, want the configured executor", got)
	}
}

// TestAdvisorCatalogDescribesWhatMatters: the advisor picks an executor partly
// on continuation support, so the catalog must state it.
func TestAdvisorCatalogDescribesWhatMatters(t *testing.T) {
	rendered := advisorCatalog(advisorSettings().Provider.Models)
	for _, want := range []string{"minimax-m3", "window", "family", "continuation yes"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("advisor catalog missing %q:\n%s", want, rendered)
		}
	}
}
