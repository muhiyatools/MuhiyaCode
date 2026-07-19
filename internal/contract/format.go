package contract

import (
	"fmt"
	"strings"
	"unicode"
)

// HumanTokens renders a token count compactly (1.2k / 3.4m) for notices and
// display lines shared by the orchestrator's compaction notices and the TUI's
// usage/context rendering (feature 010 T036: the two byte-identical
// implementations — orchestrator.humanTokens and tui.formatTokens — merged
// into this one foundation-layer helper so a future format change can only
// happen in one place).
func HumanTokens(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

// TitleWords uppercases the first letter of every word, replicating the exact
// word-boundary rule of the deprecated strings.Title (a rune following a
// non-letter starts a word) so the four display call sites that used it keep
// byte-identical output without the deprecated API. The equality is pinned by
// TestTitleWordsMatchesLegacyStringsTitle. Display-label helper only — not a
// linguistically correct title-caser (neither was strings.Title).
func TitleWords(value string) string {
	previous := ' '
	return strings.Map(func(r rune) rune {
		wordStart := !isTitleWordLetter(previous)
		previous = r
		if wordStart {
			return unicode.ToTitle(r)
		}
		return r
	}, value)
}

// isTitleWordLetter mirrors strings.Title's isSeparator complement: letters,
// digits, and underscore continue a word; everything else separates.
func isTitleWordLetter(r rune) bool {
	if r == '_' {
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// TruncateEllipsis is the ONE rune-safe truncation primitive (feature 010
// T036): it absorbs orchestrator's former truncateEllipsis/truncate and
// workspace's former byte-unsafe truncateLine (which sliced value[:limit] by
// byte offset — capable of splitting a multi-byte UTF-8 rune in half on
// non-ASCII input). Cutting on runes here fixes that class of corruption for
// every caller at once. Returns value unchanged if it already fits; appends a
// single "…" when it does not.
func TruncateEllipsis(value string, maxChars int) string {
	runes := []rune(value)
	if maxChars <= 0 || len(runes) <= maxChars {
		return value
	}
	if maxChars == 1 {
		return "…"
	}
	return string(runes[:maxChars-1]) + "…"
}

// Digest collapses value's whitespace to single spaces (folding multi-line
// text into one line) then applies TruncateEllipsis — the shared wrapper
// every "show a compact one-line summary" call site uses (feature 010 T036:
// absorbs orchestrator's former digest and oneLineGoal).
func Digest(value string, maxChars int) string {
	return TruncateEllipsis(strings.Join(strings.Fields(value), " "), maxChars)
}
