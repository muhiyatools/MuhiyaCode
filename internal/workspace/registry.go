package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type functionTool struct {
	definition contract.ToolDefinition
	execute    func(context.Context, json.RawMessage) (string, error)
}

func (t functionTool) Definition() contract.ToolDefinition { return t.definition }
func (t functionTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.execute(ctx, input)
}

func (w *Workspace) Tools() []contract.Tool {
	return []contract.Tool{
		w.tool("list_files", "List a directory. Use once to map a workspace.", object(map[string]any{"path": stringProp("Directory, default ."), "recursive": boolProp(), "maxEntries": intProp(1, 2000)}, nil), w.execList),
		w.tool("read_file", "Read a UTF-8 file with line numbers. Use offset/limit for large files.", object(map[string]any{"path": stringProp("Workspace-relative path"), "offset": intProp(1, 0), "limit": intProp(1, MaxReadLines)}, []string{"path"}), w.execRead),
		w.tool("grep", "Regex/literal search with file, line, and text results.", object(map[string]any{"pattern": stringProp("Regex or literal"), "path": stringProp("File/directory, default ."), "glob": stringProp("Optional file glob"), "ignoreCase": boolProp(), "literal": boolProp(), "maxResults": intProp(1, 500)}, []string{"pattern"}), w.execGrep),
		w.tool("search_text", "Fast literal text search; prefer grep for regex.", object(map[string]any{"query": stringProp("Literal query"), "path": stringProp("File/directory, default ."), "maxResults": intProp(1, 500)}, []string{"query"}), w.execSearch),
		w.tool("glob", "Find files by doublestar glob, e.g. **/*.go.", object(map[string]any{"pattern": stringProp("Glob"), "path": stringProp("Directory, default ."), "maxResults": intProp(1, 2000)}, []string{"pattern"}), w.execGlob),
		w.tool("edit_file", "Replace exact text in a file already read. Returns a compact diff.", object(map[string]any{"path": stringProp("File"), "oldString": stringProp("Exact current text"), "newString": stringProp("Replacement"), "replaceAll": boolProp()}, []string{"path", "oldString", "newString"}), w.execEdit),
		w.tool("multi_edit", "Apply several ordered exact replacements to one read file.", object(map[string]any{"path": stringProp("File"), "edits": map[string]any{"type": "array", "minItems": 1, "maxItems": 30, "items": object(map[string]any{"oldString": stringProp("Exact current text"), "newString": stringProp("Replacement"), "replaceAll": boolProp()}, []string{"oldString", "newString"})}}, []string{"path", "edits"}), w.execMultiEdit),
		w.tool("write_file", "Create a UTF-8 file, or replace an existing file only after reading it.", object(map[string]any{"path": stringProp("File"), "content": stringProp("Complete content")}, []string{"path", "content"}), w.execWrite),
		w.tool("apply_patch", "Apply a standard unified diff to files already read.", object(map[string]any{"patch": stringProp("Unified diff with ---/+++ headers")}, []string{"patch"}), w.execPatch),
		w.tool("run_shell", "Run a shell command in the workspace with streaming output and cancellation.", object(map[string]any{"command": stringProp("Shell command"), "timeoutMs": intProp(1, 600000)}, []string{"command"}), w.execShell),
		w.tool("git_status", "Show concise git status.", object(map[string]any{}, nil), w.execGitStatus),
		w.tool("git_diff", "Show the workspace git diff.", object(map[string]any{"staged": boolProp(), "path": stringProp("Optional path"), "context": intProp(1, 100)}, nil), w.execGitDiff),
	}
}

func (w *Workspace) tool(name, description string, parameters map[string]any, execute func(context.Context, json.RawMessage) (string, error)) contract.Tool {
	return functionTool{definition: contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: name, Description: description, Parameters: parameters}}, execute: execute}
}

func (w *Workspace) execList(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Path       string `json:"path"`
		Recursive  bool   `json:"recursive"`
		MaxEntries int    `json:"maxEntries"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	result, err := w.List(ctx, ListOptions{Path: input.Path, Recursive: input.Recursive, MaxEntries: input.MaxEntries})
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("Listed %s (%d entries).", result.Root, len(result.Entries))}
	for _, entry := range result.Entries {
		marker := "file"
		if entry.Mode.IsDir() {
			marker = "dir "
		}
		lines = append(lines, fmt.Sprintf("%s  %s", marker, entry.Path))
	}
	if result.Truncated {
		lines = append(lines, "... entries truncated; narrow the path ...")
	}
	return strings.Join(lines, "\n"), nil
}

func (w *Workspace) execRead(ctx context.Context, raw json.RawMessage) (string, error) {
	var input ReadOptions
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	result, err := w.Read(ctx, input)
	if err != nil {
		return "", err
	}
	end := result.Offset + len(result.Lines) - 1
	lines := []string{fmt.Sprintf("Read %s (lines %d-%d of %d).", result.Path, result.Offset, end, result.TotalLines)}
	if result.Outline != "" {
		lines = append(lines, "Outline: "+result.Outline)
	}
	for index, line := range result.Lines {
		lines = append(lines, fmt.Sprintf("%d | %s", result.Offset+index, line))
	}
	if result.Remaining > 0 {
		lines = append(lines, fmt.Sprintf("... %d lines remain; continue at offset %d ...", result.Remaining, end+1))
	}
	return strings.Join(lines, "\n"), nil
}

func (w *Workspace) execGrep(ctx context.Context, raw json.RawMessage) (string, error) {
	var input GrepOptions
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	return w.formatSearch(ctx, input)
}

func (w *Workspace) execSearch(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Query      string `json:"query"`
		Path       string `json:"path"`
		MaxResults int    `json:"maxResults"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	return w.formatSearch(ctx, GrepOptions{Pattern: input.Query, Path: input.Path, Literal: true, MaxResults: input.MaxResults})
}

func (w *Workspace) formatSearch(ctx context.Context, input GrepOptions) (string, error) {
	result, err := w.Grep(ctx, input)
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("Found %d match(es).", len(result.Matches))}
	for _, match := range result.Matches {
		lines = append(lines, fmt.Sprintf("%s:%d: %s", match.Path, match.Line, match.Text))
	}
	if result.Truncated {
		lines = append(lines, "... matches truncated; narrow the query ...")
	}
	return strings.Join(lines, "\n"), nil
}

func (w *Workspace) execGlob(ctx context.Context, raw json.RawMessage) (string, error) {
	var input GlobOptions
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	result, err := w.Glob(ctx, input)
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("Matched %d file(s).", len(result))}
	for _, match := range result {
		lines = append(lines, match.Path)
	}
	return strings.Join(lines, "\n"), nil
}

func (w *Workspace) execEdit(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Path       string `json:"path"`
		OldString  string `json:"oldString"`
		NewString  string `json:"newString"`
		ReplaceAll bool   `json:"replaceAll"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Path == "" || input.OldString == "" {
		return "", fmt.Errorf("path and oldString are required")
	}
	result, err := w.Edit(ctx, input.Path, Edit{Old: input.OldString, New: input.NewString, ReplaceAll: input.ReplaceAll})
	return formatEdit(result), err
}

func (w *Workspace) execMultiEdit(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Path  string `json:"path"`
		Edits []Edit `json:"edits"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Path == "" || len(input.Edits) == 0 {
		return "", fmt.Errorf("path and edits are required")
	}
	result, err := w.MultiEdit(ctx, input.Path, input.Edits)
	return formatEdit(result), err
}

func formatEdit(result EditResult) string {
	lines := []string{result.Summary}
	lines = append(lines, result.Notes...)
	lines = append(lines, result.Skipped...)
	if result.Diff != "" {
		lines = append(lines, "--- diff ---", result.Diff)
	}
	return strings.Join(lines, "\n")
}

func (w *Workspace) execWrite(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	result, err := w.Write(ctx, input.Path, input.Content)
	if err != nil {
		return "", err
	}
	if result.Diff != "" {
		return result.Summary + "\n--- diff ---\n" + result.Diff, nil
	}
	return result.Summary, nil
}

func (w *Workspace) execPatch(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Patch string `json:"patch"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Patch == "" {
		return "", fmt.Errorf("patch is required")
	}
	result, err := w.ApplyPatch(ctx, input.Patch)
	return result.Summary, err
}

func (w *Workspace) execShell(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Command   string `json:"command"`
		TimeoutMS int    `json:"timeoutMs"`
	}
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	if input.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	result, err := w.RunShell(ctx, input.Command, time.Duration(input.TimeoutMS)*time.Millisecond)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("exit code: %d\n%s", result.ExitCode, result.Output), nil
}

func (w *Workspace) execGitStatus(ctx context.Context, _ json.RawMessage) (string, error) {
	result, err := w.GitStatus(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("exit code: %d\n%s", result.ExitCode, result.Output), nil
}

func (w *Workspace) execGitDiff(ctx context.Context, raw json.RawMessage) (string, error) {
	var input GitDiffOptions
	if err := decode(raw, &input); err != nil {
		return "", err
	}
	result, err := w.GitDiff(ctx, input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("exit code: %d\n%s", result.ExitCode, result.Output), nil
}

func decode(raw json.RawMessage, output any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

func object(properties map[string]any, required []string) map[string]any {
	value := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		value["required"] = required
	}
	return value
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func boolProp() map[string]any { return map[string]any{"type": "boolean"} }

func intProp(minimum, maximum int) map[string]any {
	value := map[string]any{"type": "integer"}
	if minimum > 0 {
		value["minimum"] = minimum
	}
	if maximum > 0 {
		value["maximum"] = maximum
	}
	return value
}
