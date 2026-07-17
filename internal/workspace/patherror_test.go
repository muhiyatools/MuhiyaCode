package workspace

import (
	"os"
	"strings"
	"testing"
)

func TestFriendlyPathError(t *testing.T) {
	// A real not-exist error (whatever the OS spells it — GetFileAttributesEx on
	// Windows, ENOENT on Unix) must become a plain-language message with a hint.
	_, statErr := os.Stat(`Z:\definitely\not\here\nope.go`)
	if statErr == nil {
		t.Skip("unexpected: the bogus path exists")
	}
	got := friendlyPathError(statErr, "nope.go").Error()
	if !strings.Contains(got, "path does not exist: nope.go") || !strings.Contains(got, "glob") {
		t.Fatalf("not-exist error not made friendly: %q", got)
	}
	// The opaque syscall name must be gone.
	if strings.Contains(got, "GetFileAttributesEx") {
		t.Fatalf("raw OS error leaked through: %q", got)
	}
	// An unrecognized error passes through unchanged.
	passthrough := friendlyPathError(errSentinel, "x")
	if passthrough != errSentinel {
		t.Fatalf("unrecognized error was not passed through: %v", passthrough)
	}
	// nil stays nil.
	if friendlyPathError(nil, "x") != nil {
		t.Fatal("nil error must map to nil")
	}
}

var errSentinel = &sentinelError{}

type sentinelError struct{}

func (*sentinelError) Error() string { return "some unmapped failure" }
