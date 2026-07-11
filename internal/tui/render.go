package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

type palette struct {
	brand, brandSoft, text, muted, faint, border, surface, surface2, warning, danger, add, remove lipgloss.Style
}

func newPalette() palette {
	return palette{
		brand:     lipgloss.NewStyle().Foreground(lipgloss.Color("#43D17D")).Bold(true),
		brandSoft: lipgloss.NewStyle().Foreground(lipgloss.Color("#91E7B4")),
		text:      lipgloss.NewStyle().Foreground(lipgloss.Color("#E7F0EB")),
		muted:     lipgloss.NewStyle().Foreground(lipgloss.Color("#8A9B92")),
		faint:     lipgloss.NewStyle().Foreground(lipgloss.Color("#617068")),
		border:    lipgloss.NewStyle().Foreground(lipgloss.Color("#314239")),
		surface:   lipgloss.NewStyle().Foreground(lipgloss.Color("#DDE8E1")).Background(lipgloss.Color("#121A16")),
		surface2:  lipgloss.NewStyle().Foreground(lipgloss.Color("#DDE8E1")).Background(lipgloss.Color("#19231E")),
		warning:   lipgloss.NewStyle().Foreground(lipgloss.Color("#E7B65D")),
		danger:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F07878")),
		add:       lipgloss.NewStyle().Foreground(lipgloss.Color("#6CDE98")),
		remove:    lipgloss.NewStyle().Foreground(lipgloss.Color("#E67A83")),
	}
}

var inlineCodeRE = regexp.MustCompile("`([^`]+)`")

func RenderMarkdown(value string, width int, rtlMode string, colors palette) string {
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
			output = append(output, renderTable(block, width, colors)...)
			continue
		}
		prefix, style := "", colors.text
		plain := source
		switch {
		case strings.HasPrefix(trimmed, "### "):
			plain, style = strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")), colors.brandSoft.Bold(true)
		case strings.HasPrefix(trimmed, "## "):
			plain, style = strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")), colors.brandSoft.Bold(true)
		case strings.HasPrefix(trimmed, "# "):
			plain, style = strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")), colors.brand.Bold(true)
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			prefix, plain = colors.brand.Render("• "), strings.TrimSpace(trimmed[2:])
		}
		plain = strings.ReplaceAll(plain, "**", "")
		wrapped := wrapPlain(plain, max(10, width-runewidth.StringWidth(prefix)))
		for i, line := range wrapped {
			line = RenderRTL(line, rtlMode)
			line = renderInline(line, colors)
			if i == 0 {
				output = append(output, prefix+style.Render(line))
			} else {
				output = append(output, strings.Repeat(" ", runewidth.StringWidth(prefix))+style.Render(line))
			}
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
func renderTable(block []string, width int, colors palette) []string {
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
			if w := runewidth.StringWidth(cells[i]); w > widths[i] {
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
	pad := func(value string, w int) string {
		value = oneLine(value, w)
		return value + strings.Repeat(" ", max(0, w-runewidth.StringWidth(value)))
	}
	renderRow := func(cells []string, style lipgloss.Style) string {
		parts := make([]string, columns)
		for i := 0; i < columns; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts[i] = style.Render(pad(cell, widths[i]))
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
	for _, row := range rows {
		output = append(output, renderRow(row, colors.text))
	}
	return output
}

func renderInline(value string, colors palette) string {
	indexes := inlineCodeRE.FindAllStringSubmatchIndex(value, -1)
	if len(indexes) == 0 {
		return value
	}
	var result strings.Builder
	last := 0
	for _, match := range indexes {
		result.WriteString(value[last:match[0]])
		result.WriteString(colors.brandSoft.Render(value[match[2]:match[3]]))
		last = match[1]
	}
	result.WriteString(value[last:])
	return result.String()
}

func wrapPlain(value string, width int) []string {
	if width <= 0 || runewidth.StringWidth(value) <= width {
		return []string{value}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := ""
	for _, word := range words {
		if runewidth.StringWidth(word) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			for runewidth.StringWidth(word) > width {
				cut := runePrefix(word, width)
				lines = append(lines, cut)
				word = strings.TrimPrefix(word, cut)
			}
		}
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if runewidth.StringWidth(candidate) > width {
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

func runePrefix(value string, width int) string {
	var result strings.Builder
	used := 0
	for _, char := range value {
		charWidth := runewidth.RuneWidth(char)
		if used+charWidth > width && result.Len() > 0 {
			break
		}
		result.WriteRune(char)
		used += charWidth
	}
	return result.String()
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
