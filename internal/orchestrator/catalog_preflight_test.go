package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The gateway's model rows live in its production database, so a row can be
// renamed, deactivated, or hidden from this client between sessions. Before the
// preflight that surfaced as a 404 on the FIRST REAL REQUEST — mid-task, after
// the user had already said what they wanted.

func catalogEngine(t *testing.T, models []contract.Model, configured string) (*Engine, *[]string) {
	t.Helper()
	var notices []string
	settings := engineSettings()
	settings.Provider.Models = models
	settings.Provider.ActiveModelID = configured
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "catalog", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Callbacks: contract.Callbacks{Notice: func(line string) { notices = append(notices, line) }},
		Prompt:    PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, &notices
}

func TestCatalogPreflightSubstitutesAMissingModel(t *testing.T) {
	// deepseek-v4-pro is configured but absent — the exact shape of a row that
	// exists in one deploy's database and not another's.
	engine, notices := catalogEngine(t,
		[]contract.Model{
			{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextLimit: 128_000},
		},
		"deepseek-v4-pro")

	engine.reconcileCatalog()

	if got := engine.settings.Provider.ActiveModelID; got == "deepseek-v4-pro" {
		t.Fatal("the missing model was left configured — the first request will 404 mid-task")
	}
	// The largest window wins: the session has one model and its conversation
	// has to fit in it.
	if got := engine.settings.Provider.ActiveModelID; got != "minimax-m3" {
		t.Fatalf("substitute = %q, want the largest-window model", got)
	}
	if len(*notices) == 0 || !strings.Contains(strings.Join(*notices, " "), "deepseek-v4-pro") {
		t.Fatalf("the substitution was silent; the user must be told: %v", *notices)
	}
}

// A healthy catalog must be completely silent — this runs on every session
// start, so a false alarm would be noise on every launch.
func TestCatalogPreflightIsSilentWhenEverythingResolves(t *testing.T) {
	engine, notices := catalogEngine(t,
		[]contract.Model{
			{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
			{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
		},
		"minimax-m3")

	engine.reconcileCatalog()

	if engine.settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatal("a healthy catalog must not be rewritten")
	}
	if len(*notices) != 0 {
		t.Fatalf("a healthy catalog must be silent, got %v", *notices)
	}
}

// Once the session has spent a request the preflight stands down: the advisor
// owns model changes from then on, and it weighs the cold-start cost this
// blunt substitution cannot see.
func TestCatalogPreflightWillNotSwitchMidSession(t *testing.T) {
	engine, _ := catalogEngine(t,
		[]contract.Model{{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000}},
		"gone-model")
	if err := engine.recordUsageAndEmit(func() error {
		return engine.recordMainUsage(context.Background(), mainUsageObservation{
			model: "gone-model",
			usage: contract.Usage{PromptTokens: 10, CompletionTokens: 1, PromptTokensAvailable: true},
		})
	}); err != nil {
		t.Fatal(err)
	}

	engine.reconcileCatalog()

	if got := engine.settings.Provider.ActiveModelID; got != "gone-model" {
		t.Fatalf("the model was changed after the session had already spent a request (now %q)", got)
	}
}
