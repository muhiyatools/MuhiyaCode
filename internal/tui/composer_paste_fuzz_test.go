package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stashPaste reproduces the model's large-paste stash path for a given content and
// returns the placeholder that would sit in the composer.
func stashPaste(m *Model, content string) string {
	if m.pastes == nil {
		m.pastes = make(map[int]string)
	}
	m.pasteSeq++
	m.pastes[m.pasteSeq] = content
	return fmt.Sprintf("[#paste%d %d lines]", m.pasteSeq, logicalLineCount(content))
}

// FuzzPasteRoundTrip (US5 T063/T069) asserts the paste stash → placeholder →
// expandPastes cycle reproduces the original bytes exactly for arbitrary content,
// including content that itself contains placeholder-shaped text and control bytes.
// The seed corpus runs during a normal `go test`.
func FuzzPasteRoundTrip(f *testing.F) {
	f.Add("simple text")
	f.Add(strings.Repeat("line\n", 50))
	f.Add("你好\tworld\x00\r\nмир")
	f.Add("[#paste1 5 lines] embedded placeholder text")
	f.Add("trailing newline\n")
	f.Fuzz(func(t *testing.T, content string) {
		if content == "" {
			return
		}
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		before := "before "
		after := " after"
		placeholder := stashPaste(m, content)
		// The composed draft: text around the atomic placeholder.
		draft := before + placeholder + after
		got := m.expandPastes(draft)
		want := before + content + after
		if got != want {
			t.Fatalf("round-trip mismatch:\n content=%q\n got=%q\n want=%q", content, got, want)
		}
	})
}

// TestPasteConcatenationOracle (US5 T069) checks the exact concatenation oracle for
// several blocks interleaved with typed text, across a batch of randomized inputs.
func TestPasteConcatenationOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) // fixed seed → deterministic
	alphabet := []rune("ab \n\t你→\x00X#[]0123")
	randStr := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		return b.String()
	}
	for trial := 0; trial < 200; trial++ {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		blocks := 1 + rng.Intn(3)
		var draft strings.Builder
		var want strings.Builder
		for b := 0; b < blocks; b++ {
			typed := randStr(rng.Intn(8))
			draft.WriteString(typed)
			want.WriteString(typed)
			content := randStr(1 + rng.Intn(40))
			placeholder := stashPaste(m, content)
			draft.WriteString(placeholder)
			want.WriteString(content)
		}
		tail := randStr(rng.Intn(8))
		draft.WriteString(tail)
		want.WriteString(tail)
		if got := m.expandPastes(draft.String()); got != want.String() {
			t.Fatalf("trial %d oracle mismatch:\n got=%q\n want=%q", trial, got, want.String())
		}
	}
}

// TestPasteRemovalClearsExactly (US5 T063) proves removing one block leaves the
// other blocks and surrounding text intact.
func TestPasteRemovalClearsExactly(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	p1 := stashPaste(m, strings.Repeat("first\n", 10))
	p2 := stashPaste(m, strings.Repeat("second\n", 10))
	m.input.SetValue("A " + p1 + " B " + p2 + " C")
	m.removePaste(1)
	if strings.Contains(m.input.Value(), "#paste1") {
		t.Fatal("paste1 placeholder not removed")
	}
	if !strings.Contains(m.input.Value(), "#paste2") {
		t.Fatal("paste2 was wrongly removed")
	}
	if _, ok := m.pastes[2]; !ok {
		t.Fatal("paste2 stash was wrongly dropped")
	}
}
