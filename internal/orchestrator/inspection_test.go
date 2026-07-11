package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestInspectionStitchesRangesPersistsAndReloads(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.go")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 300)), 0o644); err != nil {
		t.Fatal(err)
	}
	var saved InspectionSnapshot
	ledger := NewInspection(InspectionSnapshot{Version: 3}, func(snapshot InspectionSnapshot) error {
		saved = snapshot
		return nil
	}, root)
	first := contract.NewToolCall("r1", "read_file", `{"path":"large.go","offset":1,"limit":100}`)
	second := contract.NewToolCall("r2", "read_file", `{"path":"large.go","offset":101,"limit":100}`)
	ledger.Record(first, "Read large.go (lines 1-100 of 300).\n1 | line")
	ledger.Record(second, "Read large.go (lines 101-200 of 300).\n101 | line")
	intact := func(id string) bool { return id == "r1" || id == "r2" }
	covered := contract.NewToolCall("r3", "read_file", `{"path":"large.go","offset":50,"limit":151}`)
	if entry, duplicate := ledger.Duplicate(covered, intact); !duplicate || entry.CallID == "" {
		t.Fatalf("stitched range not detected: entry=%+v duplicate=%v snapshot=%+v", entry, duplicate, ledger.Snapshot())
	}
	missing := contract.NewToolCall("r4", "read_file", `{"path":"large.go","offset":150,"limit":101}`)
	if _, duplicate := ledger.Duplicate(missing, intact); duplicate {
		t.Fatal("uncovered range was refused")
	}
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"signatures"`) || !strings.Contains(string(encoded), `"coverage"`) || strings.Contains(string(encoded), `"entries"`) {
		t.Fatalf("snapshot is not TypeScript-v3 compatible: %s", encoded)
	}
	reloaded := NewInspection(saved, nil, root)
	if _, duplicate := reloaded.Duplicate(covered, intact); !duplicate {
		t.Fatal("coverage did not survive reload")
	}
}

func TestInspectionFreshnessAndInvalidation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ledger := NewInspection(InspectionSnapshot{Version: 3}, nil, root)
	a := contract.NewToolCall("a1", "read_file", `{"path":"a.go","limit":3}`)
	b := contract.NewToolCall("b1", "read_file", `{"path":"b.go","limit":3}`)
	search := contract.NewToolCall("s1", "grep", `{"pattern":"one","path":"."}`)
	ledger.Record(a, "Read a.go (lines 1-3 of 3).")
	ledger.Record(b, "Read b.go (lines 1-3 of 3).")
	ledger.Record(search, "Found 2 match(es).")
	intact := func(string) bool { return true }
	readOnly := contract.NewToolCall("sh1", "run_shell", `{"command":"git status --short"}`)
	if dropped := ledger.InvalidateFor(readOnly); len(dropped) != 0 {
		t.Fatalf("read-only shell invalidated coverage: %v", dropped)
	}
	if _, duplicate := ledger.Duplicate(a, intact); !duplicate {
		t.Fatal("read-only shell lost read evidence")
	}
	editA := contract.NewToolCall("e1", "edit_file", `{"path":"a.go","oldString":"one","newString":"ONE"}`)
	dropped := ledger.InvalidateFor(editA)
	if len(dropped) != 1 || dropped[0] != "a1" {
		t.Fatalf("per-file dropped ids = %v", dropped)
	}
	if _, duplicate := ledger.Duplicate(a, intact); duplicate {
		t.Fatal("edited file remained deduplicated")
	}
	if _, duplicate := ledger.Duplicate(b, intact); !duplicate {
		t.Fatal("editing a.go invalidated b.go")
	}
	if _, duplicate := ledger.Duplicate(search, intact); duplicate {
		t.Fatal("workspace search survived a mutation")
	}

	// An edit outside MuhiyaCode must invalidate a fingerprinted read.
	info, _ := os.Stat(filepath.Join(root, "b.go"))
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(filepath.Join(root, "b.go"), info.ModTime().Add(time.Second), info.ModTime().Add(time.Second))
	if _, duplicate := ledger.Duplicate(b, intact); duplicate {
		t.Fatal("externally changed file remained deduplicated")
	}
	if ledger.Known("b.go") {
		t.Fatal("externally changed file remained safe to overwrite without a fresh read")
	}
}

func TestInspectionLoadsTypeScriptV3Fixture(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	fixture := InspectionSnapshot{
		Version: 3,
		Signatures: map[string]InspectionEntry{
			"read_file fixture.go#o1#l3": {CallID: "legacy", Kind: "read", Path: "fixture.go"},
		},
		Coverage: map[string]InspectionCoverage{
			"fixture.go": {TotalLines: 3, Segments: []InspectionSegment{{Start: 1, End: 3, CallID: "legacy"}}, Fingerprint: &InspectionFingerprint{MTimeMS: float64(info.ModTime().UnixNano()) / 1e6, Size: info.Size()}},
		},
		InspectedFiles: []string{"fixture.go"},
	}
	ledger := NewInspection(fixture, nil, root)
	call := contract.NewToolCall("next", "read_file", `{"path":"fixture.go","offset":1,"limit":3}`)
	if _, duplicate := ledger.Duplicate(call, func(id string) bool { return id == "legacy" }); !duplicate {
		t.Fatalf("TypeScript fixture did not deduplicate: %+v", ledger.Snapshot())
	}
}
