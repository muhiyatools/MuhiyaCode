package tui

import "testing"

func TestResetStreamDraftRemovesAbandonedAttempt(t *testing.T) {
	model := &Model{}
	model.items = []item{{kind: "user", content: "hello"}}
	model.appendStream(streamMsg{text: "partial"})

	model.resetStreamDraft()
	model.appendStream(streamMsg{text: "complete"})

	if len(model.items) != 2 || model.items[1].kind != "assistant_draft" || model.items[1].content != "complete" {
		t.Fatalf("retry stream retained abandoned text: %+v", model.items)
	}
}
