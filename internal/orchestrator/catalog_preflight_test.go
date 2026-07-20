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

func catalogEngine(t *testing.T, models []contract.Model, main, sub string) (*Engine, *[]string) {
	t.Helper()
	var notices []string
	settings := engineSettings()
	settings.Provider.Models = models
	settings.Provider.ActiveModelID = main
	settings.Provider.SubagentModelID = sub
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

func TestCatalogPreflightSubstitutesAMissingExecutor(t *testing.T) {
	// deepseek-v4-pro is configured but absent — the exact shape of a row that
	// exists in one deploy's database and not another's.
	engine, notices := catalogEngine(t,
		[]contract.Model{
			{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
			{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextLimit: 128_000},
		},
		"minimax-m3", "deepseek-v4-pro")

	engine.reconcileCatalog()

	if got := engine.settings.Provider.SubagentModelID; got == "deepseek-v4-pro" {
		t.Fatal("the missing executor was left configured — the first dispatch will 404 mid-task")
	}
	if engine.settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatalf("the main model was present and must not have been touched, got %q", engine.settings.Provider.ActiveModelID)
	}
	// The executor must prefer a continuation-capable family: the session-long
	// cache chain is built on it.
	if got := engine.settings.Provider.SubagentModelID; got != "deepseek-v4-flash" {
		t.Fatalf("substitute = %q, want the continuation-capable deepseek", got)
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
		"minimax-m3", "deepseek-v4-pro")

	engine.reconcileCatalog()

	if engine.settings.Provider.ActiveModelID != "minimax-m3" || engine.settings.Provider.SubagentModelID != "deepseek-v4-pro" {
		t.Fatal("a healthy catalog must not be rewritten")
	}
	if len(*notices) != 0 {
		t.Fatalf("a healthy catalog must be silent, got %v", *notices)
	}
}

// Once the session has spent a request the pairing is frozen: switching then
// would cold-start the main prefix and break the executor's continuation chain,
// which is the whole reason models are session-stable.
func TestCatalogPreflightWillNotSwitchMidSession(t *testing.T) {
	engine, _ := catalogEngine(t,
		[]contract.Model{{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000}},
		"minimax-m3", "gone-model")
	if err := engine.recordUsageAndEmit(func() error {
		return e2eUsage(engine)
	}); err != nil {
		t.Fatal(err)
	}

	engine.reconcileCatalog()

	if got := engine.settings.Provider.SubagentModelID; got != "gone-model" {
		t.Fatalf("the pairing was changed after the session had already spent a request (now %q)", got)
	}
}

// e2eUsage books one request against the session so the frozen-pairing branch
// can be exercised without a live provider.
func e2eUsage(engine *Engine) error {
	return engine.recordIsolatedUsage(context.Background(), "minimax-m3", ":main", contract.Usage{PromptTokens: 10, CompletionTokens: 1, PromptTokensAvailable: true}, true, nil, nil)
}
