package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// palette is the semantic token set (visual-system.md §1). The flat field names
// map one-to-one to tokens: brand=accent.primary, brandSoft=accent.soft,
// text=text.primary, muted=text.muted, faint=text.faint, border=border.default,
// focus=border.focus, surface=bg.surface, surface2=bg.overlay,
// warning=status.warning, danger=status.error, info=status.info,
// add=diff.add, remove=diff.remove, meta=diff.meta. Colors resolve once at
// startup from the dark/light table below; NO_COLOR drops color for attributes.
//
// Contrast rule: every foreground token must stay clearly legible on both the
// dark and light theme it targets. No token may rely on the ANSI Faint
// attribute for hierarchy in color mode — many terminal themes render Faint
// close to the background, which made muted/faint text effectively invisible.
// Faint is applied ONLY under NO_COLOR, where it is the sole hierarchy channel.
type palette struct {
	brand, brandSoft, text, muted, faint, border, focus lipgloss.Style
	surface, surface2                                   lipgloss.Style
	warning, danger, info                               lipgloss.Style
	add, remove, meta                                   lipgloss.Style
	// selection is the transcript text-selection highlight (US4 T058). Reverse
	// video is used deliberately: it is visible in every theme and under NO_COLOR,
	// so the selection never depends on color alone (visual contract §3).
	selection lipgloss.Style
}

// resolvePaletteDark maps Settings.Theme to a background mode. "light" forces
// light; everything else (incl. "auto"/"") keeps the dark default. Explicit and
// deterministic — no terminal round-trip that could stall the alt-screen setup.
func resolvePaletteDark(theme string) bool {
	return strings.ToLower(strings.TrimSpace(theme)) != "light"
}

func newPalette(theme string) palette {
	dark := resolvePaletteDark(theme)
	noColor := os.Getenv("NO_COLOR") != ""
	// fg picks the dark or light foreground unless NO_COLOR, then applies mods
	// (Bold/Faint) which carry hierarchy when color is unavailable.
	fg := func(darkHex, lightHex string, mods ...func(lipgloss.Style) lipgloss.Style) lipgloss.Style {
		s := lipgloss.NewStyle()
		if !noColor {
			if dark {
				s = s.Foreground(lipgloss.Color(darkHex))
			} else {
				s = s.Foreground(lipgloss.Color(lightHex))
			}
		}
		for _, m := range mods {
			s = m(s)
		}
		return s
	}
	bold := func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) }
	// dim carries hierarchy ONLY when color is unavailable (NO_COLOR). In color
	// mode the hierarchy comes from the hex values themselves — the Faint
	// attribute is never combined with color because many terminal themes
	// render Faint text near-invisible.
	dim := func(s lipgloss.Style) lipgloss.Style {
		if noColor {
			return s.Faint(true)
		}
		return s
	}
	// band builds a surface style with fg+bg; NO_COLOR falls back to reverse so
	// the band stays visible without color.
	band := func(darkBG, lightBG string) lipgloss.Style {
		if noColor {
			return lipgloss.NewStyle().Reverse(true)
		}
		if dark {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("#DDE8E1")).Background(lipgloss.Color(darkBG))
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#1A241E")).Background(lipgloss.Color(lightBG))
	}
	return palette{
		brand:     fg("#43D17D", "#1F8A4C", bold),
		brandSoft: fg("#91E7B4", "#3FA36B"),
		text:      fg("#E7F0EB", "#1A241E"),
		muted:     fg("#C9D7CE", "#3F4E46", dim),
		faint:     fg("#A7BBAD", "#57685E", dim),
		border:    fg("#66816F", "#6F8F7D"),
		focus:     fg("#43D17D", "#1F8A4C", bold),
		surface:   band("#121A16", "#EAF2ED"),
		surface2:  band("#19231E", "#DFEAE3"),
		warning:   fg("#E7B65D", "#9A6B00"),
		danger:    fg("#F07878", "#B03030"),
		info:      fg("#7FB8E0", "#2E6FA3"),
		add:       fg("#6CDE98", "#1F8A4C"),
		remove:    fg("#E67A83", "#B03030"),
		meta:      fg("#E7B65D", "#9A6B00", dim),
		selection: lipgloss.NewStyle().Reverse(true),
	}
}

// glyphs is the single source for non-ASCII glyphs (visual-system.md §2). Two
// variants: unicode (default, width-1 verified) and ascii (MUHIYA_ASCII=1 or
// Settings.UI.BorderMode=ascii). No glyph literal should live outside this table.
type glyphs struct {
	brand      string
	markerOK   string
	markerFail string
	ruleH      string
	gutter     string
	bullet     string
	ellipsis   string
	spinner    []string
	border     lipgloss.Border
	ascii      bool
}

func newGlyphs(ascii bool) glyphs {
	if ascii {
		return glyphs{
			brand: "*", markerOK: "*", markerFail: "x", ruleH: "-", gutter: "|",
			bullet: ".", ellipsis: "...", spinner: []string{"-", "\\", "|", "/"},
			border: lipgloss.Border{Top: "-", Bottom: "-", Left: "|", Right: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+"},
			ascii:  true,
		}
	}
	return glyphs{
		brand: "◆", markerOK: "●", markerFail: "×", ruleH: "─", gutter: "▏",
		bullet: "·", ellipsis: "…", spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"},
		border: lipgloss.RoundedBorder(),
		ascii:  false,
	}
}

// asciiGlyphs reports whether the ASCII fallback should be used, from the env
// override or the UI border-mode setting.
func asciiGlyphs(borderMode string) bool {
	if os.Getenv("MUHIYA_ASCII") != "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(borderMode), "ascii")
}

func RenderMarkdown(value string, width int, rtlMode, rtlAlign string, colors palette) string {
	if width < 20 {
		width = 20
	}
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	var output []string
	inCode := false
	for index := 0; index < len(lines); index++ {
		source := lines[index]
		trimmed := strings.TrimSpace(source)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			for _, line := range wrapPlain(source, width-2) {
				output = append(output, colors.surface2.Padding(0, 1).Width(width).Render(line))
			}
			continue
		}
		// A markdown table starts at a | row whose next line is a |---|
		// separator. Collect the whole block and render it as an aligned
		// table instead of leaving raw pipes on screen.
		if isTableRow(trimmed) && index+1 < len(lines) && isTableSeparator(strings.TrimSpace(lines[index+1])) {
			block := []string{trimmed}
			for index+1 < len(lines) && isTableRow(strings.TrimSpace(lines[index+1])) {
				index++
				block = append(block, strings.TrimSpace(lines[index]))
			}
			output = append(output, renderTable(block, width, rtlMode, colors)...)
			continue
		}
		prefix, style := "", colors.text
		plain := source
		switch {
		case strings.HasPrefix(trimmed, "### "):
			plain, style = strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")), colors.text.Bold(true)
		case strings.HasPrefix(trimmed, "## "):
			plain, style = strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")), colors.brandSoft.Bold(true)
		case strings.HasPrefix(trimmed, "# "):
			plain, style = strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")), colors.brand.Bold(true)
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			prefix, plain = colors.brand.Render("• "), strings.TrimSpace(trimmed[2:])
		}
		wrapped := wrapPlain(plain, max(10, width-displayWidth(prefix)))
		for i, line := range wrapped {
			// Wrap-then-shape: the line is logical here; the display pass shapes,
			// reorders (RTL runs), and resolves alignment for this display line.
			dl := renderForDisplay(line, rtlMode, rtlAlign)
			// renderInline applies the base style to plain text and emphasis styles on
			// top; run it on the shaped visual so styling isn't a second wrapping pass.
			rendered := renderInline(dl.Visual, style, colors)
			indent := prefix
			if i > 0 {
				indent = strings.Repeat(" ", displayWidth(prefix))
			}
			// Right-align plain RTL paragraph lines to the content width; prefixed
			// lines (headings, bullets) keep their left marker.
			if prefix == "" && dl.Align == "right" {
				if pad := width - displayWidth(dl.Visual); pad > 0 {
					indent = strings.Repeat(" ", pad)
				}
			}
			output = append(output, indent+rendered)
		}
	}
	return strings.Join(output, "\n")
}

func isTableRow(line string) bool {
	return strings.HasPrefix(line, "|") && strings.Count(line, "|") >= 2
}

var tableSeparatorRE = regexp.MustCompile(`^\|(\s*:?-{2,}:?\s*\|)+$`)

func isTableSeparator(line string) bool {
	return tableSeparatorRE.MatchString(line)
}

func splitTableRow(line string) []string {
	line = strings.Trim(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// renderTable lays out a markdown table block with aligned columns and light
// borders, shrinking columns proportionally when the terminal is narrow so the
// table never overflows. block[0] is the header, block[1] the separator.
func renderTable(block []string, width int, rtlMode string, colors palette) []string {
	header := splitTableRow(block[0])
	var rows [][]string
	for _, line := range block[2:] {
		rows = append(rows, splitTableRow(line))
	}
	columns := len(header)
	if columns == 0 {
		return block
	}
	widths := make([]int, columns)
	measure := func(cells []string) {
		for i := 0; i < columns && i < len(cells); i++ {
			if w := displayWidth(cells[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}
	measure(header)
	for _, row := range rows {
		measure(row)
	}
	// Total = cells + " │ " separators + leading/trailing space.
	total := 2 + 3*(columns-1)
	for _, w := range widths {
		total += w
	}
	for total > width {
		// Shrink the widest column until the table fits (floor 6 cells).
		widest := 0
		for i := 1; i < columns; i++ {
			if widths[i] > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 6 {
			break
		}
		widths[widest]--
		total--
	}
	// Each cell gets the full inline pass (so **bold**/`code` inside a cell render
	// styled, not raw) then is padded to the column width. Padding MUST be measured
	// from the RENDERED width (lipgloss.Width strips ANSI and the emphasis markers
	// that renderInline consumed), not from the raw content width — otherwise a cell
	// like "**Done**" is padded as if 8 wide while it displays as 4, and every column
	// after it drifts out of alignment. That drift is the table-rendering bug.
	renderCell := func(cell string, w int, style lipgloss.Style) string {
		// Shape Arabic cell content (joined/RTL) before the inline pass; keep cells
		// left-aligned within their column.
		content := renderForDisplay(oneLine(cell, w), rtlMode, "left").Visual
		rendered := renderInline(content, style, colors)
		return rendered + strings.Repeat(" ", max(0, w-lipgloss.Width(rendered)))
	}
	renderRow := func(cells []string, style lipgloss.Style) string {
		parts := make([]string, columns)
		for i := 0; i < columns; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts[i] = renderCell(cell, widths[i], style)
		}
		return " " + strings.Join(parts, colors.border.Render(" │ "))
	}
	rule := make([]string, columns)
	for i := 0; i < columns; i++ {
		rule[i] = strings.Repeat("─", widths[i])
	}
	var output []string
	output = append(output, renderRow(header, colors.brandSoft.Bold(true)))
	output = append(output, " "+colors.border.Render(strings.Join(rule, "─┼─")))
	for index, row := range rows {
		base := colors.text
		if index%2 == 1 {
			base = colors.muted // zebra fg alternation, no row backgrounds
		}
		output = append(output, renderRow(row, base))
	}
	return output
}

// renderInline tokenizes one line of inline markdown and returns a fully-styled
// string: plain runs get the base style, emphasis spans (bold/italic/code/
// strike) get their own style on top. An unclosed or empty marker renders
// literally with no styling, so a half-arrived token mid-stream never flashes.
func renderInline(value string, base lipgloss.Style, colors palette) string {
	var out strings.Builder
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out.WriteString(base.Render(plain.String()))
			plain.Reset()
		}
	}
	i := 0
	for i < len(value) {
		if m := matchInlineMarker(value, i); m != nil {
			flush()
			switch m.kind {
			case "code":
				out.WriteString(colors.surface.Render(m.inner))
			case "bold":
				out.WriteString(base.Bold(true).Render(m.inner))
			case "italic":
				out.WriteString(base.Italic(true).Render(m.inner))
			case "strike":
				out.WriteString(base.Strikethrough(true).Render(m.inner))
			}
			i = m.end
			continue
		}
		plain.WriteByte(value[i])
		i++
	}
	flush()
	return out.String()
}

type inlineMarker struct {
	kind  string
	inner string
	end   int
}

// matchInlineMarker checks position i for an emphasis span. Markers are ASCII so
// byte slicing at their boundaries is UTF-8 safe. Longer markers are tried first
// (** before *) so bold isn't misparsed as two italics. A marker with no closer
// or empty inner is not a match — the caller then emits the char literally.
func matchInlineMarker(s string, i int) *inlineMarker {
	rest := s[i:]
	type mk struct{ tok, kind string }
	order := []mk{
		{"`", "code"},
		{"**", "bold"},
		{"__", "bold"},
		{"~~", "strike"},
		{"*", "italic"},
		{"_", "italic"},
	}
	for _, m := range order {
		if !strings.HasPrefix(rest, m.tok) {
			continue
		}
		after := rest[len(m.tok):]
		idx := strings.Index(after, m.tok)
		if idx <= 0 {
			continue
		}
		return &inlineMarker{kind: m.kind, inner: after[:idx], end: i + len(m.tok) + idx + len(m.tok)}
	}
	return nil
}

// wrapPlain word-wraps logical text to width, measuring by grapheme-cluster
// display width (uniseg) so vocalized Arabic is not over-measured, and never
// splitting a cluster (base+harakat stay together). Wrapping is on LOGICAL text;
// the RTL display pass shapes/reorders each wrapped line afterward (research R4).
func wrapPlain(value string, width int) []string {
	if width <= 0 || displayWidth(value) <= width {
		return []string{value}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := ""
	for _, word := range words {
		if displayWidth(word) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			for displayWidth(word) > width {
				cut := truncateToWidth(word, width)
				if cut == "" { // width too small for even one cluster
					break
				}
				lines = append(lines, cut)
				word = strings.TrimPrefix(word, cut)
			}
		}
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if displayWidth(candidate) > width {
			lines = append(lines, line)
			line = word
		} else {
			line = candidate
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func formatTokens(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func formatDuration(duration time.Duration) string {
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(duration.Minutes()), int(duration.Seconds())%60)
}
