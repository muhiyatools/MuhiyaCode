package contract

import "strings"

// Diff-count parsing shared by the TUI's per-tool rows and the engine's
// session-cumulative lines-changed accounting (feature 008 UD-6/T023). One
// implementation so the two displays can never drift.

// diffMarker separates a tool's textual output from its embedded unified diff
// (the workspace edit/patch tools emit this exact marker).
const diffMarker = "\n--- diff ---\n"

// DiffLines returns the embedded diff's lines, or nil when output carries none.
func DiffLines(output string) []string {
	index := strings.Index(output, diffMarker)
	if index < 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(output[index+len(diffMarker):]), "\n")
}

// DiffCounts counts added/removed lines in an embedded diff payload, excluding
// the +++/--- file headers. ok is false when there is no diff to count.
func DiffCounts(output string) (add, remove int, ok bool) {
	diff := DiffLines(output)
	if len(diff) == 0 {
		return 0, 0, false
	}
	for _, line := range diff {
		switch {
		// File headers are always space-delimited ("+++ b/x", "--- a/x").
		// Requiring the space keeps added code that itself starts with "++"
		// (e.g. "+++counter;" for an added "++counter;") counted as a real
		// change instead of being skipped as a header.
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			continue
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			remove++
		}
	}
	return add, remove, true
}
