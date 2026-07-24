package workspace

import (
	"context"
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func codeindexFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata", "codeindex")
}

func codeindexFixturePath(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(codeindexFixtureDir(t), name+".gosrc")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), name+".go")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func copyCodeindexFixtures(t *testing.T, root string) {
	t.Helper()
	srcDir := codeindexFixtureDir(t)
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gosrc") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(entry.Name(), ".gosrc") + ".go"
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newInspectWorkspace(t *testing.T) (*Workspace, context.Context) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	copyCodeindexFixtures(t, root)
	trust := NewMemoryTrustStore()
	if err := trust.Trust(ctx, root); err != nil {
		t.Fatal(err)
	}
	w, err := New(root, Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	return w, ctx
}

func inspectRaw(mode, path, symbol string, testFiles *bool) json.RawMessage {
	input := map[string]any{"mode": mode, "path": path}
	if symbol != "" {
		input["symbol"] = symbol
	}
	if testFiles != nil {
		input["testFiles"] = *testFiles
	}
	raw, _ := json.Marshal(input)
	return raw
}

func symbolByName(symbols []SymbolEntry, name string) (SymbolEntry, bool) {
	for _, sym := range symbols {
		if sym.Name == name {
			return sym, true
		}
	}
	return SymbolEntry{}, false
}

func TestOutline_BasicFile(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	outline, err := ParseFileOutline(fset, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outline.Package != "codeindex" {
		t.Errorf("expected package codeindex, got %s", outline.Package)
	}
	if len(outline.Imports) == 0 {
		t.Errorf("expected imports, got none")
	}

	sym, ok := symbolByName(outline.Symbols, "Config")
	if !ok || sym.Kind != SymbolStruct {
		t.Errorf("expected struct Config, got %+v", sym)
	}
	sym, ok = symbolByName(outline.Symbols, "Run")
	if !ok || sym.Kind != SymbolMethod {
		t.Errorf("expected method Run, got %+v", sym)
	}
}

func TestOutline_StructFields(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	outline, err := ParseFileOutline(fset, file)
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := symbolByName(outline.Symbols, "Config")
	if !ok {
		t.Fatal("Config struct not found")
	}
	if !strings.Contains(sym.Signature, "Name string") || !strings.Contains(sym.Signature, "Timeout int") {
		t.Errorf("expected struct fields in signature, got: %s", sym.Signature)
	}
}

func TestOutline_InterfaceMethods(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "interfaces")
	outline, err := ParseFileOutline(fset, file)
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := symbolByName(outline.Symbols, "Reader")
	if !ok || sym.Kind != SymbolInterface {
		t.Fatalf("expected interface Reader, got %+v", sym)
	}
	if !strings.Contains(sym.Signature, "Read") {
		t.Errorf("expected Read method in signature, got %s", sym.Signature)
	}
}

func TestOutline_MethodReceivers(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	outline, err := ParseFileOutline(fset, file)
	if err != nil {
		t.Fatal(err)
	}
	sym, ok := symbolByName(outline.Symbols, "Run")
	if !ok {
		t.Fatal("Run method not found")
	}
	if !strings.Contains(sym.Signature, "*Service") {
		t.Errorf("expected pointer receiver *Service in signature, got %s", sym.Signature)
	}
}

func TestOutline_SyntaxError(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "syntax_error")
	outline, _ := ParseFileOutline(fset, file)
	if outline == nil {
		t.Fatal("expected non-nil outline even on parse error")
	}
	if len(outline.Symbols) == 0 {
		t.Errorf("expected partial symbols before syntax error")
	}
	if len(outline.Errors) == 0 {
		t.Errorf("expected error message recorded")
	}
}

func TestOutline_NonGoFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test.txt")
	_ = os.WriteFile(tmp, []byte("hello"), 0o644)
	_, err := collectGoFiles(tmp, true)
	if err == nil {
		t.Errorf("expected error when running collectGoFiles on non-go file")
	}
}

func TestOutline_Directory(t *testing.T) {
	dir := t.TempDir()
	copyCodeindexFixtures(t, dir)
	files, err := collectGoFiles(dir, true)
	if err != nil || len(files) < 3 {
		t.Fatalf("expected at least 3 files, got %d (err: %v)", len(files), err)
	}
}

func TestOutline_SymbolCap(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	outline, err := ParseFileOutline(fset, file)
	if err != nil {
		t.Fatal(err)
	}
	if outline.Truncated {
		t.Errorf("sample.go should not be truncated")
	}
}

func TestDefinition_ExactMatch(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	defs, err := FindDefinitions(fset, []string{file}, "Config")
	if err != nil || len(defs) != 1 {
		t.Fatalf("expected 1 definition of Config, got %d (err: %v)", len(defs), err)
	}
	if defs[0].Kind != SymbolStruct {
		t.Errorf("expected SymbolStruct, got %s", defs[0].Kind)
	}
}

func TestDefinition_MethodMatch(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	defs, err := FindDefinitions(fset, []string{file}, "Run")
	if err != nil || len(defs) != 1 {
		t.Fatalf("expected 1 definition of Run, got %d (err: %v)", len(defs), err)
	}
	if defs[0].Kind != SymbolMethod {
		t.Errorf("expected SymbolMethod, got %s", defs[0].Kind)
	}
}

func TestDefinition_NotFound(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	defs, err := FindDefinitions(fset, []string{file}, "NonExistentSymbol")
	if err != nil || len(defs) != 0 {
		t.Errorf("expected 0 definitions, got %d", len(defs))
	}
}

func TestReferences_IdentifiersOnly(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	refs, truncated, err := FindReferences(fset, []string{file}, "Config", 50)
	if err != nil || len(refs) == 0 {
		t.Fatalf("expected references to Config, got %d (err: %v)", len(refs), err)
	}
	if truncated {
		t.Errorf("did not expect truncation")
	}
}

func TestReferences_Cap(t *testing.T) {
	fset := token.NewFileSet()
	file := codeindexFixturePath(t, "sample")
	refs, truncated, err := FindReferences(fset, []string{file}, "Config", 1)
	if err != nil || len(refs) != 1 || !truncated {
		t.Errorf("expected max 1 ref with truncated=true, got len=%d truncated=%v", len(refs), truncated)
	}
}

func TestExecInspectCode_OutlineMode(t *testing.T) {
	w, ctx := newInspectWorkspace(t)
	out, err := w.execInspectCode(ctx, inspectRaw("outline", "sample.go", "", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Outline of sample.go") || !strings.Contains(out, "type Config struct") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestExecInspectCode_DefinitionMode(t *testing.T) {
	w, ctx := newInspectWorkspace(t)
	out, err := w.execInspectCode(ctx, inspectRaw("definition", "sample.go", "Config", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Definitions of \"Config\"") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestExecInspectCode_ReferencesMode(t *testing.T) {
	w, ctx := newInspectWorkspace(t)
	out, err := w.execInspectCode(ctx, inspectRaw("references", "sample.go", "Config", nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "References to \"Config\"") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestExecInspectCode_SecurityGuard(t *testing.T) {
	w, ctx := newInspectWorkspace(t)
	_, err := w.execInspectCode(ctx, inspectRaw("outline", "../outside.go", "", nil))
	if err == nil {
		t.Errorf("expected error for path outside workspace root")
	}
}

func TestExecInspectCode_InvalidMode(t *testing.T) {
	w, ctx := newInspectWorkspace(t)
	_, err := w.execInspectCode(ctx, inspectRaw("badmode", "sample.go", "", nil))
	if err == nil {
		t.Errorf("expected error for invalid mode")
	}
}

func TestExecInspectCode_MissingSymbol(t *testing.T) {
	w, ctx := newInspectWorkspace(t)
	_, err := w.execInspectCode(ctx, inspectRaw("definition", "sample.go", "", nil))
	if err == nil {
		t.Errorf("expected error for missing symbol in definition mode")
	}
}

func TestInspectCodePythonError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "test.py")
	os.WriteFile(path, []byte(""), 0o644)
	_, err := collectGoFiles(path, false)
	if err == nil || !strings.Contains(err.Error(), "grep_search") {
		t.Errorf("expected grep_search guidance in error, got: %v", err)
	}
}

