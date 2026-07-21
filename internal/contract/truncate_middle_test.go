package contract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// E-4: the "N characters trimmed" marker must report the ACTUAL number removed
// (len - kept), not len - maxChars, which understated it by the marker's own
// length.
func TestTruncateMiddleReportsActualTrimmedCount(t *testing.T) {
	input := strings.Repeat("a", 1000)
	out := TruncateMiddle(input, 100)
	m := regexp.MustCompile(`\[(\d+) characters trimmed`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no trim marker in output: %q", out)
	}
	stated, _ := strconv.Atoi(m[1])
	marker := fmt.Sprintf("\n…[%d characters trimmed from the middle]…\n", stated)
	kept := len([]rune(out)) - len([]rune(marker))
	actualRemoved := len([]rune(input)) - kept
	if stated != actualRemoved {
		t.Fatalf("marker says %d trimmed but %d were actually removed", stated, actualRemoved)
	}
}
