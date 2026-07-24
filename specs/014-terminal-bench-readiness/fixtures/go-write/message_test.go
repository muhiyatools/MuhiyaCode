package message

import "testing"

func TestGreeting(t *testing.T) {
	if got, want := Greeting(), "hello"; got != want {
		t.Fatalf("Greeting() = %q, want %q", got, want)
	}
}
