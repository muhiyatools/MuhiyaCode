package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}

func TestDiv(t *testing.T) {
	if got := Div(10, 2); got != 5 {
		t.Fatalf("Div(10, 2) = %d, want 5", got)
	}
}
