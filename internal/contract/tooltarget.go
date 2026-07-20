package contract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolTarget derives the short display target for a tool call from its input
// arguments — the file path, shell command, search pattern, web query, or
// subagent title/kind that names WHAT the call acted on. It is the canonical
// impl shared by the live TUI (which computes it from the streaming tool_start
// input) and the engine (which persists it alongside each tool event so a
// resumed transcript renders the same target it showed live). Returns "" when
// no target field is present. Pure: input is a JSON object; malformed input
// yields "".
func ToolTarget(name string, input []byte) string {
	var values map[string]any
	_ = json.Unmarshal(input, &values)
	// apply_patch carries no "path" argument — its target lives in the unified
	// diff's file headers, so derive it there like the workspace resolver does.
	if name == "apply_patch" {
		if patch, ok := values["patch"].(string); ok {
			return PatchTargetFile(patch)
		}
		return ""
	}
	for _, key := range []string{"path", "command", "query", "pattern", "title", "agent"} {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// PatchTargetFile derives the display target for an apply_patch call from its
// unified-diff body, mirroring the workspace patch resolver: the +++ name, or
// the --- name when the +++ side is /dev/null (a deletion), with a/ or b/
// prefixes stripped. Multiple distinct files render as the first followed by
// " (+N more)". Returns "" when no file header can be parsed.
func PatchTargetFile(patch string) string {
	files := PatchTargetFiles(patch)
	if len(files) == 0 {
		return ""
	}
	if len(files) == 1 {
		return files[0]
	}
	return fmt.Sprintf("%s (+%d more)", files[0], len(files)-1)
}

// PatchTargetFiles returns EVERY file a unified diff touches, in order, with
// a/ and b/ prefixes stripped. PatchTargetFile renders the display form from
// it; callers that must match a specific path (the tasks.md checklist feed)
// need the whole set, because a single patch can edit code and the checklist
// in one call.
func PatchTargetFiles(patch string) []string {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	name := func(v string) string {
		v = strings.TrimSpace(strings.SplitN(v, "\t", 2)[0])
		v = strings.SplitN(v, " ", 2)[0]
		return strings.TrimPrefix(strings.TrimPrefix(v, "a/"), "b/")
	}
	var files []string
	seen := map[string]bool{}
	for i := 0; i+1 < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "--- ") || !strings.HasPrefix(lines[i+1], "+++ ") {
			continue
		}
		target := name(strings.TrimPrefix(lines[i+1], "+++ "))
		if target == "/dev/null" || target == "" {
			target = name(strings.TrimPrefix(lines[i], "--- "))
		}
		if target == "" || target == "/dev/null" || seen[target] {
			continue
		}
		seen[target] = true
		files = append(files, target)
	}
	return files
}

// ToolTargetPaths returns every workspace path a tool call writes, for callers
// that must decide whether a specific file changed. Only mutating tools are
// reported; a read or a shell command returns nothing (a shell redirect into a
// file is deliberately not tracked — the harness cannot parse arbitrary shells).
func ToolTargetPaths(name string, input []byte) []string {
	var values map[string]any
	_ = json.Unmarshal(input, &values)
	switch name {
	case "apply_patch":
		if patch, ok := values["patch"].(string); ok {
			return PatchTargetFiles(patch)
		}
	case "write_file", "edit_file", "multi_edit":
		if path, ok := values["path"].(string); ok && path != "" {
			return []string{path}
		}
	}
	return nil
}
