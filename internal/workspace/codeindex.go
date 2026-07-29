package workspace

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

const (
	maxSymbols     = 200
	maxReferences  = 50
	maxOutputBytes = 3000
)

// SymbolKind classifies a Go declaration.
type SymbolKind string

const (
	SymbolFunc      SymbolKind = "func"
	SymbolMethod    SymbolKind = "method"
	SymbolStruct    SymbolKind = "struct"
	SymbolInterface SymbolKind = "interface"
	SymbolConst     SymbolKind = "const"
	SymbolVar       SymbolKind = "var"
	SymbolType      SymbolKind = "type"
)

// SymbolEntry represents one top-level declaration.
type SymbolEntry struct {
	Name      string     `json:"name"`
	Kind      SymbolKind `json:"kind"`
	Line      int        `json:"line"`
	Signature string     `json:"signature"`
	Receiver  string     `json:"receiver,omitempty"`
	File      string     `json:"file,omitempty"`
}

// FileOutline is the structural summary of one Go source file.
type FileOutline struct {
	Path      string        `json:"path"`
	Package   string        `json:"package"`
	Imports   []string      `json:"imports,omitempty"`
	Symbols   []SymbolEntry `json:"symbols"`
	Truncated bool          `json:"truncated,omitempty"`
	Errors    []string      `json:"errors,omitempty"`
}

// ReferenceEntry is one usage site of a symbol.
type ReferenceEntry struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Context string `json:"context"`
}

// ParseFileOutline parses a single Go source file and extracts its structural
// outline including package name, imports, and all top-level declarations.
// Even on parse errors the returned outline may contain partial results.
func ParseFileOutline(fset *token.FileSet, path string) (outline *FileOutline, err error) {
	source, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, fmt.Errorf("codeindex: read file %s: %w", path, readErr)
	}
	return ParseFileOutlineSource(fset, path, source)
}

// ParseFileOutlineSource parses an immutable source snapshot. Callers that
// already read a file can avoid a second read, and content-addressed caches can
// safely reuse its result even when a file is externally replaced with the
// same size and modification time.
func ParseFileOutlineSource(fset *token.FileSet, path string, source []byte) (outline *FileOutline, err error) {
	if !strings.HasSuffix(strings.ToLower(path), ".go") {
		return ParsePolyglotOutlineSource(path, source)
	}
	outline = &FileOutline{
		Path:    path,
		Symbols: []SymbolEntry{},
	}

	// Guard against any panics during AST walking.
	defer func() {
		if r := recover(); r != nil {
			outline.Errors = append(outline.Errors, fmt.Sprintf("panic during parse: %v", r))
		}
	}()

	if fset == nil {
		return nil, fmt.Errorf("codeindex: fset must not be nil")
	}

	file, parseErr := parser.ParseFile(fset, path, source, parser.AllErrors)
	if parseErr != nil {
		outline.Errors = append(outline.Errors, parseErr.Error())
	}
	// Even with errors the AST may be partially usable.
	if file == nil {
		if parseErr != nil {
			return outline, parseErr
		}
		return outline, fmt.Errorf("codeindex: parser returned nil file for %s", path)
	}

	// Package name.
	if file.Name != nil {
		outline.Package = file.Name.Name
	}

	// Imports.
	for _, imp := range file.Imports {
		if imp != nil && imp.Path != nil {
			p := imp.Path.Value
			p = strings.Trim(p, "\"")
			outline.Imports = append(outline.Imports, p)
		}
	}

	// Declarations.
	for _, decl := range file.Decls {
		if decl == nil {
			continue
		}
		switch d := decl.(type) {
		case *ast.FuncDecl:
			outline.Symbols = append(outline.Symbols, parseFuncDecl(fset, d)...)
		case *ast.GenDecl:
			outline.Symbols = append(outline.Symbols, parseGenDecl(fset, d)...)
		}
		// Cap symbols.
		if len(outline.Symbols) > maxSymbols {
			outline.Symbols = outline.Symbols[:maxSymbols]
			outline.Truncated = true
			break
		}
	}

	return outline, nil
}

// parseFuncDecl extracts a SymbolEntry from a function or method declaration.
func parseFuncDecl(fset *token.FileSet, decl *ast.FuncDecl) []SymbolEntry {
	if decl == nil || decl.Name == nil {
		return nil
	}

	entry := SymbolEntry{
		Name:      decl.Name.Name,
		Kind:      SymbolFunc,
		Line:      safePos(fset, decl.Pos()),
		Signature: renderFuncSignature(fset, decl),
	}

	if decl.Recv != nil && decl.Recv.List != nil && len(decl.Recv.List) > 0 {
		entry.Kind = SymbolMethod
		entry.Receiver = renderFieldList(fset, decl.Recv)
	}

	return []SymbolEntry{entry}
}

// parseGenDecl extracts SymbolEntry items from a general declaration (type, const, var).
func parseGenDecl(fset *token.FileSet, decl *ast.GenDecl) []SymbolEntry {
	if decl == nil {
		return nil
	}
	var entries []SymbolEntry

	switch decl.Tok {
	case token.TYPE:
		for _, spec := range decl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts == nil || ts.Name == nil {
				continue
			}
			entry := SymbolEntry{
				Name: ts.Name.Name,
				Kind: SymbolType,
				Line: safePos(fset, ts.Pos()),
			}
			switch t := ts.Type.(type) {
			case *ast.StructType:
				entry.Kind = SymbolStruct
				entry.Signature = "type " + ts.Name.Name + " struct " + renderStructFields(fset, t)
			case *ast.InterfaceType:
				entry.Kind = SymbolInterface
				entry.Signature = "type " + ts.Name.Name + " interface " + renderInterfaceMethods(fset, t)
			default:
				entry.Signature = "type " + ts.Name.Name + " " + renderTypeExpr(ts.Type)
			}
			entries = append(entries, entry)
		}

	case token.CONST:
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs == nil {
				continue
			}
			for _, name := range vs.Names {
				if name == nil {
					continue
				}
				entry := SymbolEntry{
					Name: name.Name,
					Kind: SymbolConst,
					Line: safePos(fset, name.Pos()),
				}
				if vs.Type != nil {
					entry.Signature = "const " + name.Name + " " + renderTypeExpr(vs.Type)
				} else {
					entry.Signature = "const " + name.Name
				}
				entries = append(entries, entry)
			}
		}

	case token.VAR:
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs == nil {
				continue
			}
			for _, name := range vs.Names {
				if name == nil {
					continue
				}
				entry := SymbolEntry{
					Name: name.Name,
					Kind: SymbolVar,
					Line: safePos(fset, name.Pos()),
				}
				if vs.Type != nil {
					entry.Signature = "var " + name.Name + " " + renderTypeExpr(vs.Type)
				} else {
					entry.Signature = "var " + name.Name
				}
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

// renderFuncSignature builds the full text signature for a function or method.
func renderFuncSignature(fset *token.FileSet, decl *ast.FuncDecl) string {
	if decl == nil || decl.Name == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("func ")

	// Receiver.
	if decl.Recv != nil && decl.Recv.List != nil && len(decl.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(renderFieldList(fset, decl.Recv))
		b.WriteString(") ")
	}

	b.WriteString(decl.Name.Name)
	b.WriteString("(")
	if decl.Type != nil {
		b.WriteString(renderFieldList(fset, decl.Type.Params))
	}
	b.WriteString(")")

	// Results.
	if decl.Type != nil && decl.Type.Results != nil && decl.Type.Results.List != nil && len(decl.Type.Results.List) > 0 {
		results := renderFieldList(fset, decl.Type.Results)
		if len(decl.Type.Results.List) == 1 && len(decl.Type.Results.List[0].Names) == 0 {
			b.WriteString(" ")
			b.WriteString(results)
		} else {
			b.WriteString(" (")
			b.WriteString(results)
			b.WriteString(")")
		}
	}

	return b.String()
}

// renderFieldList renders a parameter/result/receiver field list as a
// comma-separated string.
func renderFieldList(fset *token.FileSet, fl *ast.FieldList) string {
	if fl == nil || fl.List == nil || len(fl.List) == 0 {
		return ""
	}

	var parts []string
	for _, field := range fl.List {
		if field == nil {
			continue
		}
		typeStr := renderTypeExpr(field.Type)
		if len(field.Names) == 0 {
			// Unnamed (e.g. return types, embedded fields).
			parts = append(parts, typeStr)
		} else {
			for _, name := range field.Names {
				if name == nil {
					continue
				}
				parts = append(parts, name.Name+" "+typeStr)
			}
		}
	}
	return strings.Join(parts, ", ")
}

// renderTypeExpr converts an ast.Expr representing a type into a readable string.
func renderTypeExpr(expr ast.Expr) string {
	if expr == nil {
		return "?"
	}

	switch t := expr.(type) {
	case *ast.Ident:
		if t == nil {
			return "?"
		}
		return t.Name

	case *ast.StarExpr:
		if t == nil {
			return "*?"
		}
		return "*" + renderTypeExpr(t.X)

	case *ast.ArrayType:
		if t == nil {
			return "[]?"
		}
		return "[]" + renderTypeExpr(t.Elt)

	case *ast.MapType:
		if t == nil {
			return "map[?]?"
		}
		return "map[" + renderTypeExpr(t.Key) + "]" + renderTypeExpr(t.Value)

	case *ast.SelectorExpr:
		if t == nil || t.Sel == nil {
			return "?.?"
		}
		return renderTypeExpr(t.X) + "." + t.Sel.Name

	case *ast.FuncType:
		return "func(...)"

	case *ast.InterfaceType:
		return "interface{}"

	case *ast.StructType:
		return "struct{}"

	case *ast.Ellipsis:
		if t == nil {
			return "...?"
		}
		return "..." + renderTypeExpr(t.Elt)

	case *ast.ChanType:
		if t == nil {
			return "chan ?"
		}
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + renderTypeExpr(t.Value)
		case ast.RECV:
			return "<-chan " + renderTypeExpr(t.Value)
		default:
			return "chan " + renderTypeExpr(t.Value)
		}

	case *ast.ParenExpr:
		if t == nil {
			return "(?)"
		}
		return "(" + renderTypeExpr(t.X) + ")"

	default:
		return "?"
	}
}

// renderStructFields builds a compact representation of struct fields.
func renderStructFields(fset *token.FileSet, st *ast.StructType) string {
	if st == nil || st.Fields == nil || st.Fields.List == nil || len(st.Fields.List) == 0 {
		return "{}"
	}

	fields := st.Fields.List
	limit := 5
	truncated := false
	remaining := 0
	if len(fields) > limit {
		remaining = len(fields) - limit
		fields = fields[:limit]
		truncated = true
	}

	var parts []string
	for _, field := range fields {
		if field == nil {
			continue
		}
		typeStr := renderTypeExpr(field.Type)
		if len(field.Names) == 0 {
			// Embedded field.
			parts = append(parts, typeStr)
		} else {
			for _, name := range field.Names {
				if name == nil {
					continue
				}
				parts = append(parts, name.Name+" "+typeStr)
			}
		}
	}

	result := "{ " + strings.Join(parts, "; ")
	if truncated {
		result += fmt.Sprintf("; ... %d more", remaining)
	}
	result += " }"
	return result
}

// renderInterfaceMethods builds a compact representation of interface methods.
func renderInterfaceMethods(fset *token.FileSet, iface *ast.InterfaceType) string {
	if iface == nil || iface.Methods == nil || iface.Methods.List == nil || len(iface.Methods.List) == 0 {
		return "{}"
	}

	methods := iface.Methods.List
	limit := 5
	truncated := false
	remaining := 0
	if len(methods) > limit {
		remaining = len(methods) - limit
		methods = methods[:limit]
		truncated = true
	}

	var parts []string
	for _, m := range methods {
		if m == nil {
			continue
		}
		switch t := m.Type.(type) {
		case *ast.FuncType:
			// Method signature.
			if len(m.Names) > 0 && m.Names[0] != nil {
				name := m.Names[0].Name
				params := renderFieldList(fset, t.Params)
				results := ""
				if t.Results != nil && t.Results.List != nil && len(t.Results.List) > 0 {
					r := renderFieldList(fset, t.Results)
					if len(t.Results.List) == 1 && len(t.Results.List[0].Names) == 0 {
						results = " " + r
					} else {
						results = " (" + r + ")"
					}
				}
				parts = append(parts, name+"("+params+")"+results)
			}
		default:
			// Embedded interface.
			parts = append(parts, renderTypeExpr(m.Type))
		}
	}

	result := "{ " + strings.Join(parts, "; ")
	if truncated {
		result += fmt.Sprintf("; ... %d more", remaining)
	}
	result += " }"
	return result
}

// FindDefinitions searches the given Go files for top-level declarations
// matching the given symbol name exactly.
func FindDefinitions(fset *token.FileSet, paths []string, symbol string) ([]SymbolEntry, error) {
	if fset == nil {
		return nil, fmt.Errorf("codeindex: fset must not be nil")
	}
	if symbol == "" {
		return nil, fmt.Errorf("codeindex: symbol must not be empty")
	}

	var results []SymbolEntry
	for _, p := range paths {
		outline, err := ParseFileOutline(fset, p)
		if err != nil && outline == nil {
			continue
		}
		if outline == nil {
			continue
		}
		for _, sym := range outline.Symbols {
			if sym.Name == symbol {
				sym.File = p
				results = append(results, sym)
			}
		}
	}
	return results, nil
}

// FindReferences searches Go files for all identifier usages of the given symbol.
func FindReferences(fset *token.FileSet, paths []string, symbol string, maxResults int) ([]ReferenceEntry, bool, error) {
	if fset == nil {
		return nil, false, fmt.Errorf("codeindex: fset must not be nil")
	}
	if symbol == "" {
		return nil, false, fmt.Errorf("codeindex: symbol must not be empty")
	}
	if maxResults <= 0 {
		maxResults = maxReferences
	}

	var results []ReferenceEntry
	truncated := false

	for _, p := range paths {
		if truncated {
			break
		}

		source, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, p, source, parser.AllErrors)
		if file == nil {
			_ = err
			continue
		}

		ast.Inspect(file, func(n ast.Node) bool {
			if truncated {
				return false
			}
			ident, ok := n.(*ast.Ident)
			if !ok || ident == nil {
				return true
			}
			if ident.Name != symbol {
				return true
			}

			pos := fset.Position(ident.Pos())
			ctx := sourceLine(source, pos.Line)

			results = append(results, ReferenceEntry{
				File:    p,
				Line:    pos.Line,
				Context: ctx,
			})

			if len(results) >= maxResults {
				truncated = true
				return false
			}
			return true
		})
	}

	return results, truncated, nil
}

// collectGoFiles gathers Go source files from the given path.
func collectGoFiles(path string, includeTests bool) ([]string, error) {
	return CollectSourceFiles(path, includeTests)
}

func sourceLine(source []byte, line int) string {
	if line <= 0 {
		return ""
	}
	lines := strings.Split(string(source), "\n")
	if line > len(lines) {
		return ""
	}

	result := strings.TrimSpace(lines[line-1])
	if len(result) > 120 {
		result = result[:120]
	}
	return result
}

// safePos converts an ast position to a line number.
func safePos(fset *token.FileSet, pos token.Pos) int {
	if fset == nil || !pos.IsValid() {
		return 0
	}
	return fset.Position(pos).Line
}
