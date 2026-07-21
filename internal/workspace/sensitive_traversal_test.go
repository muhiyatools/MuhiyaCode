package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// I-1: grep/list_files/glob authorize only the root they are pointed at, then
// walk its children. A protected credential root nested inside a non-sensitive
// search root (here .ssh inside the workspace) must be pruned from every walk,
// or a single approved search of an ancestor exfiltrates the credentials that a
// direct read of the same path would refuse.
func TestTraversalToolsSkipSensitiveRoots(t *testing.T) {
	root := t.TempDir()

	ssh := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_rsa"), []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "app.go"), []byte("// loads a PRIVATE KEY at boot\npackage app\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := New(root, Options{
		PermissionMode: contract.PermissionAutoAccept,
		Trust:          NewMemoryTrustStore(),
		Approver:       ApproverFunc(func(context.Context, ApprovalRequest) (bool, error) { return true, nil }),
		SensitiveRoots: []string{ssh},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	leaked := func(path string) bool { return strings.Contains(filepath.ToSlash(path), ".ssh") }

	res, err := ws.Grep(ctx, GrepOptions{Path: ".", Pattern: "PRIVATE KEY"})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	sawNormal := false
	for _, m := range res.Matches {
		if leaked(m.Path) {
			t.Errorf("grep read a file inside a sensitive root: %s", m.Path)
		}
		if strings.Contains(filepath.ToSlash(m.Path), "src/app.go") {
			sawNormal = true
		}
	}
	if !sawNormal {
		t.Error("grep did not return the normal file — the sensitive skip is over-broad")
	}

	list, err := ws.List(ctx, ListOptions{Path: ".", Recursive: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	sawSrc := false
	for _, e := range list.Entries {
		if leaked(e.Path) {
			t.Errorf("list enumerated a sensitive entry: %s", e.Path)
		}
		if e.Name == "app.go" {
			sawSrc = true
		}
	}
	if !sawSrc {
		t.Error("list did not enumerate the normal file — the sensitive skip is over-broad")
	}

	globs, err := ws.Glob(ctx, GlobOptions{Path: ".", Pattern: "**/*"})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, g := range globs {
		if leaked(g.Path) {
			t.Errorf("glob matched a path inside a sensitive root: %s", g.Path)
		}
	}
}
