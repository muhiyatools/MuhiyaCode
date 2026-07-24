package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// advisorSettings builds a three-model catalog so the advisor has something to
// choose between.
func advisorSettings() contract.Settings {
	settings := engineSettings()
	settings.Provider.Advisor = "auto" // this suite is about the advisor
	settings.Provider.ActiveModelID = "minimax-m3"
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
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"needs deeper reasoning"}`},
		contract.ChatResponse{Content: "done"},
	)
	if _, _, err := engine.Run(context.Background(), "please update the widget configuration"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "deepseek-v4-pro" {
		t.Fatalf("advisor did not apply the model through Run: %q", settings.Provider.ActiveModelID)
	}
	// A switch must be accompanied by its invalidation event, or per-model cache
	// attribution silently lies.
	switches := 0
	for _, event := range engine.InvalidationEvents() {
		if event.Cause == contract.InvalidationModelSwitch {
			switches++
		}
	}
	if switches != 1 {
		t.Fatalf("expected exactly one invalidation event for the switch, got %d", switches)
	}
}

// TestAdvisorRunsAtEveryTaskBoundary: the model is chosen per task, not frozen
// for the session. The first task keeps the configured model; the second, told
// the work is heavier, moves — which the session-scoped advisor could never do.
func TestAdvisorRunsAtEveryTaskBoundary(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"keep":true}`}, // task 1 advisor
		contract.ChatResponse{Content: "first done"},
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"multi-file refactor"}`}, // task 2 advisor
		contract.ChatResponse{Content: "second done"},
	)
	if _, _, err := engine.Run(context.Background(), "rename one local variable"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatalf("the first task should have kept the configured model, got %q", settings.Provider.ActiveModelID)
	}
	if _, _, err := engine.Run(context.Background(), "refactor the loader across the package"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "deepseek-v4-pro" {
		t.Fatalf("the second task's advisor never ran or never applied: %q", settings.Provider.ActiveModelID)
	}
}

// TestAdvisorRefusesASwitchTheConversationCannotFit is the hard gate: a model
// with a smaller window is never adopted when the conversation would not fit,
// because the request assembler would silently drop the oldest messages —
// context destruction wearing the costume of a model upgrade.
func TestAdvisorRefusesASwitchTheConversationCannotFit(t *testing.T) {
	settings := advisorSettings()
	engine, _ := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"stronger coder"}`},
		contract.ChatResponse{Content: "done"},
	)
	// A conversation far larger than DeepSeek's 128k window but comfortable
	// inside MiniMax M3's 1M one.
	engine.latestPromptTokens, engine.latestPromptAvailable = 400_000, true

	if _, _, err := engine.Run(context.Background(), "keep going"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatalf("switched into a window the conversation cannot fit: %q", settings.Provider.ActiveModelID)
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
	if settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatalf("keep changed the model: %q", settings.Provider.ActiveModelID)
	}
	for _, notice := range notices {
		if strings.Contains(notice, "Model for this task") {
			t.Fatalf("keep emitted a switch notice: %q", notice)
		}
	}
}

// TestAdvisorFailuresKeepConfiguredModels: every failure mode is silent and
// non-fatal. A session that starts beats a marginally better model.
func TestAdvisorFailuresKeepConfiguredModels(t *testing.T) {
	for _, row := range []struct{ name, answer string }{
		{"malformed json", "sorry, I cannot help with that"},
		{"unknown model id", `{"model":"gpt-9-turbo","why":"no such model"}`},
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
			if settings.Provider.ActiveModelID != "minimax-m3" {
				t.Fatalf("configured model was disturbed: %q", settings.Provider.ActiveModelID)
			}
		})
	}
}

// TestTheModelNeverChangesInsideATask is the headline caching invariant. A task
// runs many requests against one growing conversation; if the model changed
// partway, every request after the change would re-bill the whole prefix as
// uncached. The advisor is deliberately confined to the task boundary, so the
// only way this can regress is by someone moving the call site.
func TestTheModelNeverChangesInsideATask(t *testing.T) {
	settings := advisorSettings()
	engine, provider := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"heavier task"}`}, // advisor
		contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}},
		contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("c2", "read_file", `{"path":"b.go"}`)}},
		contract.ChatResponse{Content: "done"},
	)
	if _, _, err := engine.Run(context.Background(), "refactor the loader across the package"); err != nil {
		t.Fatal(err)
	}
	// requests[0] is the advisor's own aux call on the utility model; every main
	// request after it must name one and the same model.
	if len(provider.requests) < 4 {
		t.Fatalf("expected the advisor call plus three main turns, got %d", len(provider.requests))
	}
	first := provider.requests[1].ModelID
	if first != "deepseek-v4-pro" {
		t.Fatalf("the task did not start on the advised model: %q", first)
	}
	for index, request := range provider.requests[1:] {
		if request.ModelID != first {
			t.Fatalf("main request %d switched models mid-task: %q → %q", index, first, request.ModelID)
		}
	}
}

func TestAdvisorSkippedWhenOffOrPinned(t *testing.T) {
	for _, row := range []struct {
		name  string
		apply func(*contract.Settings)
	}{
		{"advisor off", func(s *contract.Settings) { s.Provider.Advisor = "off" }},
		{"advisor pinned", func(s *contract.Settings) { s.Provider.Advisor = "pinned" }},
		{"model pinned by the user", func(s *contract.Settings) { s.Provider.RolesPinned = true }},
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
				t.Fatalf("model changed: %q", settings.Provider.ActiveModelID)
			}
		})
	}
}

// TestUtilityModelPrefersFlashWithFallback pins the aux-call model resolution:
// a Flash-class model when the catalog has one, the smallest window when it
// does not — never a hardcoded id that could vanish, and never empty.
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
		t.Fatalf("fallback utility model = %q, want the smallest window", got)
	}
	single := advisorSettings()
	single.Provider.Models = []contract.Model{{ID: "only", Name: "Only"}} // no window recorded
	singleEngine, _ := advisorEngine(t, &single, contract.ChatResponse{Content: "done"})
	if got := singleEngine.utilityModelID(); got == "" {
		t.Fatal("a single-model catalog must still resolve a utility model")
	}
}

// TestAdvisorCatalogDescribesWhatMatters: continuation support decides how well
// a long conversation keeps hitting cache, so the catalog must state it.
func TestAdvisorCatalogDescribesWhatMatters(t *testing.T) {
	rendered := advisorCatalog(advisorSettings().Provider.Models)
	for _, want := range []string{"minimax-m3", "window", "family", "continuation yes"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("advisor catalog missing %q:\n%s", want, rendered)
		}
	}
}

func TestAdvisorRunsWhenPinnedButRouted(t *testing.T) {
	settings := advisorSettings()
	settings.Provider.RolesPinned = true
	settings.Provider.Advisor = "routed"
	
	engine, provider := advisorEngine(t, &settings,
		contract.ChatResponse{Content: `{"model":"deepseek-v4-pro","why":"explicitly routed"}`},
		contract.ChatResponse{Content: "done"},
	)
	if _, _, err := engine.Run(context.Background(), "do the work"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) < 2 {
		t.Fatalf("advisor did not run despite routed mode")
	}
	if settings.Provider.ActiveModelID != "deepseek-v4-pro" {
		t.Fatalf("model did not change: %q", settings.Provider.ActiveModelID)
	}
}
