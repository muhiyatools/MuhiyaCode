package wsdocs

import (
	"unicode/utf8"
	"fmt"
)

// Truncate shortens s to at most maxLen runes, appending "..." when it cuts.
func Truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	r := []rune(s)
	return fmt.Sprintf("%s...", string(r[:maxLen]))
}
