package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestWorkspaceHasCodeToResearch pins the greenfield probe: an empty or
// scaffold-only folder reads as greenfield (no fan-out), a real codebase does
// not, and vendored/hidden directories never tip a greenfield folder over.
func TestWorkspaceHasCodeToResearch(t *testing.T) {
	newEngine := func(root string) *Engine {
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "probe", WorkspacePath: root}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
		if err != nil {
			t.Fatal(err)
		}
		return engine
	}
	write := func(root, rel string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	empty := t.TempDir()
	if newEngine(empty).workspaceHasCodeToResearch() {
		t.Fatal("empty folder must read as greenfield")
	}

	scaffold := t.TempDir()
	write(scaffold, "go.mod")
	write(scaffold, "README.md")
	write(scaffold, ".gitignore")
	if newEngine(scaffold).workspaceHasCodeToResearch() {
		t.Fatal("go.mod + README only must still read as greenfield (no source files)")
	}

	vendored := t.TempDir()
	write(vendored, "main.go")
	for i := 0; i < 20; i++ { // lots of vendored JS that must NOT count
		write(vendored, filepath.Join("node_modules", "pkg", "index"+string(rune('a'+i))+".js"))
	}
	if newEngine(vendored).workspaceHasCodeToResearch() {
		t.Fatal("one real source file plus vendored code must stay greenfield (vendor excluded)")
	}

	real := t.TempDir()
	write(real, "main.go")
	write(real, filepath.Join("internal", "store", "store.go"))
	write(real, filepath.Join("internal", "cli", "cli.go"))
	if !newEngine(real).workspaceHasCodeToResearch() {
		t.Fatal("a folder with several source files must be researchable")
	}
}

// TestDynamicResearchLensCountScalesWithCodebase pins the graduated fan-out: a
// small existing project gets one generic research lens, a medium one two, a
// large one three — instead of a fixed three near-identical full-repo reads.
func TestDynamicResearchLensCountScalesWithCodebase(t *testing.T) {
	newEngineWith := func(sourceFiles int) *Engine {
		dir := t.TempDir()
		for i := 0; i < sourceFiles; i++ {
			if err := os.WriteFile(filepath.Join(dir, "f"+strconv.Itoa(i)+".go"), []byte("package x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		settings := engineSettings()
		engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "lens", WorkspacePath: dir}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
		if err != nil {
			t.Fatal(err)
		}
		return engine
	}
	for _, tc := range []struct{ files, want int }{
		{4, 1},  // small
		{12, 2}, // medium
		{25, 3}, // large
	} {
		if got := newEngineWith(tc.files).dynamicResearchLensCount(); got != tc.want {
			t.Fatalf("%d source files ⇒ %d lenses, want %d", tc.files, got, tc.want)
		}
	}

	// The generic fallback must report itself as non-explicit so the caller scales
	// it; named directories must report explicit so they are NOT collapsed.
	if _, explicit := pipelineResearchScopes("Improve the taskflow CLI overall."); explicit {
		t.Fatal("a prompt with no named directories must use the non-explicit generic lenses")
	}
}

// TestPipelineStepsAreDependent pins the implementation-phase collapse rule:
// explicit ordering markers or a shared file make steps dependent (→ one coherent
// agent); independent distinct-file steps stay parallelizable (→ per-step fan-out,
// preserving the FI-11 partial-success path).
func TestPipelineStepsAreDependent(t *testing.T) {
	step := func(title string) contract.PlanStep {
		return contract.PlanStep{Title: title, Status: contract.PlanPending}
	}
	cases := []struct {
		name  string
		steps []contract.PlanStep
		want  bool
	}{
		{"independent prose (FI-11 shape)", []contract.PlanStep{step("Fix the login token validator"), step("Update the dashboard error banner")}, false},
		{"distinct files", []contract.PlanStep{step("cmd/a/main.go: create entry"), step("internal/store/store.go: add storage")}, false},
		{"explicit serial marker", []contract.PlanStep{step("[serial] internal/x.go: step one"), step("internal/y.go: step two")}, true},
		{"depends-on phrasing", []contract.PlanStep{step("Wire the handler"), step("Add the route; depends on the handler")}, true},
		{"after phrasing", []contract.PlanStep{step("Create the schema"), step("Seed data after the schema exists")}, true},
		{"shared file target", []contract.PlanStep{step("internal/store/store.go: add write path"), step("internal/store/store.go: add read path")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pipelineStepsAreDependent(contract.Plan{Steps: tc.steps}); got != tc.want {
				t.Fatalf("pipelineStepsAreDependent = %v, want %v", got, tc.want)
			}
		})
	}

	// mergePipelineGroups folds every group's steps into one non-independent group.
	merged := mergePipelineGroups([]pipelineStepGroup{{Indexes: []int{0, 2}}, {Indexes: []int{1}}})
	if merged.Independent || len(merged.Indexes) != 3 {
		t.Fatalf("merge = %+v, want 3 indexes and Independent=false", merged)
	}
}

// TestGreenfieldResearchSkipsFanout locks in the dynamic fan-out: research on an
// empty workspace dispatches ZERO explore subagents, records the greenfield
// degradation, and advances straight to planning — instead of the old fixed
// 2–3-agent fan-out mapping a directory that has no code.
func TestGreenfieldResearchSkipsFanout(t *testing.T) {
	settings := engineSettings()
	agentStarts := 0
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "greenfield", WorkspacePath: t.TempDir()}, // empty
		Provider: &scriptedProvider{},
		Registry: NewRegistry(),
		Callbacks: contract.Callbacks{Agent: func(event contract.AgentEvent) {
			if event.Kind == "start" {
				agentStarts++
			}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleResearch, Depth: PipelineDepthFull}
	budget := BudgetFor(Assessment{Class: ClassLarge}, Profile(contract.EffortMedium))

	out, err := engine.runPipelineResearch(context.Background(), "Build a brand-new CLI called taskflow from scratch.", budget, Profile(contract.EffortMedium))
	if err != nil {
		t.Fatalf("greenfield research must not error: %v", err)
	}
	if agentStarts != 0 {
		t.Fatalf("greenfield research must dispatch zero explore agents, dispatched %d", agentStarts)
	}
	if engine.LifecycleState() != contract.LifecyclePlanning {
		t.Fatalf("greenfield research must advance to planning, state = %s", engine.LifecycleState())
	}
	if !engine.Lifecycle().IsDegraded(contract.LifecycleResearch) {
		t.Fatal("greenfield skip must record a research degradation for the session record")
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("greenfield status line should say research was skipped, got: %q", out)
	}
}
