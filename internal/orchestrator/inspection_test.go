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

// A-2: apply_patch must invalidate only the files its diff touches, not wipe the
// whole ledger (which superseded reads of untouched files and forced needless
// re-reads).
func TestApplyPatchInvalidatesOnlyItsTargets(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ledger := NewInspection(InspectionSnapshot{Version: 3}, nil, root)
	a := contract.NewToolCall("a1", "read_file", `{"path":"a.go","limit":3}`)
	b := contract.NewToolCall("b1", "read_file", `{"path":"b.go","limit":3}`)
	ledger.Record(a, "Read a.go (lines 1-3 of 3).")
	ledger.Record(b, "Read b.go (lines 1-3 of 3).")
	intact := func(string) bool { return true }

	patchCall := contract.NewToolCall("p1", "apply_patch", `{"patch":"--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-one\n+ONE\n"}`)
	dropped := ledger.InvalidateFor(patchCall)
	if len(dropped) != 1 || dropped[0] != "a1" {
		t.Fatalf("apply_patch dropped ids = %v, want [a1]", dropped)
	}
	if _, duplicate := ledger.Duplicate(a, intact); duplicate {
		t.Fatal("patched file remained deduplicated")
	}
	if _, duplicate := ledger.Duplicate(b, intact); !duplicate {
		t.Fatal("patching a.go invalidated b.go's read — the whole-ledger wipe was not fixed")
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

// The read-only shell gate is permissive-by-default (a denylist): it allows any
// inspection/build/test command in any shell syntax (cd anywhere, &&, ||, ;, &,
// pipes, substitution) and refuses ONLY genuinely destructive operations — a
// file-mutating command word, an in-place/writing flag, or a redirect that writes
// a file. This keeps a read-only agent from ever seeing a false "blocked" on a
// harmless command while still stopping an rm/overwrite.
func TestIsReadOnlyShellAllowsChainedReadOnlyCommands(t *testing.T) {
	allowed := []string{
		`cd "F:\x" && grep -rn "aria-" --include=*.tsx src/ | head -30`,
		`cd F:\proj && grep -rn pat src/`,
		`grep -c hex src/App.tsx`,
		`git log --oneline -10 2>/dev/null || echo none`,
		`git status --short`,
		`git grep -n TODO -- internal/`,
		`git ls-files internal | wc -l`,
		`go test ./internal/orchestrator`,
		`go vet ./... && go test ./... 2>/dev/null`,
		`go env GOOS; go version`,
		`npm test`,
		`npx tsc --noEmit`,
		`rg "IsReadOnlyShell" internal | sort | uniq -c`,
		`find . -name "*.go" | head -5`,
		`cat internal/orchestrator/engine.go | tail -20`,
		`ls -la internal/orchestrator`,
		`grep -rn "rm -rf" docs/`, // "rm -rf" is quoted data, not a command
		// Windows stderr silencer, merge-stderr, and the broadened build/test/inspect
		// tools a research subagent runs on a real project.
		`cd F:\Programs\Warp && dir /s /b 2>nul | head -50`,
		`dir /s /b *.go 2>nul && dir /s /b go.mod 2>nul`,
		`npm run build 2>&1 | head -40`,
		`npx eslint . 2>nul`,
		`go build ./...`,
		`python -m pytest -q`,
		`cargo check 2>&1 | tail`,
		`pnpm test && yarn lint`,
		// Exact live failures from the screenshots: `cd <path> ;` (semicolon, not
		// &&), Windows `&` command separator, `go env`/`go version`, and the
		// PowerShell null device `2>$null`. The old allowlist parser blocked them;
		// the denylist allows every one.
		`cd "F:\Programs\Warp" ; go env GOROOT GOPATH GOMOD`,
		`dir "F:\Programs\Warp\cmd" 2>nul & dir "F:\Programs\Warp\taskflow" 2>nul & where go`,
		`cd "F:\Programs\Warp" ; go version`,
		`dir . /s /b 2>$null`,
		`Get-ChildItem -Recurse 2>$null | Select-Object -First 50`,
		`go build ./... 2>$null`,
		// Harmless commands the old allowlist walled off but which mutate nothing:
		// a bare cd, an input (read) redirect, and a read-only command substitution.
		`cd src`,
		`sort < input.txt`,
		"cat `ls`",
		`git fetch --dry-run`, // fetch does not touch the working tree
		// D1 fix (T031): a destructive word as an ARGUMENT is harmless — only
		// command-position words are commands.
		`grep format main.go`,
		`rg kill internal/ | head -5`,
		`echo copy`,
		`git log --grep "rm -rf"`,
		`findstr /s format *.go`,
		`cat kill.txt; grep mv notes.md`,
	}
	for _, command := range allowed {
		if !IsReadOnlyShell(command) {
			t.Errorf("read-only command was walled off: %q", command)
		}
	}
}

func TestIsReadOnlyShellBlocksWritesRedirectsAndSubstitution(t *testing.T) {
	blocked := []string{
		``,
		`cd x && rm -rf src`,
		`grep a > out.txt`,
		`cat a >> b`,
		`Remove-Item important.go`,
		`go test ./...; Remove-Item important.go`,
		`git checkout -- .`,
		`git add -A && git commit -m x`,
		`git commit -m "wip"`,
		`git reset --hard`,
		`git log $(rm -rf x)`, // destructive word inside a substitution is still caught
		"echo `rm -rf x`",     // destructive word inside backticks is still caught
		`find . -name "*.go" -delete`,
		`find . -name "*.go" -exec rm {} +`,
		`cat a | tee b`,
		`echo hi | sed -i s/a/b/ file.go`,
		`grep pat src & rm x`,
		`git diff --output=evil.txt`,
		`npx tsc --noEmit --fix`, // --fix is a mutating flag
		`cp secrets.txt backup.txt`,
		`mkdir newdir && cd newdir`,
		`dd if=/dev/zero of=disk.img`,
		// D1 fix (T031): the SAME words at command position are still blocked.
		`format c:`,
		`kill -9 123`,
		`git status; rm x`,
		`dir | rm x`,
	}
	for _, command := range blocked {
		if IsReadOnlyShell(command) {
			t.Errorf("mutating or unsafe command escaped the gate: %q", command)
		}
	}
}
