package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// TestParseChecklistAcceptsPlainGitHubCheckboxes is the headline parser
// contract: the format the instructions teach the model to write — a bare
// GitHub checklist with no status parenthetical — must parse. (The predecessor
// plan.md parser required a "(pending|in_progress|completed)" suffix, so a
// plain checklist silently produced zero steps.)
func TestParseChecklistAcceptsPlainGitHubCheckboxes(t *testing.T) {
	plan := ParseChecklist("- [x] Read the config loader\n- [ ] Add validation\n- [ ] Wire the tests\n")
	if len(plan.Steps) != 3 {
		t.Fatalf("plain checklist parsed to %d steps, want 3: %+v", len(plan.Steps), plan.Steps)
	}
	if plan.Steps[0].Status != contract.PlanCompleted {
		t.Errorf("checked box = %q, want completed", plan.Steps[0].Status)
	}
	if plan.Steps[1].Status != contract.PlanPending {
		t.Errorf("unchecked box = %q, want pending", plan.Steps[1].Status)
	}
	if plan.Steps[0].Title != "Read the config loader" {
		t.Errorf("title = %q", plan.Steps[0].Title)
	}
}

func TestParseChecklistStatusMarkersAndProse(t *testing.T) {
	for _, row := range []struct {
		name  string
		input string
		want  contract.PlanStatus
	}{
		{"in progress", "- [ ] Add validation (in progress)", contract.PlanInProgress},
		{"legacy underscore", "- [ ] Add validation (in_progress)", contract.PlanInProgress},
		{"redundant pending", "- [ ] Add validation (pending)", contract.PlanPending},
		{"redundant completed", "- [x] Add validation (completed)", contract.PlanCompleted},
		{"asterisk bullet", "* [ ] Add validation", contract.PlanPending},
		{"upper X", "- [X] Add validation", contract.PlanCompleted},
	} {
		t.Run(row.name, func(t *testing.T) {
			plan := ParseChecklist(row.input)
			if len(plan.Steps) != 1 {
				t.Fatalf("parsed %d steps: %+v", len(plan.Steps), plan.Steps)
			}
			if plan.Steps[0].Status != row.want {
				t.Errorf("status = %q, want %q", plan.Steps[0].Status, row.want)
			}
			if plan.Steps[0].Title != "Add validation" {
				t.Errorf("title = %q, want the marker stripped", plan.Steps[0].Title)
			}
		})
	}
	// Surrounding prose becomes the note; an item-less file yields nothing.
	mixed := ParseChecklist("# Tasks\n\nScope: only the loader.\n\n- [ ] Add validation\n")
	if len(mixed.Steps) != 1 || !strings.Contains(mixed.Note, "Scope: only the loader.") {
		t.Fatalf("prose not captured as note: %+v", mixed)
	}
	if empty := ParseChecklist("just prose, no items\n"); len(empty.Steps) != 0 {
		t.Fatalf("item-less file parsed to %d steps", len(empty.Steps))
	}
}

// checklistEngine builds an engine over a real workspace so file writes hit
// disk exactly as they do in production.
func checklistEngine(t *testing.T) (*Engine, string, *[]contract.Plan) {
	t.Helper()
	dir := t.TempDir()
	settings := engineSettings()
	var pushed []contract.Plan
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "checklist", WorkspacePath: dir},
		Provider: &scriptedProvider{}, Registry: NewRegistry(service.Tools()...),
		Callbacks: contract.Callbacks{PlanUpdate: func(p contract.Plan) { pushed = append(pushed, p) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, dir, &pushed
}

func checklistScope(engine *Engine) dispatchScope {
	allowed := map[string]bool{}
	for _, name := range engine.registry.Names() {
		allowed[name] = true
	}
	return dispatchScope{
		counters: newCallCounters(),
		dispatch: func(ctx context.Context, call contract.ToolCall) (string, error) {
			return engine.registry.Execute(ctx, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), allowed)
		},
	}
}

// TestChecklistWriteFeedsThePanel proves the end-to-end feed: a tool call that
// writes tasks.md re-parses the file and pushes it to the UI callback.
func TestChecklistWriteFeedsThePanel(t *testing.T) {
	engine, dir, pushed := checklistEngine(t)
	scope := checklistScope(engine)
	definitions := engine.registry.Definitions(map[string]bool{"write_file": true})
	body := "- [x] Read the loader\n- [ ] Add validation\n"
	args, _ := json.Marshal(map[string]any{"path": "tasks.md", "content": body})
	outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("c1", "write_file", string(args)), definitions, Profile(contract.EffortLow), scope)
	if outcome.Failed {
		t.Fatalf("write failed: %s", outcome.Output)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.md")); err != nil {
		t.Fatalf("tasks.md not written: %v", err)
	}
	if len(*pushed) == 0 {
		t.Fatal("writing tasks.md did not push a checklist update")
	}
	latest := (*pushed)[len(*pushed)-1]
	if len(latest.Steps) != 2 || latest.Steps[1].Status != contract.PlanPending {
		t.Fatalf("pushed checklist = %+v", latest.Steps)
	}
	if got := engine.CurrentChecklist(); len(got.Steps) != 2 {
		t.Fatalf("CurrentChecklist = %+v", got.Steps)
	}
}

// TestChecklistFeedIgnoresUnrelatedWrites keeps the feed cheap: only writes to
// the checklist itself re-read the file.
func TestChecklistFeedIgnoresUnrelatedWrites(t *testing.T) {
	engine, _, pushed := checklistEngine(t)
	scope := checklistScope(engine)
	definitions := engine.registry.Definitions(map[string]bool{"write_file": true})
	args, _ := json.Marshal(map[string]any{"path": "main.go", "content": "package main\n"})
	if outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("c1", "write_file", string(args)), definitions, Profile(contract.EffortLow), scope); outcome.Failed {
		t.Fatalf("write failed: %s", outcome.Output)
	}
	if len(*pushed) != 0 {
		t.Fatalf("an unrelated write pushed %d checklist updates", len(*pushed))
	}
}

// TestChecklistFeedSeesMultiFilePatch is the trap the display-oriented target
// extractor would fall into: one apply_patch that edits code AND checks off a
// checklist item must still refresh the panel.
func TestChecklistFeedSeesMultiFilePatch(t *testing.T) {
	paths := contract.ToolTargetPaths("apply_patch", []byte(`{"patch":"--- a/main.go\n+++ b/main.go\n@@\n-old\n+new\n--- a/tasks.md\n+++ b/tasks.md\n@@\n-- [ ] Add validation\n+- [x] Add validation\n"}`))
	if len(paths) != 2 || paths[1] != "tasks.md" {
		t.Fatalf("multi-file patch paths = %v, want main.go and tasks.md", paths)
	}
	engine, _, _ := checklistEngine(t)
	if !engine.touchesChecklist(contract.NewToolCall("p", "apply_patch", `{"patch":"--- a/main.go\n+++ b/main.go\n@@\n-old\n+new\n--- a/tasks.md\n+++ b/tasks.md\n@@\n-x\n+y\n"}`)) {
		t.Fatal("a patch touching tasks.md alongside code was not recognized")
	}
}

// TestCompletionDisclosesOpenChecklistItems rebuilds the honesty guard against
// tasks.md: an answer that would imply completion while items are open says so.
func TestCompletionDisclosesOpenChecklistItems(t *testing.T) {
	engine, dir, _ := checklistEngine(t)
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [x] Read the loader\n- [ ] Add validation\n- [ ] Wire the tests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.refreshChecklist()
	got := engine.appendCompletionDisclosure("All set.")
	if !strings.Contains(got, "2 of 3 to-dos incomplete") {
		t.Fatalf("no disclosure appended: %q", got)
	}
	if !strings.Contains(got, "Add validation") {
		t.Fatalf("disclosure does not name the open items: %q", got)
	}
}

func TestCompletionSilentWhenChecklistComplete(t *testing.T) {
	engine, dir, _ := checklistEngine(t)
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [x] Read the loader\n- [x] Add validation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.refreshChecklist()
	if got := engine.appendCompletionDisclosure("All set."); got != "All set." {
		t.Fatalf("disclosure fired on a complete checklist: %q", got)
	}
	// No checklist at all is the common case and must stay silent too.
	bare, _, _ := checklistEngine(t)
	if got := bare.appendCompletionDisclosure("Answered."); got != "Answered." {
		t.Fatalf("disclosure fired without a checklist: %q", got)
	}
}

// TestChecklistSeededOnSessionOpen proves a resumed session shows work already
// in flight without waiting for the first write.
func TestChecklistSeededOnSessionOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [ ] Finish the migration\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "resume", WorkspacePath: dir},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.CurrentChecklist(); len(got.Steps) != 1 || got.Steps[0].Title != "Finish the migration" {
		t.Fatalf("checklist not seeded on open: %+v", got.Steps)
	}
}
