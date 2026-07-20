package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const modulePath = "github.com/muhiya/muhiyacode"

// allowedInternalImports encodes the feature-010 layering contract (MS-5):
// contract (foundation) → gateway/workspace/state (services) → orchestrator
// (orchestration) → tui/command (interface). `command` is the composition root
// and may import any internal package ("*"). `mcpclient → state` is the one
// documented allowed lateral edge; `state → gateway` was removed by T005 and is
// deliberately NOT listed, so its reintroduction fails this test.
//
// `instructions` (US3, contracts/instruction-system.md IS-1) is a second
// foundation-layer package alongside `contract`: it imports only `contract`,
// and gateway/workspace/orchestrator (its importers) sit at or above the
// service layer, so adding it to their allowed sets cannot introduce a cycle
// — `instructions` never imports any of them back.
//
// `buildinfo` is a third foundation leaf (it imports NOTHING — just linker-
// injected vars), so tui may import it for the crash report's build identity
// (Stability Overhaul T052) with no cycle risk.
var allowedInternalImports = map[string][]string{
	"arch":      {},
	"buildinfo": {},
	// updatecheck is a fourth foundation leaf: stdlib only (net/http, os, json),
	// no internal imports, so tui may render its verdict with no cycle risk.
	"updatecheck":  {},
	"contract":     {},
	"instructions": {"contract"},
	"gateway":      {"contract", "instructions"},
	"workspace":    {"contract", "instructions"},
	"state":        {"contract"},
	"mcpclient":    {"contract", "state"},
	"orchestrator": {"contract", "gateway", "instructions"},
	"tui":          {"contract", "gateway", "orchestrator", "buildinfo", "updatecheck"},
	"command":      {"*"},
}

func TestInternalLayeringHasNoForbiddenEdges(t *testing.T) {
	root := moduleRoot(t)
	edges := internalImportEdges(t, filepath.Join(root, "internal"))
	for pkg, imports := range edges {
		allowed, known := allowedInternalImports[pkg]
		if !known {
			t.Errorf("internal package %q has no layering entry — add it to allowedInternalImports with its permitted edges", pkg)
			continue
		}
		if len(allowed) == 1 && allowed[0] == "*" {
			continue
		}
		allow := map[string]bool{}
		for _, a := range allowed {
			allow[a] = true
		}
		for imp := range imports {
			if !allow[imp] {
				t.Errorf("layering violation: internal/%s imports internal/%s, not in its allowed set %v", pkg, imp, allowed)
			}
		}
	}
	assertNoCycle(t, edges)
}

// internalImportEdges maps each internal package name to the set of internal
// package names it imports (non-test files only), via stdlib import parsing.
func internalImportEdges(t *testing.T, internalDir string) map[string]map[string]bool {
	t.Helper()
	edges := map[string]map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		pkg := filepath.Base(filepath.Dir(path))
		if _, ok := edges[pkg]; !ok {
			edges[pkg] = map[string]bool{}
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			p := strings.Trim(spec.Path.Value, `"`)
			if rest, ok := strings.CutPrefix(p, modulePath+"/internal/"); ok {
				dep := rest
				if slash := strings.IndexByte(dep, '/'); slash >= 0 {
					dep = dep[:slash]
				}
				if dep != pkg {
					edges[pkg][dep] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	return edges
}

// assertNoCycle fails if the internal-package import graph contains a cycle
// (defense in depth: the allow-map already forbids back-edges, but an explicit
// check documents the acyclic invariant for SC-004).
func assertNoCycle(t *testing.T, edges map[string]map[string]bool) {
	t.Helper()
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var visit func(string) bool
	visit = func(pkg string) bool {
		color[pkg] = gray
		stack = append(stack, pkg)
		deps := make([]string, 0, len(edges[pkg]))
		for d := range edges[pkg] {
			deps = append(deps, d)
		}
		sort.Strings(deps)
		for _, dep := range deps {
			switch color[dep] {
			case gray:
				t.Errorf("import cycle detected: %s -> %s", strings.Join(stack, " -> "), dep)
				return true
			case white:
				if visit(dep) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[pkg] = black
		return false
	}
	pkgs := make([]string, 0, len(edges))
	for p := range edges {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		if color[p] == white {
			if visit(p) {
				return
			}
		}
	}
}
