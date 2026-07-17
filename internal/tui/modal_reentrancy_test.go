package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

func twoChoices() []contract.QuestionChoice {
	return []contract.QuestionChoice{{Label: "yes", Recommended: true}, {Label: "no"}}
}

// TestModalQueuePreservesReplyChannels (T023) locks in the re-entrancy invariant:
// when two bridge-driven reply modals stack (the second queued behind the first),
// answering the first surfaces the second and BOTH reply channels receive a value.
// A lost reply would park the engine goroutine in bridge.request() forever.
func TestModalQueuePreservesReplyChannels(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})

	replyA := make(chan int, 1)
	replyC := make(chan int, 1)
	m.enqueueModal(modalRequest{title: "A", message: "first", choices: twoChoices(), reply: replyA})
	m.enqueueModal(modalRequest{title: "C", message: "second", choices: twoChoices(), reply: replyC})

	if m.modal == nil || m.modal.title != "A" {
		t.Fatalf("first reply modal did not show: %+v", m.modal)
	}

	// Answer A (Enter selects the recommended choice, index 0).
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case v := <-replyA:
		if v != 0 {
			t.Fatalf("replyA = %d, want 0", v)
		}
	default:
		t.Fatal("answering A did not deliver its reply — engine would park forever")
	}

	// C must now be the active modal (dequeued by closeModal).
	if m.modal == nil || m.modal.title != "C" {
		t.Fatalf("queued reply modal C did not surface after A closed: %+v", m.modal)
	}

	// Answer C.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case <-replyC:
	default:
		t.Fatal("answering C did not deliver its reply")
	}
	if m.modal != nil {
		t.Fatalf("a modal remained open after both were answered: %+v", m.modal)
	}
}

// TestChoiceModalDoesNotClobberPendingReply (T023) pins the protection that a
// non-reply modal (openChoice) refuses to replace a showing reply modal, so a
// callback that tries to open one cannot orphan the reply channel.
func TestChoiceModalDoesNotClobberPendingReply(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})

	reply := make(chan int, 1)
	m.enqueueModal(modalRequest{title: "Permission required", message: "allow?", choices: twoChoices(), reply: reply})

	// A stray attempt to open a plain choice modal must be refused, leaving the
	// reply modal intact.
	m.openChoice("Intruder", "should not show", twoChoices(), func(int) tea.Cmd { return nil })
	if m.modal == nil || m.modal.reply == nil {
		t.Fatalf("reply modal was clobbered by a choice modal: %+v", m.modal)
	}

	// The reply modal still answers cleanly.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case <-reply:
	default:
		t.Fatal("protected reply modal failed to deliver its reply")
	}
}
