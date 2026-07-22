package contract

import (
	"fmt"
	"strings"
	"unicode"
)

// FullTokens renders a token count in full, comma-grouped digits (1,234,567).
//
// It replaced the abbreviated form at every user-visible site (013 FR-020):
// "1.2m" hides up to 50,000 tokens behind a rounding, and users make cost and
// trust decisions from these numbers. Grouping is ASCII and locale-independent
// on purpose — a locale-varying separator would make golden tests and Arabic
// sessions non-deterministic for no reader benefit.
func FullTokens(value int) string {
	digits := fmt.Sprintf("%d", value)
	negative := strings.HasPrefix(digits, "-")
	if negative {
		digits = digits[1:]
	}
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if negative {
		return "-" + b.String()
	}
	return b.String()
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
