package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestAsyncModalDoesNotClobberReplyModal (M2 regression) proves an async command
// result (e.g. /usage → openInfo, /sessions → openChoice) does not overwrite an
// open bridge permission/ask modal that carries a reply channel — clobbering it
// would discard the reply and hang the running task.
func TestAsyncModalDoesNotClobberReplyModal(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	reply := make(chan int, 1)
	m.modal = &modalState{title: "Permission required", message: "Allow edit?", choices: []contract.QuestionChoice{{Label: "Allow once"}, {Label: "Deny"}}, reply: reply}

	// Async info result lands while the permission prompt is up.
	m.openInfo("Account usage", "42 credits remaining")
	if m.modal == nil || m.modal.reply != reply {
		t.Fatal("openInfo clobbered the pending permission modal (would hang the task)")
	}
	// Async choice result too.
	m.openChoice("Sessions", "pick", []contract.QuestionChoice{{Label: "a"}, {Label: "b"}}, func(int) tea.Cmd { return nil })
	if m.modal == nil || m.modal.reply != reply {
		t.Fatal("openChoice clobbered the pending permission modal")
	}
	// The permission modal still resolves normally.
	m.closeModal(0)
	select {
	case got := <-reply:
		if got != 0 {
			t.Fatalf("reply = %d, want 0", got)
		}
	default:
		t.Fatal("permission modal did not deliver its reply")
	}
}

// TestAskEmptyChoicesDoesNotPanic (H1 regression) proves the bridge Ask callback
// degrades a choiceless question to a blank answer instead of indexing an empty
// slice, which previously panicked the event loop and crashed the app.
func TestAskEmptyChoicesDoesNotPanic(t *testing.T) {
	b := NewBridge()
	cb := b.Callbacks()
	// No program attached and no fallback: a normal question would resolve to -1,
	// but a choiceless one must be handled before request() is ever called.
	answers, err := cb.Ask(context.Background(), []contract.Question{
		{Question: "Proceed?", Choices: nil},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("want 1 answer, got %d", len(answers))
	}
	if answers[0].Index != -1 || answers[0].Choice.Label != "" {
		t.Fatalf("choiceless question should yield a blank answer, got %+v", answers[0])
	}
}

// TestAskMixedChoicesHandlesEmptySafely proves a batch with one valid and one
// choiceless question does not crash and answers both positionally.
func TestAskMixedChoicesHandlesEmptySafely(t *testing.T) {
	b := NewBridge()
	b.SetFallback(func(modalRequest) int { return 0 }) // "answer" the valid question
	cb := b.Callbacks()
	answers, err := cb.Ask(context.Background(), []contract.Question{
		{Question: "Pick", Choices: []contract.QuestionChoice{{Label: "A"}, {Label: "B"}}},
		{Question: "Broken", Choices: nil},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("want 2 answers, got %d", len(answers))
	}
	if answers[0].Choice.Label != "A" {
		t.Fatalf("first answer should be A, got %q", answers[0].Choice.Label)
	}
	if answers[1].Index != -1 {
		t.Fatalf("second (choiceless) answer should be blank, got %+v", answers[1])
	}
}

func TestAskCancellationDoesNotSelectRecommendedChoice(t *testing.T) {
	b := NewBridge()
	b.SetFallback(func(modalRequest) int { return -1 })
	_, err := b.Callbacks().Ask(context.Background(), []contract.Question{{
		Question: "Proceed?",
		Choices: []contract.QuestionChoice{
			{Label: "Proceed", Recommended: true},
			{Label: "Stop"},
		},
	}})
	if err == nil || err != ErrQuestionCancelled {
		t.Fatalf("cancelled question must fail closed, got %v", err)
	}
}
