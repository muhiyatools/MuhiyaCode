package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolyglotParsing(t *testing.T) {
	dir := t.TempDir()

	tsFile := filepath.Join(dir, "sample.ts")
	tsContent := `
export interface User {
	id: string;
	name: string;
}

export function getUser(id: string): User {
	return { id, name: "Alice" };
}

export class UserService {
	async fetchUser() {}
}
`
	if err := os.WriteFile(tsFile, []byte(tsContent), 0644); err != nil {
		t.Fatalf("write ts file: %v", err)
	}

	outline, err := ParsePolyglotOutline(tsFile)
	if err != nil {
		t.Fatalf("ParsePolyglotOutline failed: %v", err)
	}

	if outline.Package != "typescript" {
		t.Errorf("expected package typescript, got %s", outline.Package)
	}

	if len(outline.Symbols) < 3 {
		t.Errorf("expected at least 3 symbols, got %d", len(outline.Symbols))
	}

	pyFile := filepath.Join(dir, "sample.py")
	pyContent := `
class DataProcessor:
    def process(self):
        pass

def main():
    print("hello")
`
	if err := os.WriteFile(pyFile, []byte(pyContent), 0644); err != nil {
		t.Fatalf("write py file: %v", err)
	}

	pyOutline, err := ParsePolyglotOutline(pyFile)
	if err != nil {
		t.Fatalf("ParsePolyglotOutline py failed: %v", err)
	}

	if pyOutline.Package != "python" {
		t.Errorf("expected package python, got %s", pyOutline.Package)
	}

	if len(pyOutline.Symbols) < 2 {
		t.Errorf("expected at least 2 symbols for py, got %d", len(pyOutline.Symbols))
	}
}

func TestPolyglotReferencesAreExplicitlyLexical(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	source := "export function loadUser() {}\nconst value = loadUser();\n"
	if err := os.WriteFile(filepath.Join(root, "user.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := ws.execInspectCode(context.Background(), json.RawMessage(
		`{"mode":"references","path":"user.ts","symbol":"loadUser"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "lexical identifier match") || !strings.Contains(output, "user.ts:2") {
		t.Fatalf("polyglot reference capability/result was not explicit:\n%s", output)
	}
}
