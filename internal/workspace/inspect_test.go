package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func inspectWorkspace(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package pkg\n\nfunc Alpha() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "b.go"), []byte("package pkg\n\nfunc Beta() { Alpha() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, Options{PermissionMode: contract.PermissionAutoAccept, Trust: NewMemoryTrustStore()})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestInspectWorkspaceMapSearchReadManyAndSymbol(t *testing.T) {
	w := inspectWorkspace(t)
	tool := w.inspectTool()
	cases := []struct{ name, body string }{
		{"map", `{"mode":"map","path":"pkg","limit":10}`},
		{"search", `{"mode":"search","query":"Alpha","path":"pkg","limit":10}`},
		{"read_many", `{"mode":"read_many","files":[{"path":"pkg/a.go","offset":1,"limit":3},{"path":"pkg/b.go","offset":2,"limit":2}]}`},
		{"symbol", `{"mode":"symbol","query":"Alpha"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output, err := tool.Execute(context.Background(), json.RawMessage(test.body))
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if json.Unmarshal([]byte(output), &value) != nil || value["mode"] != test.name {
				t.Fatalf("output=%s", output)
			}
		})
	}
}

func TestInspectWorkspaceBoundsAndReportsSkippedInputs(t *testing.T) {
	w := inspectWorkspace(t)
	files := make([]map[string]any, 0, 14)
	for i := 0; i < 13; i++ {
		files = append(files, map[string]any{"path": "missing.go", "limit": 200})
	}
	body, _ := json.Marshal(map[string]any{"mode": "read_many", "files": files})
	output, err := w.inspectTool().Execute(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Skipped []map[string]string `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Skipped) != maxInspectFiles {
		t.Fatalf("skipped=%d want=%d", len(value.Skipped), maxInspectFiles)
	}
}
