package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestBenchmarkManifestValidationRequiresReproducibilityPins(t *testing.T) {
	manifest := benchmarkManifest{
		Version: 1, Revision: "working-tree", EnvironmentImage: "sha256:test",
		ModelID: "model", Provider: "test", Seed: 7, Sandbox: "workspace-write",
		Permission: "normal",
		Cases: []benchmarkCase{{
			Name: "smoke", Fixture: t.TempDir(), Prompt: "fix",
			TimeoutSeconds: 30, GraderCommand: "go test ./...",
		}},
	}
	if err := validateBenchmarkManifest(manifest); err != nil {
		t.Fatalf("valid pinned manifest rejected: %v", err)
	}
	manifest.EnvironmentImage = ""
	if err := validateBenchmarkManifest(manifest); err == nil {
		t.Fatal("missing environment image pin was accepted")
	}
}

func TestParseBenchmarkEventsUsesTypedTerminal(t *testing.T) {
	event := headlessEvent{
		Version: 1, Type: "task_terminal",
		Stats: &contract.TaskStats{Status: contract.TaskStatusIncomplete, Turns: 3, ToolCalls: 2},
		Error: "budget exceeded",
	}
	payload, _ := json.Marshal(event)
	events, stats, terminalError := parseBenchmarkEvents(append(payload, '\n'))
	if len(events) != 1 || stats == nil || stats.Status != contract.TaskStatusIncomplete || stats.Turns != 3 || terminalError != "budget exceeded" {
		t.Fatalf("terminal parse mismatch: events=%d stats=%+v error=%q", len(events), stats, terminalError)
	}
}

func TestCopyBenchmarkWorkspaceRejectsSymlink(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	target := filepath.Join(source, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(source, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := copyBenchmarkWorkspace(source, destination); err == nil {
		t.Fatal("symlinked benchmark fixture was accepted")
	}
}
