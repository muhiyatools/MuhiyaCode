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

// HumanTokens renders a token count compactly (1.2k / 3.4m). It survives for
// non-display consumers only — user-visible figures use FullTokens (013 FR-020).
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

// TruncateMiddle bounds value while preserving BOTH ends, dropping from the
// middle. It exists because a subagent report's most load-bearing content is at
// its END: the verification section and the machine-read STATUS line. Tail-
// cutting truncation silently deleted exactly those on any long report, so the
// caller read "no verification shown" and re-dispatched work that was already
// verified — the trust protocol failing precisely when a run had been most
// productive.
//
// Head keeps 60% of the budget and tail 40%: the head carries what the run did,
// the tail carries whether it worked. The elision is explicit, never silent.
func TruncateMiddle(value string, maxChars int) string {
	runes := []rune(value)
	if maxChars <= 0 || len(runes) <= maxChars {
		return value
	}
	// Below this the marker dominates and a plain tail-cut is more useful.
	const minSplit = 40
	if maxChars < minSplit {
		return TruncateEllipsis(value, maxChars)
	}
	dropped := len(runes) - maxChars
	marker := fmt.Sprintf("\n…[%d characters trimmed from the middle]…\n", dropped)
	budget := maxChars - len([]rune(marker))
	if budget < 2 {
		return TruncateEllipsis(value, maxChars)
	}
	// The output keeps only `budget` runes of content, so the real removed count is
	// len(runes)-budget — bigger than len(runes)-maxChars by the marker's own
	// length. The marker used to understate it by ~45 chars (E-4).
	marker = fmt.Sprintf("\n…[%d characters trimmed from the middle]…\n", len(runes)-budget)
	head := budget * 6 / 10
	return string(runes[:head]) + marker + string(runes[len(runes)-(budget-head):])
}

// Digest collapses value's whitespace to single spaces (folding multi-line
// text into one line) then applies TruncateEllipsis — the shared wrapper
// every "show a compact one-line summary" call site uses (feature 010 T036:
// absorbs orchestrator's former digest and oneLineGoal).
func Digest(value string, maxChars int) string {
	return TruncateEllipsis(strings.Join(strings.Fields(value), " "), maxChars)
}
