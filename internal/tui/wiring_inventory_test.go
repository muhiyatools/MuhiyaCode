package tui

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// Feature 010 US5 (T040, WI-1/WI-2/WI-3): this is the bidirectional guard the
// wiring inventory (specs/010-ultimate-consolidation/wiring-inventory-data.md)
// promises. It does NOT hardcode a mirror of the doc's identifiers — every
// live-surface set below is derived from the real running code (a real
// workspace+engine's tool schema, an AST parse of the actual switch
// statements, reflection over the actual structs, a regex scan of the actual
// event-emitting source) so a tool/command/keybinding/setting/event/callback
// added or removed without a matching doc edit fails this test in EITHER
// direction, per WI-3.

// wiringModuleRoot walks up from the test's working directory to the
// directory holding go.mod (same approach as internal/arch's moduleRoot;
// duplicated here because unexported test helpers do not cross packages).
func wiringModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}

// --- doc parsing -----------------------------------------------------------

type inventoryRow struct {
	Surface, Identifier, VerifiedBy, Status string
	Line                                    int
}

// parseWiringInventory reads the checked-in inventory's markdown tables and
// extracts every data row. Rows are recognized purely by shape (`| a | b | c
// | d | e |` with exactly 5 cells, skipping the header and the `---`
// separator), so the doc is free to use any number of `##` sections without
// the parser caring which section a row lives under — the Surface column is
// what groups rows, not the heading above them.
func parseWiringInventory(t *testing.T, path string) []inventoryRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var rows []inventoryRow
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}
		trimmed := strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
		cells := strings.Split(trimmed, "|")
		if len(cells) != 5 {
			continue
		}
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if cells[0] == "Surface" {
			continue // header row
		}
		if isMarkdownSeparatorRow(cells) {
			continue
		}
		rows = append(rows, inventoryRow{Surface: cells[0], Identifier: cells[1], VerifiedBy: cells[3], Status: cells[4], Line: i + 1})
	}
	return rows
}

func isMarkdownSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

// --- live surface: tools -----------------------------------------------

// liveToolNames builds the REAL registry a production session uses
// (workspace.New(...).Tools(), exactly as internal/command/application.go
// does) and reads its real, live tool-schema names off a real Engine via the
// exported SessionToolNames feeder (orchestrator/definitions.go) — the same
// derivation instructions_wiring_test.go's TestWiring_PrefixBytesGolden
// exercises for the prefix bytes. web_search and mcp__* tools are session-
// conditional (a network probe / discovered servers respectively, neither of
// which this test can or should depend on), so they are proven structurally
// instead: web_search via its real Definition().Function.Name, and the
// mcp__* dynamic-tool mechanism via a real Registry.Add/Definitions
// round-trip with a synthetic mcp__ tool.
func liveToolNames(t *testing.T) map[string]bool {
	t.Helper()
	ws, err := workspace.New(t.TempDir(), workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: workspace.NewMemoryTrustStore()})
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	settings := &contract.Settings{Version: 1, Effort: contract.EffortMedium, PermissionMode: contract.PermissionNormal}
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{Settings: settings, Provider: inertProvider{}, Registry: orchestrator.NewRegistry(ws.Tools()...)})
	if err != nil {
		t.Fatalf("orchestrator.NewEngine: %v", err)
	}
	names := map[string]bool{}
	for _, name := range engine.SessionToolNames() {
		names[name] = true
	}
	if (gateway.WebSearchTool{}).Definition().Function.Name == "web_search" {
		names["web_search"] = true
	}
	if mcpRegistrySupportsDynamicTools(t) {
		names["mcp__* (dynamic)"] = true
	}
	return names
}

type fakeMCPTool struct{ name string }

func (f fakeMCPTool) Definition() contract.ToolDefinition {
	return contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: f.name, Description: "probe"}}
}
func (f fakeMCPTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

// mcpRegistrySupportsDynamicTools proves the live mechanism registry.go
// implements for mcp__-prefixed tools: BaseDefinitions excludes them (so the
// byte-stable session prefix never varies with the MCP server set) while
// Definitions/MCPDefinitions includes them (so they still reach the model).
func mcpRegistrySupportsDynamicTools(t *testing.T) bool {
	t.Helper()
	registry := orchestrator.NewRegistry()
	registry.Add(fakeMCPTool{name: "mcp__probe__ping"})
	if hasName(registry.BaseDefinitions(nil), "mcp__probe__ping") {
		t.Error("BaseDefinitions unexpectedly included an mcp__ tool — the prefix-stability invariant would break")
		return false
	}
	return hasName(registry.Definitions(nil), "mcp__probe__ping") && hasName(registry.MCPDefinitions(nil), "mcp__probe__ping")
}

func hasName(definitions []contract.ToolDefinition, name string) bool {
	for _, d := range definitions {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}

// --- live surface: slash commands and keybindings ---------------------

// extractSwitchStringCases parses the named function's top-level switch
// statement out of the given source file (a real go/ast parse, not a text
// grep) and returns every string-literal case label — including
// comma-joined aliases (`case "/permissions", "/mode":`), which is exactly
// how slash.go's runSlash encodes its alias commands.
func extractSwitchStringCases(t *testing.T, path, funcName string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var cases []string
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName || fn.Body == nil {
			continue
		}
		found = true
		for _, stmt := range fn.Body.List {
			sw, ok := stmt.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			for _, clause := range sw.Body.List {
				cc, ok := clause.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range cc.List {
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if value, err := strconv.Unquote(lit.Value); err == nil {
						cases = append(cases, value)
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("function %s not found in %s", funcName, path)
	}
	return cases
}

// liveSlashCommands parses the REAL switch statement inside runSlash
// (internal/tui/slash.go), so a command or alias added there without a
// matching inventory row fails this test.
func liveSlashCommands(t *testing.T, root string) map[string]bool {
	t.Helper()
	path := filepath.Join(root, "internal", "tui", "slash.go")
	set := map[string]bool{}
	for _, c := range extractSwitchStringCases(t, path, "runSlash") {
		set[c] = true
	}
	return set
}

// altDigitMarker is the exact source fragment keys.go uses to recognize the
// alt+1..9 direct agent-select shortcuts, which are matched by a prefix test
// rather than a switch case (so extractSwitchStringCases cannot see them).
// Its presence is checked directly in the live source, so removing that
// block — not just editing the inventory — is what makes this row disappear.
const altDigitMarker = `strings.HasPrefix(stroke, "alt+")`

// liveKeybindings parses the REAL switch statement inside handleKey
// (internal/tui/keys.go) for its literal key-stroke cases, plus a direct
// source check for the alt+<digit> block that handleKey recognizes outside
// the switch.
func liveKeybindings(t *testing.T, root string) map[string]bool {
	t.Helper()
	path := filepath.Join(root, "internal", "tui", "keys.go")
	set := map[string]bool{}
	for _, c := range extractSwitchStringCases(t, path, "handleKey") {
		set[c] = true
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(src), altDigitMarker) {
		set["alt+<1-9>"] = true
	}
	return set
}

// --- live surface: Settings fields and Callbacks members ---------------

// settingsFieldPaths walks contract.Settings by reflection, recursing into
// nested struct fields (Provider/Shell/RTL/UI) and building a dotted path
// from each field's real `json` tag — the same shape settings.json itself
// uses. A field's removal (like UI.Density, feature 010 WI-5) or addition is
// picked up automatically; nothing here is a copy of field names.
func settingsFieldPaths() []string {
	var paths []string
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				name = field.Name
			}
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			if field.Type.Kind() == reflect.Struct {
				walk(field.Type, path)
				continue
			}
			paths = append(paths, path)
		}
	}
	walk(reflect.TypeOf(contract.Settings{}), "")
	sort.Strings(paths)
	return paths
}

// callbacksMemberNames lists every field of contract.Callbacks by reflection.
func callbacksMemberNames() []string {
	t := reflect.TypeOf(contract.Callbacks{})
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		names = append(names, t.Field(i).Name)
	}
	sort.Strings(names)
	return names
}

func setOf(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

// --- live surface: AgentEvent kinds and the Handoff field ---------------

// agentEventKindPattern matches every `Kind: "..."` literal subagent.go
// writes onto a contract.AgentEvent — the actual emission sites, not a
// separately-maintained enum (contract.AgentEvent.Kind is a bare string).
var agentEventKindPattern = regexp.MustCompile(`Kind:\s*"([a-z_]+)"`)

func liveAgentEventKinds(t *testing.T, root string) map[string]bool {
	t.Helper()
	path := filepath.Join(root, "internal", "orchestrator", "subagent.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	set := map[string]bool{}
	for _, m := range agentEventKindPattern.FindAllStringSubmatch(string(src), -1) {
		set[m[1]] = true
	}
	return set
}

// liveAgentEventFields proves WI-6's Handoff carve-out structurally:
// contract.AgentEvent must still declare the field (reflection), AND the
// delegation benchmark's audit — its one real consumer (RL-021) — must still
// reference it. Losing either makes "Handoff" disappear from the live set.
func liveAgentEventFields(t *testing.T, root string) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	if _, ok := reflect.TypeOf(contract.AgentEvent{}).FieldByName("Handoff"); !ok {
		return set
	}
	path := filepath.Join(root, "benchmarks", "delegationbench", "audit.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(src), ".Handoff") {
		set["Handoff"] = true
	}
	return set
}

// --- the guard itself ----------------------------------------------------

func TestWiringInventoryMatchesLiveSurface(t *testing.T) {
	root := wiringModuleRoot(t)
	docPath := filepath.Join(root, "specs", "010-ultimate-consolidation", "wiring-inventory-data.md")
	rows := parseWiringInventory(t, docPath)
	if len(rows) == 0 {
		t.Fatalf("no rows parsed from %s — is the table still `| Surface | Identifier | Advertised behavior | Verified-by | Status |`?", docPath)
	}

	live := map[string]map[string]bool{
		"Tool":             liveToolNames(t),
		"Slash Command":    liveSlashCommands(t, root),
		"Keybinding":       liveKeybindings(t, root),
		"Settings Field":   setOf(settingsFieldPaths()),
		"AgentEvent Kind":  liveAgentEventKinds(t, root),
		"AgentEvent Field": liveAgentEventFields(t, root),
		"Callbacks Member": setOf(callbacksMemberNames()),
	}

	docWired := map[string]map[string]bool{}
	docRemoved := map[string]map[string]bool{}
	seenSurfaces := map[string]bool{}
	for _, row := range rows {
		seenSurfaces[row.Surface] = true
		if strings.TrimSpace(row.VerifiedBy) == "" {
			t.Errorf("%s:%d — %s %q has no Verified-by (WI-2)", docPath, row.Line, row.Surface, row.Identifier)
		}
		switch row.Status {
		case "wired":
			if docWired[row.Surface] == nil {
				docWired[row.Surface] = map[string]bool{}
			}
			if docWired[row.Surface][row.Identifier] {
				t.Errorf("%s:%d — duplicate row for %s %q", docPath, row.Line, row.Surface, row.Identifier)
			}
			docWired[row.Surface][row.Identifier] = true
		case "removed":
			if docRemoved[row.Surface] == nil {
				docRemoved[row.Surface] = map[string]bool{}
			}
			docRemoved[row.Surface][row.Identifier] = true
		default:
			t.Errorf("%s:%d — %s %q has status %q; must be exactly \"wired\" or \"removed\" — no third status exists (WI-2)", docPath, row.Line, row.Surface, row.Identifier, row.Status)
		}
	}

	for surface, liveSet := range live {
		if !seenSurfaces[surface] {
			t.Errorf("the live surface has %d %q entries, but the inventory doc has no rows with Surface=%q at all — add them", len(liveSet), surface, surface)
			continue
		}
		wired := docWired[surface]
		removed := docRemoved[surface]
		for id := range liveSet {
			switch {
			case wired[id]:
				// matched
			case removed[id]:
				t.Errorf("%s %q is live at runtime but the inventory marks it \"removed\" — mark it \"wired\" or actually remove the wiring", surface, id)
			default:
				t.Errorf("%s %q is live at runtime but has no inventory row at all — add one to %s (WI-1/WI-3)", surface, id, docPath)
			}
		}
		for id := range wired {
			if !liveSet[id] {
				t.Errorf("the inventory marks %s %q \"wired\" but it is no longer part of the live surface — mark it \"removed\" in the doc or restore the wiring", surface, id)
			}
		}
	}
}
