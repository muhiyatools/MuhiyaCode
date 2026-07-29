package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"go/token"
	"path/filepath"
	"strings"
)

func (w *Workspace) execInspectCode(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Mode      string `json:"mode"`
		Path      string `json:"path"`
		Symbol    string `json:"symbol"`
		TestFiles *bool  `json:"testFiles"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if input.Mode != "outline" && input.Mode != "definition" && input.Mode != "references" && input.Mode != "repo_map" {
		return "", fmt.Errorf("mode must be outline, definition, references, or repo_map")
	}
	if (input.Mode == "definition" || input.Mode == "references") && input.Symbol == "" {
		return "", fmt.Errorf("symbol is required for %s mode", input.Mode)
	}

	// Resolve and authorize the path through the workspace guard.
	target, err := w.authorizePath(ctx, ActionRead, input.Path)
	if err != nil {
		return "", err
	}
	if input.Mode == "repo_map" {
		return w.GenerateRankedRepoMap(target, 1_000, input.Symbol)
	}

	includeTests := true
	if input.TestFiles != nil {
		includeTests = *input.TestFiles
	}

	// Collect .go files from the resolved path.
	files, err := collectGoFiles(target, includeTests)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no supported source files (Go, TypeScript, JavaScript, Python, Rust, C++, Java) found in %s", input.Path)
	}

	fset := token.NewFileSet()

	switch input.Mode {
	case "outline":
		return w.inspectOutline(fset, files, target)
	case "definition":
		return w.inspectDefinition(fset, files, input.Symbol, target)
	case "references":
		return w.inspectReferences(fset, files, input.Symbol, target)
	default:
		return "", fmt.Errorf("unknown mode %q", input.Mode)
	}
}

func (w *Workspace) inspectOutline(fset *token.FileSet, files []string, target string) (string, error) {
	var b strings.Builder
	totalSymbols := 0

	for _, file := range files {
		outline, err := w.sourceCache.outline(file)
		if err != nil && outline == nil {
			continue
		}

		relPath := relativeSlash(w.root, file)
		capability, _ := CodeIntelligenceFor(file)
		if len(files) > 1 || b.Len() == 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("Outline of %s (package %s)\n", relPath, outline.Package))
			b.WriteString("Capability: " + capability.Outline + "\n")
			if len(outline.Imports) > 0 {
				b.WriteString(fmt.Sprintf("Imports: %s\n", strings.Join(outline.Imports, ", ")))
			}
		}

		if len(outline.Errors) > 0 {
			b.WriteString(fmt.Sprintf("Parse errors: %s\n", strings.Join(outline.Errors, "; ")))
		}

		for _, sym := range outline.Symbols {
			if totalSymbols >= maxSymbols {
				b.WriteString(fmt.Sprintf("\n... %d symbols shown (capped at %d).\n", totalSymbols, maxSymbols))
				return capOutput(b.String()), nil
			}
			b.WriteString(fmt.Sprintf("L%-5d %s\n", sym.Line, sym.Signature))
			totalSymbols++
		}

		if outline.Truncated {
			b.WriteString("... symbols truncated in this file\n")
		}
	}

	b.WriteString(fmt.Sprintf("\n%d symbols shown.\n", totalSymbols))
	return capOutput(b.String()), nil
}

func (w *Workspace) inspectDefinition(fset *token.FileSet, files []string, symbol, target string) (string, error) {
	var results []SymbolEntry
	for _, file := range files {
		outline, err := w.sourceCache.outline(file)
		if err != nil && outline == nil {
			continue
		}
		for _, candidate := range outline.Symbols {
			if candidate.Name == symbol {
				candidate.File = file
				results = append(results, candidate)
			}
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf("No definition found for %q. Try a broader path or check the symbol name.", symbol), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Definitions of %q (built-in index; not compiler/LSP resolved):\n\n", symbol))
	for _, entry := range results {
		relPath := relativeSlash(w.root, entry.File)
		capability, _ := CodeIntelligenceFor(entry.File)
		b.WriteString(fmt.Sprintf("  %s:%d  %s [%s]\n", relPath, entry.Line, entry.Signature, capability.Definition))
	}
	b.WriteString(fmt.Sprintf("\n%d definition(s) found.\n", len(results)))
	return capOutput(b.String()), nil
}

func (w *Workspace) inspectReferences(fset *token.FileSet, files []string, symbol, target string) (string, error) {
	var goFiles, lexicalFiles []string
	for _, file := range files {
		if strings.EqualFold(filepath.Ext(file), ".go") {
			goFiles = append(goFiles, file)
		} else {
			lexicalFiles = append(lexicalFiles, file)
		}
	}
	results, truncated, err := FindReferences(fset, goFiles, symbol, maxReferences)
	if err != nil {
		return "", err
	}
	if !truncated && len(lexicalFiles) > 0 {
		remaining := maxReferences - len(results)
		lexical, lexicalTruncated, lexicalErr := w.findLexicalReferences(lexicalFiles, symbol, remaining)
		if lexicalErr != nil {
			return "", lexicalErr
		}
		results = append(results, lexical...)
		truncated = lexicalTruncated
	}

	if len(results) == 0 {
		return fmt.Sprintf("No references found for %q. Try a broader path or check the symbol name.", symbol), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("References to %q (identifier matches; not compiler/LSP resolved):\n\n", symbol))
	for _, ref := range results {
		relPath := relativeSlash(w.root, ref.File)
		capability, _ := CodeIntelligenceFor(ref.File)
		b.WriteString(fmt.Sprintf("  %s:%d  %s [%s]\n", relPath, ref.Line, ref.Context, capability.References))
	}
	if truncated {
		b.WriteString(fmt.Sprintf("\n... %d references shown (capped at %d; use a narrower path).\n", len(results), maxReferences))
	} else {
		b.WriteString(fmt.Sprintf("\n%d reference(s) found.\n", len(results)))
	}
	return capOutput(b.String()), nil
}

func (w *Workspace) findLexicalReferences(files []string, symbol string, limit int) ([]ReferenceEntry, bool, error) {
	if limit <= 0 {
		return nil, true, nil
	}
	var results []ReferenceEntry
	for _, file := range files {
		source, err := safeReadTargetLimit(w.root, file, MaxReadBytes)
		if err != nil {
			continue
		}
		for index, line := range strings.Split(string(source), "\n") {
			if !containsIdentifier(line, symbol) {
				continue
			}
			results = append(results, ReferenceEntry{
				File: file, Line: index + 1, Context: boundedSourceLine(line),
			})
			if len(results) >= limit {
				return results, true, nil
			}
		}
	}
	return results, false, nil
}

func containsIdentifier(line, symbol string) bool {
	for offset := 0; offset <= len(line)-len(symbol); {
		index := strings.Index(line[offset:], symbol)
		if index < 0 {
			return false
		}
		index += offset
		beforeOK := index == 0 || !identifierByte(line[index-1])
		end := index + len(symbol)
		afterOK := end == len(line) || !identifierByte(line[end])
		if beforeOK && afterOK {
			return true
		}
		offset = index + 1
	}
	return false
}

func identifierByte(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func boundedSourceLine(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > 120 {
		return line[:120]
	}
	return line
}

func capOutput(output string) string {
	if len(output) <= maxOutputBytes {
		return output
	}
	return output[:maxOutputBytes] + "\n... output truncated"
}
