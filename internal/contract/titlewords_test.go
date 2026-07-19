package contract

import (
	"strings"
	"testing"
)

// TestTitleWordsMatchesLegacyStringsTitle pins TitleWords to the deprecated
// strings.Title it replaced: the four display call sites (permission labels,
// tool-chip names, skill names, model-role headings) must keep byte-identical
// output. The corpus covers every real input shape plus separator and
// non-ASCII edges.
func TestTitleWordsMatchesLegacyStringsTitle(t *testing.T) {
	corpus := []string{
		"", "edit", "run shell", "read_file", "edit file", "multi edit",
		"main", "subagent", "release-notes", "web search", "git status",
		"approve edit", "reject", "deepseek v4 pro", "minimax m3",
		"a", "A", "7zip", "über tool", "mémoire vive", "run  shell",
		"already Titled Words", "-leading", "trailing-", "x_y z",
	}
	for _, sample := range corpus {
		//lint:ignore SA1019 the deprecated function is the behavior contract this test pins against.
		want := strings.Title(sample)
		if got := TitleWords(sample); got != want {
			t.Fatalf("TitleWords(%q) = %q, strings.Title = %q", sample, got, want)
		}
	}
}
