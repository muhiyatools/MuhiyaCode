package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

// Feature 008 T017 — mid-session model-switch warning (contracts/
// model-switch-warning.md). Trigger-matrix coverage in this file:
//
//   - Same-model reselect → silent no-op (no modal, no dispatch): covered
//     directly (TestChooseModelSameModelIsSilentNoOp), including with the
//     warning trigger armed, proving the no-op check wins.
//   - Zero completed requests → immediate dispatch, no modal: covered directly
//     (TestChooseModelFreshSessionDispatchesImmediately).
//   - ≥1 completed request + different model → warning modal before any
//     dispatch: covered directly via EngineConfig.InitialUsageRecords, which
//     feeds Engine.UsageAggregate().Requests at construction — the exact value
//     chooseModel consults (TestChooseModelMidSessionWarnsBeforeDispatch,
//     ...ProceedDispatches, ...EscCancelsWithoutSideEffects).
//   - "Refresh from gateway" branch and the CLI config path are untouched code
//     paths (no chooseModel call) and are NOT exercised here.
//
// MS-2/MS-7 content requirements are additionally locked by the pure builder
// test (TestBuildModelSwitchWarningContent) so the copy stays honest even if
// the modal plumbing changes.

// modelSwitchRuntime mirrors testRuntime but seeds the engine with usage
// records so Engine.UsageAggregate().Requests reflects a mid-session state.
func modelSwitchRuntime(t *testing.T, records []contract.UsageRecord) Runtime {
	t.Helper()
	settings := &contract.Settings{Version: 1, PermissionMode: contract.PermissionNormal, Effort: contract.EffortMedium}
	settings.Provider.Type = "openai-compatible"
	settings.Provider.ActiveModelID = "main"
	settings.Provider.SubagentModelID = "fast"
	settings.Provider.Models = []contract.Model{{ID: "main", Name: "Main", ContextLimit: 64000}, {ID: "fast", Name: "Fast", ContextLimit: 32000}}
	settings.RTL.Mode = "auto"
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{Settings: settings, Provider: inertProvider{}, Registry: orchestrator.NewRegistry(), InitialUsageRecords: records})
	if err != nil {
		t.Fatal(err)
	}
	return Runtime{Engine: engine, Settings: settings, Session: contract.Session{ID: "session", WorkspacePath: `C:\work\muhiya`, Title: "test"}}
}

func oneRequestRecords() []contract.UsageRecord {
	prompt := 1200
	return []contract.UsageRecord{{Seq: 1, Stream: contract.UsageStreamMain, Model: "main", PromptTokens: &prompt}}
}

// Same-model reselect is a silent no-op even mid-session: no modal, no
// settings write (trigger matrix row 2 beats row 1).
func TestChooseModelSameModelIsSilentNoOp(t *testing.T) {
	m := NewModel(Options{Runtime: modelSwitchRuntime(t, oneRequestRecords()), Version: "test"})
	if cmd := m.chooseModel("main", "main"); cmd != nil {
		t.Fatal("same-model reselect returned a dispatch command")
	}
	if m.modal != nil {
		t.Fatal("same-model reselect opened a modal")
	}
	if m.runtime.Settings.Provider.ActiveModelID != "main" {
		t.Fatalf("same-model reselect mutated settings: %q", m.runtime.Settings.Provider.ActiveModelID)
	}
	// The subagent role checks its own current ID.
	if cmd := m.chooseModel("subagent", "fast"); cmd != nil || m.modal != nil {
		t.Fatal("same-model subagent reselect was not a silent no-op")
	}
}

// Zero completed requests: the switch applies immediately with no modal — the
// pre-008 dispatch path, byte-identical (fallback settings mutation here, since
// the fixture wires no SetModel/SaveSettings actions).
func TestChooseModelFreshSessionDispatchesImmediately(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m.chooseModel("main", "fast")
	if m.modal != nil {
		t.Fatal("fresh session opened a warning modal")
	}
	if m.runtime.Settings.Provider.ActiveModelID != "fast" {
		t.Fatalf("fresh-session switch did not dispatch: %q", m.runtime.Settings.Provider.ActiveModelID)
	}
	m.chooseModel("subagent", "main")
	if m.modal != nil || m.runtime.Settings.Provider.SubagentModelID != "main" {
		t.Fatalf("fresh-session subagent switch: modal=%v id=%q", m.modal != nil, m.runtime.Settings.Provider.SubagentModelID)
	}
}

// ≥1 completed request + different model: the warning opens INSTEAD of the
// dispatch (MS-1), with exactly two choices and Cancel default-selected (MS-3).
func TestChooseModelMidSessionWarnsBeforeDispatch(t *testing.T) {
	m := NewModel(Options{Runtime: modelSwitchRuntime(t, oneRequestRecords()), Version: "test"})
	if cmd := m.chooseModel("main", "fast"); cmd != nil {
		t.Fatal("warning path returned a dispatch command")
	}
	if m.modal == nil {
		t.Fatal("mid-session switch did not open the warning modal")
	}
	if m.runtime.Settings.Provider.ActiveModelID != "main" {
		t.Fatalf("dispatch ran before the warning (MS-1): %q", m.runtime.Settings.Provider.ActiveModelID)
	}
	if m.modal.title != "Switch model mid-session?" {
		t.Fatalf("title = %q", m.modal.title)
	}
	for _, expected := range []string{"main", "fast", "cold", "uncached rate", "Pricing may also differ"} {
		if !strings.Contains(m.modal.message, expected) {
			t.Fatalf("warning message missing %q:\n%s", expected, m.modal.message)
		}
	}
	if len(m.modal.choices) != 2 {
		t.Fatalf("choices = %d, want 2", len(m.modal.choices))
	}
	if m.modal.choices[0].Label != "Switch model" || m.modal.choices[1].Label != "Cancel" {
		t.Fatalf("choice labels = %q / %q", m.modal.choices[0].Label, m.modal.choices[1].Label)
	}
	if !m.modal.choices[1].Recommended {
		t.Fatal("Cancel does not carry Recommended (MS-3)")
	}
	if m.modal.selected != 1 {
		t.Fatalf("default selection = %d, want 1 (Cancel)", m.modal.selected)
	}
}

// Proceeding from the warning runs the exact dispatch the immediate path runs
// (MS-5): here the fallback settings mutation, since no actions are wired.
func TestChooseModelMidSessionProceedDispatches(t *testing.T) {
	m := NewModel(Options{Runtime: modelSwitchRuntime(t, oneRequestRecords()), Version: "test"})
	m.chooseModel("main", "fast")
	if m.modal == nil {
		t.Fatal("warning modal missing")
	}
	// Move the selection from Cancel (default) up to "Switch model" and confirm.
	m.handleModalKey(tea.KeyPressMsg{Code: tea.KeyUp})
	m.handleModalKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.modal != nil {
		t.Fatal("modal stayed open after proceed")
	}
	if m.runtime.Settings.Provider.ActiveModelID != "fast" {
		t.Fatalf("proceed did not dispatch: %q", m.runtime.Settings.Provider.ActiveModelID)
	}
}

// Cancel via Esc (and via the Cancel choice) leaves state untouched (MS-4).
func TestChooseModelMidSessionEscCancelsWithoutSideEffects(t *testing.T) {
	m := NewModel(Options{Runtime: modelSwitchRuntime(t, oneRequestRecords()), Version: "test"})
	m.chooseModel("main", "fast")
	if m.modal == nil {
		t.Fatal("warning modal missing")
	}
	m.handleModalKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.modal != nil {
		t.Fatal("Esc did not close the modal")
	}
	if m.runtime.Settings.Provider.ActiveModelID != "main" {
		t.Fatalf("Esc-cancel mutated settings: %q", m.runtime.Settings.Provider.ActiveModelID)
	}
	// The default-selected Cancel choice confirmed with Enter is also side-effect free.
	m.chooseModel("main", "fast")
	if cmd := m.handleModalKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("Cancel returned a command")
	}
	if m.modal != nil || m.runtime.Settings.Provider.ActiveModelID != "main" {
		t.Fatalf("Cancel choice mutated state: modal=%v id=%q", m.modal != nil, m.runtime.Settings.Provider.ActiveModelID)
	}
}

// MS-2/MS-7: the warning copy names the role and both model IDs, states the
// cold cache restart and the qualitative pricing note, and fabricates no cost
// figures (no currency amounts).
func TestBuildModelSwitchWarningContent(t *testing.T) {
	title, message := buildModelSwitchWarning("subagent", "old-model", "new-model")
	if title != "Switch model mid-session?" {
		t.Fatalf("title = %q", title)
	}
	for _, expected := range []string{"subagent", "old-model", "new-model", "cache restarts cold", "uncached rate", "Pricing may also differ"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message missing %q:\n%s", expected, message)
		}
	}
	if strings.Contains(message, "$") || strings.ContainsAny(message, "0123456789") {
		t.Fatalf("message fabricates cost/number content (MS-7):\n%s", message)
	}
}
