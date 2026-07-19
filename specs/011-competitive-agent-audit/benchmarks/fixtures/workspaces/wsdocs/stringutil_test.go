package wsdocs

import "testing"

func TestReverse(t *testing.T) {
	if got := Reverse("abc"); got != "cba" {
		t.Fatalf("Reverse: got %q, want %q", got, "cba")
	}
}

func TestCaptialize(t *testing.T) {
	if got := Captialize("go"); got != "Go" {
		t.Fatalf("Captialize: got %q, want %q", got, "Go")
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 4); got != "hell..." {
		t.Fatalf("Truncate: got %q, want %q", got, "hell...")
	}
}
