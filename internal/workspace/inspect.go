package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	maxInspectFiles = 12
	maxInspectLines = 600
)

type inspectReadInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type inspectInput struct {
	Mode       string             `json:"mode"`
	Path       string             `json:"path,omitempty"`
	Query      string             `json:"query,omitempty"`
	Pattern    string             `json:"pattern,omitempty"`
	Glob       string             `json:"glob,omitempty"`
	IgnoreCase bool               `json:"ignoreCase,omitempty"`
	Files      []inspectReadInput `json:"files,omitempty"`
	Limit      int                `json:"limit,omitempty"`
}

func (w *Workspace) inspectTool() contract.Tool {
	file := object(map[string]any{
		"path": stringProp("Workspace-relative path"), "offset": intProp(1, 0), "limit": intProp(1, MaxReadLines),
	}, []string{"path"})
	parameters := object(map[string]any{
		"mode": map[string]any{"type": "string", "enum": []string{"map", "search", "read_many", "symbol"}},
		"path": stringProp("Directory/file, default ."), "query": stringProp("Literal search or symbol"),
		"pattern": stringProp("Regex search"), "glob": stringProp("Optional file glob"),
		"ignoreCase": boolProp(), "files": map[string]any{"type": "array", "maxItems": maxInspectFiles, "items": file},
		"limit": intProp(1, 500),
	}, []string{"mode"})
	return w.tool("inspect_workspace", "Batch workspace map, search, and bounded multi-file reads with exact coordinates. Symbol mode reports unavailable until indexed.", parameters, w.execInspect)
}

func (w *Workspace) execInspect(ctx context.Context, raw json.RawMessage) (string, error) {
	var input inspectInput
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	switch input.Mode {
	case "map":
		return w.inspectMap(ctx, input)
	case "search":
		return w.inspectSearch(ctx, input)
	case "read_many":
		return w.inspectReadMany(ctx, input)
	case "symbol":
		return `{"mode":"symbol","available":false,"reason":"repository symbol index is not built; use search mode"}`, nil
	default:
		return "", fmt.Errorf("mode must be map, search, read_many, or symbol")
	}
}

func (w *Workspace) inspectMap(ctx context.Context, input inspectInput) (string, error) {
	limit := min(max(input.Limit, 1), 500)
	result, err := w.List(ctx, ListOptions{Path: input.Path, Recursive: true, MaxEntries: limit})
	if err != nil {
		return "", err
	}
	entries := make([]map[string]any, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, map[string]any{"path": entry.Path, "directory": entry.Mode.IsDir()})
	}
	return marshalInspect(map[string]any{"mode": "map", "root": result.Root, "entries": entries,
		"truncated": result.Truncated, "returned": len(entries)})
}

func (w *Workspace) inspectSearch(ctx context.Context, input inspectInput) (string, error) {
	pattern, literal := input.Pattern, false
	if pattern == "" {
		pattern, literal = input.Query, true
	}
	if strings.TrimSpace(pattern) == "" {
		return "", fmt.Errorf("query or pattern is required")
	}
	limit := min(max(input.Limit, 1), 200)
	result, err := w.Grep(ctx, GrepOptions{Pattern: pattern, Path: input.Path, Glob: input.Glob,
		IgnoreCase: input.IgnoreCase, Literal: literal, MaxResults: limit})
	if err != nil {
		return "", err
	}
	matches := make([]map[string]any, 0, len(result.Matches))
	for _, match := range result.Matches {
		matches = append(matches, map[string]any{"path": match.Path, "line": match.Line, "text": match.Text})
	}
	return marshalInspect(map[string]any{"mode": "search", "matches": matches, "returned": len(matches),
		"truncated": result.Truncated, "skippedFiles": result.SkippedFiles, "firstSkipped": result.FirstSkipped,
		"completeNegative": len(matches) > 0 || result.SkippedFiles == 0 && !result.Truncated})
}

func (w *Workspace) inspectReadMany(ctx context.Context, input inspectInput) (string, error) {
	if len(input.Files) == 0 {
		return "", fmt.Errorf("files are required")
	}
	if len(input.Files) > maxInspectFiles {
		input.Files = input.Files[:maxInspectFiles]
	}
	remaining := maxInspectLines
	items, skipped := []map[string]any{}, []map[string]string{}
	for _, file := range input.Files {
		if remaining <= 0 {
			skipped = append(skipped, map[string]string{"path": file.Path, "reason": "total line budget exhausted"})
			continue
		}
		limit := file.Limit
		if limit <= 0 {
			limit = min(200, remaining)
		}
		limit = min(limit, remaining)
		result, err := w.Read(ctx, ReadOptions{Path: file.Path, Offset: file.Offset, Limit: limit})
		if err != nil {
			skipped = append(skipped, map[string]string{"path": file.Path, "reason": err.Error()})
			continue
		}
		lines := make([]map[string]any, 0, len(result.Lines))
		for index, line := range result.Lines {
			lines = append(lines, map[string]any{"line": result.Offset + index, "text": line})
		}
		remaining -= len(lines)
		items = append(items, map[string]any{"path": result.Path, "lines": lines, "totalLines": result.TotalLines,
			"remainingLines": result.Remaining, "outline": result.Outline})
	}
	return marshalInspect(map[string]any{"mode": "read_many", "files": items, "skipped": skipped,
		"lineBudget": maxInspectLines, "linesReturned": maxInspectLines - remaining})
}

func marshalInspect(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
