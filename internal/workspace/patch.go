package workspace

import (
	"context"
	"errors"
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
	newStart int
	lines    []string
	// newFinalNewline records whether the NEW-side content this hunk produces
	// ends with a newline. It is false only when a "\ No newline at end of file"
	// marker follows the hunk's last new-side (' ' or '+') line. Used to decide
	// the file's terminal newline when the last hunk rewrites through EOF (C-5).
	newFinalNewline bool
}

// hunkHeader captures the four fields of an "@@ -a,b +c,d @@" header: old start,
// old line count, new start, new line count. The counts are optional (a lone
// "@@ -a +c @@" means a count of 1) and bound how many body lines the hunk has —
// which is what lets a deleted "-- comment" line (rendered "--- comment") be
// read as body rather than mistaken for the next file header (C-1).
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

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
		mode          os.FileMode
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
		mode := os.FileMode(0o644)
		if existed {
			info, statErr := os.Stat(target)
			if statErr != nil {
				return PatchResult{}, statErr
			}
			mode = info.Mode().Perm()
		}
		before := strings.ReplaceAll(string(data), "\r\n", "\n")
		after, err := applyHunks(before, patch.hunks)
		if err != nil {
			return PatchResult{}, fmt.Errorf("patch %s: %w", name, err)
		}
		// Re-encode to CRLF only when it is the file's DOMINANT ending, so a
		// mostly-LF file with a stray CRLF is not flipped wholesale to CRLF (C-2).
		if dominantCRLF(string(data)) {
			after = strings.ReplaceAll(after, "\n", "\r\n")
		}
		changes = append(changes, change{path: target, before: string(data), after: after, existed: existed, drop: patch.newName == "/dev/null", mode: mode})
	}
	paths := make([]string, len(changes))
	for i := range changes {
		paths[i] = changes[i].path
	}
	if err := w.checkpoint(ctx, "before applying patch", paths); err != nil {
		return PatchResult{}, err
	}
	var committed []change
	rollback := func() error {
		var rollbackErrors []error
		for i := len(committed) - 1; i >= 0; i-- {
			entry := committed[i]
			if entry.existed {
				if err := writeAtomic(entry.path, []byte(entry.before), entry.mode); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %s: %w", entry.path, err))
				}
			} else {
				if err := os.Remove(entry.path); err != nil && !os.IsNotExist(err) {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %s: %w", entry.path, err))
				}
			}
		}
		return errors.Join(rollbackErrors...)
	}
	fail := func(operationErr error) (PatchResult, error) {
		if rollbackErr := rollback(); rollbackErr != nil {
			return PatchResult{}, errors.Join(operationErr, fmt.Errorf("patch rollback incomplete: %w", rollbackErr))
		}
		return PatchResult{}, operationErr
	}
	for _, entry := range changes {
		if entry.drop {
			if err := os.Remove(entry.path); err != nil && !os.IsNotExist(err) {
				return fail(err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(entry.path), 0o755); err != nil {
				return fail(err)
			}
			if err := writeAtomic(entry.path, []byte(entry.after), entry.mode); err != nil {
				return fail(err)
			}
		}
		committed = append(committed, entry)
		w.readLedger.add(entry.path)
	}
	result := PatchResult{Files: make([]string, 0, len(changes))}
	for _, entry := range changes {
		result.Files = append(result.Files, relativeSlash(w.root, entry.path))
		if entry.existed {
			if warning := oversizeChangeWarning(int64(len(entry.before)), false); warning != "" {
				result.Notes = append(result.Notes, warning)
			}
		}
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
			newStart, _ := strconv.Atoi(match[3])
			oldRemaining := hunkLineCount(match[2])
			newRemaining := hunkLineCount(match[4])
			hunk := patchHunk{oldStart: start, newStart: newStart, newFinalNewline: true}
			i++
			// Consume exactly the counted body lines. Bounding by the header's line
			// counts — not by the first "--- "/"@@ " prefix — is what lets a deleted
			// "-- comment" line (rendered "--- comment") and an added "++ x" line
			// (rendered "+++ x") be read as body instead of a file header (C-1).
			lastNewSide := false
			for i < len(lines) && (oldRemaining > 0 || newRemaining > 0) {
				line := lines[i]
				if line == `\ No newline at end of file` {
					if lastNewSide {
						hunk.newFinalNewline = false
					}
					i++
					continue
				}
				prefix := byte(' ')
				if line != "" {
					prefix = line[0]
				}
				if line != "" && prefix != ' ' && prefix != '-' && prefix != '+' {
					break // not a body line: the hunk ended (counts over-specified)
				}
				// An empty line is an empty context line whose leading space was
				// dropped; normalize it so counts and applyHunks stay aligned.
				body := line
				if body == "" {
					body = " "
				}
				hunk.lines = append(hunk.lines, body)
				switch prefix {
				case ' ':
					oldRemaining--
					newRemaining--
					lastNewSide = true
				case '-':
					oldRemaining--
					lastNewSide = false
				case '+':
					newRemaining--
					lastNewSide = true
				}
				i++
			}
			// The new-side "\ No newline at end of file" marker sits AFTER the last
			// counted body line, so the loop above exits before reaching it; honor a
			// trailing one when the hunk's last body line was new-side.
			if i < len(lines) && lines[i] == `\ No newline at end of file` {
				if lastNewSide {
					hunk.newFinalNewline = false
				}
				i++
			}
			if oldRemaining != 0 || newRemaining != 0 {
				return nil, fmt.Errorf("hunk at old line %d/new line %d ended before declared counts were satisfied (old remaining %d, new remaining %d)", start, newStart, oldRemaining, newRemaining)
			}
			patch.hunks = append(patch.hunks, hunk)
		}
		if len(patch.hunks) == 0 {
			return nil, fmt.Errorf("patch for %q contains no hunks", patch.newName)
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
	// Terminal newline: when the final hunk rewrites through EOF, the patch's own
	// no-newline marker governs (a well-formed diff of a file lacking a trailing
	// newline carries "\ No newline at end of file"); otherwise the file's
	// original state is preserved. The former `len(hunks) > 0` appended a newline
	// unconditionally, so a file WITHOUT one gained one on every patch (C-5).
	addNewline := hadNewline
	if len(hunks) > 0 && cursor >= len(source) {
		addNewline = hunks[len(hunks)-1].newFinalNewline
	}
	if len(output) > 0 && addNewline {
		result += "\n"
	}
	return result, nil
}

// hunkLineCount parses an "@@" header's optional line-count field. A missing
// count (a lone "@@ -a +c @@") means exactly one line, per unified-diff rules; a
// present count (including 0, for a creation or deletion side) is used verbatim.
func hunkLineCount(field string) int {
	if field == "" {
		return 1
	}
	n, _ := strconv.Atoi(field)
	return n
}

func patchName(value string) string {
	value = strings.TrimSpace(strings.SplitN(value, "\t", 2)[0])
	return strings.SplitN(value, " ", 2)[0]
}
