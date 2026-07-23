package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"go/token"
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
	if input.Mode != "outline" && input.Mode != "definition" && input.Mode != "references" {
		return "", fmt.Errorf("mode must be outline, definition, or references")
	}
	if (input.Mode == "definition" || input.Mode == "references") && input.Symbol == "" {
		return "", fmt.Errorf("symbol is required for %s mode", input.Mode)
	}

	// Resolve and authorize the path through the workspace guard.
	target, err := w.authorizePath(ctx, ActionRead, input.Path)
	if err != nil {
		return "", err
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
		if strings.HasSuffix(input.Path, ".go") {
			return "", fmt.Errorf("inspect_code only supports Go source files (.go)")
		}
		return "", fmt.Errorf("no Go source files found in %s", input.Path)
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
		outline, err := ParseFileOutline(fset, file)
		if err != nil && outline == nil {
			continue
		}

		relPath := relativeSlash(w.root, file)
		if len(files) > 1 || b.Len() == 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("Outline of %s (package %s)\n", relPath, outline.Package))
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
	results, err := FindDefinitions(fset, files, symbol)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("No definition found for %q. Try a broader path or check the symbol name.", symbol), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Definitions of %q:\n\n", symbol))
	for _, entry := range results {
		relPath := relativeSlash(w.root, entry.File)
		b.WriteString(fmt.Sprintf("  %s:%d  %s\n", relPath, entry.Line, entry.Signature))
	}
	b.WriteString(fmt.Sprintf("\n%d definition(s) found.\n", len(results)))
	return capOutput(b.String()), nil
}

func (w *Workspace) inspectReferences(fset *token.FileSet, files []string, symbol, target string) (string, error) {
	results, truncated, err := FindReferences(fset, files, symbol, maxReferences)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("No references found for %q. Try a broader path or check the symbol name.", symbol), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("References to %q:\n\n", symbol))
	for _, ref := range results {
		relPath := relativeSlash(w.root, ref.File)
		b.WriteString(fmt.Sprintf("  %s:%d  %s\n", relPath, ref.Line, ref.Context))
	}
	if truncated {
		b.WriteString(fmt.Sprintf("\n... %d references shown (capped at %d; use a narrower path).\n", len(results), maxReferences))
	} else {
		b.WriteString(fmt.Sprintf("\n%d reference(s) found.\n", len(results)))
	}
	return capOutput(b.String()), nil
}

func capOutput(output string) string {
	if len(output) <= maxOutputBytes {
		return output
	}
	return output[:maxOutputBytes] + "\n... output truncated"
}
