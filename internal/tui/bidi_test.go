package tui

import (
	"strings"
	"testing"
)

// TestMixedContentKeepsTechnicalTokens (006 T022/US3, FR-010): in a mixed line,
// LTR technical tokens (paths, URLs, inline code, bracketed calls, numbers) stay
// intact and correctly positioned inside surrounding Arabic.
func TestMixedContentKeepsTechnicalTokens(t *testing.T) {
	cases := []struct{ in, token string }{
		{corpusMixedFile, "main.go"},
		{corpusMixedPath, "internal/tui/rtl.go"},
		{corpusMixedURL, "https://muhiya.com"},
		{corpusMixedNum, "42"},
		{"استخدم func(x) هنا", "func(x)"},
		{"شغل go test ./... الان", "go test ./..."},
	}
	for _, c := range cases {
		if v := renderForDisplay(c.in, "visual", "auto").Visual; !strings.Contains(v, c.token) {
			t.Errorf("token %q fragmented in %q: %q", c.token, c.in, v)
		}
	}
}

// TestSegmentRuns (006 T023): the segmenter groups Arabic vs non-Arabic, gluing
// neutral spaces to the current run.
func TestSegmentRuns(t *testing.T) {
	runs := segmentRuns("مرحبا func(x) هنا")
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d: %+v", len(runs), runs)
	}
	if !runs[0].rtl || runs[1].rtl || !runs[2].rtl {
		t.Fatalf("run directions wrong: %+v", runs)
	}
	if strings.TrimSpace(runs[1].text) != "func(x)" {
		t.Fatalf("technical token not kept as one LTR run: %q", runs[1].text)
	}
}

// TestPureLTRSingleRun (006 FR-011): pure-LTR content is one LTR run, unchanged.
func TestPureLTRSingleRun(t *testing.T) {
	runs := segmentRuns(corpusLTROnly)
	if len(runs) != 1 || runs[0].rtl || runs[0].text != corpusLTROnly {
		t.Fatalf("pure-LTR should be one unchanged LTR run: %+v", runs)
	}
}
