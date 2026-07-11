package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type unifiedPatch struct {
	oldName string
	newName string
	hunks   []patchHunk
}

type patchHunk struct {
	oldStart int
	lines    []string
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func (w *Workspace) ApplyPatch(ctx context.Context, text string) (PatchResult, error) {
	patches, err := parseUnifiedPatch(text)
	if err != nil {
		return PatchResult{}, err
	}
	if len(patches) == 0 {
		return PatchResult{}, fmt.Errorf("patch contains no file headers")
	}
	type change struct {
		path          string
		before, after string
		existed, drop bool
	}
	changes := make([]change, 0, len(patches))
	for _, patch := range patches {
		name := patch.newName
		if name == "/dev/null" || name == "" {
			name = patch.oldName
		}
		name = strings.TrimPrefix(strings.TrimPrefix(name, "a/"), "b/")
		target, err := w.authorizePath(ctx, ActionPatch, name)
		if err != nil {
			return PatchResult{}, err
		}
		data, readErr := os.ReadFile(target)
		existed := readErr == nil
		if readErr != nil && !os.IsNotExist(readErr) {
			return PatchResult{}, readErr
		}
		if existed && !w.canOverwrite(target) {
			return PatchResult{}, fmt.Errorf("%w: %s", ErrUnreadOverwrite, name)
		}
		before := strings.ReplaceAll(string(data), "\r\n", "\n")
		after, err := applyHunks(before, patch.hunks)
		if err != nil {
			return PatchResult{}, fmt.Errorf("patch %s: %w", name, err)
		}
		if strings.Contains(string(data), "\r\n") {
			after = strings.ReplaceAll(after, "\n", "\r\n")
		}
		changes = append(changes, change{path: target, before: string(data), after: after, existed: existed, drop: patch.newName == "/dev/null"})
	}
	paths := make([]string, len(changes))
	for i := range changes {
		paths[i] = changes[i].path
	}
	if err := w.checkpoint(ctx, "before applying patch", paths); err != nil {
		return PatchResult{}, err
	}
	var committed []change
	rollback := func() {
		for i := len(committed) - 1; i >= 0; i-- {
			entry := committed[i]
			if entry.existed {
				_ = writeAtomic(entry.path, []byte(entry.before), 0o644)
			} else {
				_ = os.Remove(entry.path)
			}
		}
	}
	for _, entry := range changes {
		if entry.drop {
			if err := os.Remove(entry.path); err != nil && !os.IsNotExist(err) {
				rollback()
				return PatchResult{}, err
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(entry.path), 0o755); err != nil {
				rollback()
				return PatchResult{}, err
			}
			if err := writeAtomic(entry.path, []byte(entry.after), 0o644); err != nil {
				rollback()
				return PatchResult{}, err
			}
		}
		committed = append(committed, entry)
		w.readLedger.add(entry.path)
	}
	result := PatchResult{Files: make([]string, 0, len(changes))}
	for _, entry := range changes {
		result.Files = append(result.Files, relativeSlash(w.root, entry.path))
	}
	result.Summary = fmt.Sprintf("Applied patch to %d file(s): %s.", len(result.Files), strings.Join(result.Files, ", "))
	return result, nil
}

func parseUnifiedPatch(text string) ([]unifiedPatch, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var result []unifiedPatch
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "--- ") {
			i++
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
			return nil, fmt.Errorf("missing +++ header after %q", lines[i])
		}
		patch := unifiedPatch{oldName: patchName(strings.TrimPrefix(lines[i], "--- ")), newName: patchName(strings.TrimPrefix(lines[i+1], "+++ "))}
		i += 2
		for i < len(lines) && !strings.HasPrefix(lines[i], "--- ") {
			match := hunkHeader.FindStringSubmatch(lines[i])
			if len(match) == 0 {
				i++
				continue
			}
			start, _ := strconv.Atoi(match[1])
			hunk := patchHunk{oldStart: start}
			i++
			for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") && !strings.HasPrefix(lines[i], "--- ") {
				line := lines[i]
				if line == `\ No newline at end of file` {
					i++
					continue
				}
				if line == "" {
					// An empty line outside a hunk body is harmless. Inside a
					// unified hunk, an empty content line carries a prefix.
					i++
					continue
				}
				if !strings.ContainsRune(" +-", rune(line[0])) {
					break
				}
				hunk.lines = append(hunk.lines, line)
				i++
			}
			patch.hunks = append(patch.hunks, hunk)
		}
		result = append(result, patch)
	}
	return result, nil
}

func applyHunks(content string, hunks []patchHunk) (string, error) {
	hadNewline := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimSuffix(content, "\n")
	source := []string{}
	if trimmed != "" {
		source = strings.Split(trimmed, "\n")
	}
	output := make([]string, 0, len(source)+16)
	cursor := 0
	for _, hunk := range hunks {
		start := max(0, hunk.oldStart-1)
		if start < cursor || start > len(source) {
			return "", fmt.Errorf("invalid hunk start %d", hunk.oldStart)
		}
		output = append(output, source[cursor:start]...)
		cursor = start
		for _, line := range hunk.lines {
			prefix, value := line[0], line[1:]
			switch prefix {
			case ' ':
				if cursor >= len(source) || source[cursor] != value {
					return "", fmt.Errorf("context did not match at line %d", cursor+1)
				}
				output = append(output, value)
				cursor++
			case '-':
				if cursor >= len(source) || source[cursor] != value {
					return "", fmt.Errorf("deletion did not match at line %d", cursor+1)
				}
				cursor++
			case '+':
				output = append(output, value)
			}
		}
	}
	output = append(output, source[cursor:]...)
	result := strings.Join(output, "\n")
	if hadNewline || len(hunks) > 0 {
		result += "\n"
	}
	return result, nil
}

func patchName(value string) string {
	value = strings.TrimSpace(strings.SplitN(value, "\t", 2)[0])
	return strings.SplitN(value, " ", 2)[0]
}
