package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TA01: a tasks.md written beside the work drives the to-do panel. The write side
// always allowed it (the role-gate carve-out matches the basename anywhere); the
// reader was pinned to the workspace root, so such a checklist was accepted and
// then never read — an empty panel and a blind completion guard.

func TestChecklistInTargetDirectoryDrivesThePanel(t *testing.T) {
	var published []contract.Plan
	engine, _, dir := fieldTestEngine(t)
	engine.callbacks.PlanUpdate = func(plan contract.Plan) { published = append(published, plan) }

	target := filepath.Join(dir, "games", "snake")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "tasks.md"), []byte("- [ ] build the game\n- [x] scaffold the folder\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The engine learns the location from the write that just landed.
	call := contract.NewToolCall("w1", "write_file", `{"path":"games/snake/tasks.md","content":"..."}`)
	engine.adoptChecklistPath(call)
	engine.refreshChecklist()

	if len(published) == 0 {
		t.Fatal("a checklist in the target directory never reached the panel")
	}
	plan := published[len(published)-1]
	if len(plan.Steps) != 2 {
		t.Fatalf("expected the two items to parse, got %d: %+v", len(plan.Steps), plan.Steps)
	}
	// The completion guard reads the same active path, so open items are visible.
	if open := engine.openChecklistItems(); len(open) != 1 {
		t.Fatalf("expected exactly one open item from the target-directory checklist, got %v", open)
	}
}

// The workspace root stays the default until something is written elsewhere, so
// existing sessions behave exactly as before.
func TestChecklistDefaultsToWorkspaceRoot(t *testing.T) {
	engine, _, dir := fieldTestEngine(t)
	if got, want := engine.checklistPath(), filepath.Join(dir, "tasks.md"); got != want {
		t.Fatalf("default checklist path = %q, want %q", got, want)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [ ] root item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.refreshChecklist()
	if open := engine.openChecklistItems(); len(open) != 1 {
		t.Fatalf("root checklist should still drive the guard, got %v", open)
	}
}

// A later write switches the active checklist, so moving the file mid-session is
// tracked rather than leaving the panel pointed at a stale path.
func TestLatestChecklistWriteWins(t *testing.T) {
	engine, _, dir := fieldTestEngine(t)
	sub := filepath.Join(dir, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "tasks.md"), []byte("- [ ] one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [ ] a\n- [ ] b\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine.adoptChecklistPath(contract.NewToolCall("w1", "write_file", `{"path":"svc/tasks.md","content":"..."}`))
	engine.refreshChecklist()
	if open := engine.openChecklistItems(); len(open) != 1 {
		t.Fatalf("expected the svc checklist, got %v", open)
	}

	engine.adoptChecklistPath(contract.NewToolCall("w2", "write_file", `{"path":"tasks.md","content":"..."}`))
	engine.refreshChecklist()
	if open := engine.openChecklistItems(); len(open) != 2 {
		t.Fatalf("expected the root checklist after the later write, got %v", open)
	}
}

// An absolute path (the model may emit one) resolves without being re-joined to
// the workspace root.
func TestChecklistAdoptsAbsolutePath(t *testing.T) {
	engine, _, dir := fieldTestEngine(t)
	nested := filepath.Join(dir, "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(nested, "tasks.md")
	if err := os.WriteFile(absolute, []byte("- [ ] deep item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments, _ := json.Marshal(map[string]any{"path": absolute, "content": "..."})
	engine.adoptChecklistPath(contract.NewToolCall("w1", "write_file", string(arguments)))
	if got := engine.checklistPath(); got != filepath.Clean(absolute) {
		t.Fatalf("absolute checklist path = %q, want %q", got, absolute)
	}
}
