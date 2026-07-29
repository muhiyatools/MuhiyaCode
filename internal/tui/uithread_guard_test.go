package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBlockingEngineCallsOnUIThread is the standing guard for Stability
// Overhaul Phase 2. The engine's mutators emit user-facing callbacks that flow to
// bridge.send → program.Send, whose message channel is UNBUFFERED and drained
// only by the Bubble Tea event loop. Calling such a mutator from inside Update
// (directly, or in a modal callback — which also runs on the Update goroutine)
// deadlocks the whole terminal (the live "Proceed now" freeze). This test makes
// that class of regression impossible: it parses every *Model method (the
// Update-reachable surface) and flags any Engine method call that runs ON the
// Update goroutine unless the method is on the read-only/fast-in-memory allowlist.
//
// Off-thread discriminator: a call is safe when it is enclosed by a ZERO-parameter
// function literal — `func() tea.Msg { … }` (a tea.Cmd) or `func() (any,error)`
// (an actionCommand thunk), both of which Bubble Tea runs in a goroutine. A
// PARAMETERIZED literal (`func(index int) tea.Cmd { … }`, the modal-choice
// callback) runs on the Update goroutine, so a call inside it is NOT safe — which
// is exactly the shape the proceed-now freeze had.
//
// Every allowlist entry is an Engine method proven (T020 inventory) to take no
// lock that reaches program.Send and to do no persistence I/O: a lock-guarded
// read, or a fast in-memory mutation.
func TestNoBlockingEngineCallsOnUIThread(t *testing.T) {
	allowed := map[string]string{
		// Read-only reads (return a copy/snapshot; no callbacks, no I/O).
		"ContextReport":    "read-only snapshot",
		"CurrentChecklist": "read-only checklist copy",
		"Usage":            "read-only usage copy",
		"UsageAggregate":   "read-only aggregate copy",
		"HarnessEvents":    "read-only ring copy (T012)",
		"DiscoveredModels": "read-only model list copy",
		"CatalogModelName": "read-only model name lookup",
		// Fast in-memory mutations (no persistence I/O, no callback/Send).
		"QueueUserMessage":  "mu-guarded steering append",
		"SetEffort":         "liveSettingsMu-guarded field set",
		"SetPermissionMode": "liveSettingsMu-guarded field set",
		"Cancel":            "cancels the task context",
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !isModelMethod(fd) {
				continue
			}
			checked++
			safe := offThreadCalls(fd)
			ast.Inspect(fd, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				method, isEngine := engineMethodName(call)
				if !isEngine || safe[call] {
					return true
				}
				if _, ok := allowed[method]; ok {
					return true
				}
				pos := fset.Position(call.Pos())
				t.Errorf("%s:%d: Engine.%s runs on the Update goroutine. Wrap it in a `func() tea.Msg { … }` command (see the T021 pattern in slash.go), or, if it is a lock-guarded read with no persistence I/O and no callback emission, add it to the allowlist in this test with a justification.", filepath.Base(pos.Filename), pos.Line, method)
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("guard inspected zero *Model methods — the parser or file glob is broken")
	}
}

// isModelMethod reports whether fd is a method on *Model (the Update-reachable
// surface; package funcs like tui.Run's teardown are excluded).
func isModelMethod(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return false
	}
	star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Model"
}

// offThreadCalls returns the set of CallExprs within fd that are enclosed by a
// zero-parameter function literal (a tea.Cmd / actionCommand thunk that Bubble
// Tea runs off the Update goroutine).
func offThreadCalls(fd *ast.FuncDecl) map[*ast.CallExpr]bool {
	safe := map[*ast.CallExpr]bool{}
	ast.Inspect(fd, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit.Type.Params.NumFields() != 0 {
			return true
		}
		ast.Inspect(lit, func(m ast.Node) bool {
			if call, ok := m.(*ast.CallExpr); ok {
				safe[call] = true
			}
			return true
		})
		return true
	})
	return safe
}

// engineMethodName returns the method name and true when call is `<x>.Engine.<M>(…)`.
func engineMethodName(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "Engine" {
		return "", false
	}
	return sel.Sel.Name, true
}
