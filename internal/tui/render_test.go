package tui

import (
	"regexp"
	"strings"
	"testing"
)

var sgrRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return sgrRE.ReplaceAllString(s, "") }

// TestInlineMarkdownEmphasis (003 T013/T015/SC-002) is the regression corpus for
// the bold bug: bold/italic/code/strike must render styled with no raw markers,
// and an unclosed marker (streaming) must render literally without a flash.
func TestInlineMarkdownEmphasis(t *testing.T) {
	colors := newPalette("dark")
	cases := []struct {
		name    string
		source  string
		wantTxt string   // visible text after stripping ANSI
		noRaw   []string // markers that must NOT appear in the visible text
	}{
		{"bold", "This is **bold** text", "This is bold text", []string{"**"}},
		{"italic star", "This is *italic* text", "This is italic text", []string{"*"}},
		{"italic underscore", "This is _italic_ text", "This is italic text", []string{"_"}},
		{"double underscore bold", "This is __strong__ text", "This is strong text", []string{"__"}},
		{"inline code", "Call `fmt.Println` now", "Call fmt.Println now", []string{"`"}},
		{"strike", "This is ~~gone~~ text", "This is gone text", []string{"~~"}},
		{"mixed", "**bold** and `code` and *it*", "bold and code and it", []string{"**", "`"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripANSI(RenderMarkdown(tc.source, 60, "off", "auto", colors))
			if !strings.Contains(got, tc.wantTxt) {
				t.Fatalf("visible text = %q, want to contain %q", got, tc.wantTxt)
			}
			for _, raw := range tc.noRaw {
				if strings.Contains(got, raw) {
					t.Fatalf("raw marker %q leaked into visible text: %q", raw, got)
				}
			}
		})
	}
}

// TestInlineMarkdownStreamingHalfToken (003 T013): an unclosed marker renders
// literally — the '**' stays visible rather than styling or vanishing.
func TestInlineMarkdownStreamingHalfToken(t *testing.T) {
	colors := newPalette("dark")
	got := stripANSI(RenderMarkdown("start of **bold that is not closed yet", 80, "off", "auto", colors))
	if !strings.Contains(got, "**bold that is not closed yet") {
		t.Fatalf("unclosed marker did not render literally: %q", got)
	}
}

// TestTableCellInlineMarkdown (003 T014): bold/code inside a table cell renders
// styled, never as raw markers.
func TestTableCellInlineMarkdown(t *testing.T) {
	colors := newPalette("dark")
	source := "| Feature | Note |\n|---------|------|\n| **Bold** | uses `code` |\n| plain | none |"
	got := stripANSI(RenderMarkdown(source, 80, "off", "auto", colors))
	if strings.Contains(got, "**") || strings.Contains(got, "`") {
		t.Fatalf("raw markers leaked inside table cells:\n%s", got)
	}
	if !strings.Contains(got, "Bold") || !strings.Contains(got, "code") {
		t.Fatalf("table cell content missing:\n%s", got)
	}
}

// TestTableColumnsAlignWithInlineMarkup (006) is the regression for the table
// misalignment bug: a non-last cell like **done** was padded by its RAW width (8)
// though it renders 4 wide, so the next column's separator drifted. The column
// separator must sit at the same visible offset on the header and every data row.
func TestTableColumnsAlignWithInlineMarkup(t *testing.T) {
	colors := newPalette("dark")
	// **done** in the FIRST column: its mispadding would shift the column-2 separator.
	source := "| Status | Name |\n|--------|------|\n| **done** | alpha |\n| ok | b |"
	out := RenderMarkdown(source, 60, "off", "auto", colors)
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected header + rule + 2 data rows, got %d:\n%s", len(lines), out)
	}
	sepCol := func(s string) int { return strings.Index(stripANSI(s), "│") }
	want := sepCol(lines[0]) // header row separator column
	if want < 0 {
		t.Fatalf("no column separator in header:\n%s", out)
	}
	for i, ln := range lines {
		if i == 1 || !strings.Contains(stripANSI(ln), "│") {
			continue // lines[1] is the ─┼─ rule
		}
		if got := sepCol(ln); got != want {
			t.Fatalf("row %d separator at col %d, want %d (columns misaligned):\n%s", i, got, want, out)
		}
	}
}

// TestNoColorHonored (003 T011): with NO_COLOR set, output carries no SGR color
// escapes (attributes only). We assert no truecolor foreground sequences appear.
func TestNoColorHonored(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	colors := newPalette("dark")
	out := RenderMarkdown("# Heading\n\n**bold** and normal", 60, "off", "auto", colors)
	if strings.Contains(out, "38;2;") {
		t.Fatalf("NO_COLOR output still contains truecolor foreground escapes:\n%q", out)
	}
}
