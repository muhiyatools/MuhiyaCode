package contract

import "testing"

// TestFullTokensGrouping (013 T015, contract display-formats §1): every
// user-visible token figure goes through this, so the boundaries matter.
func TestFullTokensGrouping(t *testing.T) {
	for _, test := range []struct {
		value int
		want  string
	}{
		{0, "0"},
		{7, "7"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{999999, "999,999"},
		{1000000, "1,000,000"},
		{41382, "41,382"},
		{2147483647, "2,147,483,647"},
	} {
		if got := FullTokens(test.value); got != test.want {
			t.Errorf("FullTokens(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}

// Nothing should ever display a negative token count, but a formatter that
// mangles one would turn a bookkeeping bug into a nonsense number on screen.
func TestFullTokensHandlesNegatives(t *testing.T) {
	if got := FullTokens(-1234); got != "-1,234" {
		t.Errorf("FullTokens(-1234) = %q, want %q", got, "-1,234")
	}
}

// The abbreviation FullTokens replaced hid real magnitude — this pins the
// difference that motivated the change (FR-020).
func TestFullTokensIsNotAbbreviated(t *testing.T) {
	if got := FullTokens(1_249_999); got != "1,249,999" {
		t.Fatalf("got %q, want the exact count", got)
	}
}
