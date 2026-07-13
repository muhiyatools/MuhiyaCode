package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// partialRuntime returns a runtime with settings + session but no engine — the
// shape the loading shell receives during two-stage launch.
func partialRuntime(full Runtime) Runtime {
	return Runtime{Settings: full.Settings, Session: full.Session}
}

// TestTwoStageStartupShowsShellThenHydrates (US1 T021/T014) proves the composer
// shell renders immediately with a loading cue, then adopts the runtime and the
// recent transcript once hydration completes.
func TestTwoStageStartupShowsShellThenHydrates(t *testing.T) {
	full := testRuntime(t)
	m := NewModel(Options{
		Runtime: partialRuntime(full),
		Hydrate: func(context.Context) (HydratedRuntime, error) { return HydratedRuntime{Runtime: full}, nil },
		Version: "test",
	})
	if !m.loading {
		t.Fatal("two-stage launch should start in the loading state")
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	// The shell renders (header from the partial runtime, no panic) and shows a cue.
	if !strings.Contains(ansi.Strip(m.View().Content), "Loading workspace") {
		t.Fatal("loading cue not shown during hydration")
	}
	// Init must schedule the hydration command.
	if m.Init() == nil {
		t.Fatal("Init did not schedule hydration")
	}
	// Hydration completes with a restored transcript.
	updated, _ = m.Update(hydratedMsg{result: HydratedRuntime{Runtime: full, Recent: []contract.Event{{Role: "assistant", Content: "restored reply"}}}})
	m = updated.(*Model)
	if m.loading {
		t.Fatal("still loading after hydration completed")
	}
	if m.runtime.Engine == nil {
		t.Fatal("engine not adopted on hydration")
	}
	if !strings.Contains(ansi.Strip(m.transcriptContent), "restored reply") {
		t.Fatal("recent transcript not loaded on hydration")
	}
}

// TestTwoStageDefersSubmitUntilReady (US1 T021/T040) proves a prompt entered while
// loading is held and sent exactly once, after the runtime is ready.
func TestTwoStageDefersSubmitUntilReady(t *testing.T) {
	full := testRuntime(t)
	m := NewModel(Options{
		Runtime: partialRuntime(full),
		Hydrate: func(context.Context) (HydratedRuntime, error) { return HydratedRuntime{Runtime: full}, nil },
		Version: "test",
	})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	m = typeInto(t, m, "hello there")
	// Enter during loading stashes the prompt rather than submitting.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*Model)
	if m.pendingSubmit != "hello there" {
		t.Fatalf("prompt not stashed during loading: %q", m.pendingSubmit)
	}
	if m.input.Value() != "" {
		t.Fatal("composer not cleared after stashing the deferred prompt")
	}
	if m.busy {
		t.Fatal("submit happened before the runtime was ready")
	}
	// Hydration sends the deferred prompt exactly once.
	updated, _ = m.Update(hydratedMsg{result: HydratedRuntime{Runtime: full}})
	m = updated.(*Model)
	if m.pendingSubmit != "" {
		t.Fatal("deferred prompt not consumed")
	}
	if !m.busy {
		t.Fatal("deferred prompt was not submitted after hydration")
	}
}

// TestTwoStageInitialPromptDeferred (US1 T021) proves a CLI initial prompt is held
// until the runtime is ready.
func TestTwoStageInitialPromptDeferred(t *testing.T) {
	full := testRuntime(t)
	m := NewModel(Options{
		Runtime:       partialRuntime(full),
		InitialPrompt: "do the thing",
		Hydrate:       func(context.Context) (HydratedRuntime, error) { return HydratedRuntime{Runtime: full}, nil },
		Version:       "test",
	})
	if m.pendingSubmit != "do the thing" {
		t.Fatalf("initial prompt not deferred: %q", m.pendingSubmit)
	}
}
