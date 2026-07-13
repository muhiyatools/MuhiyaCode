package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTranscriptPageRequestZeroValueInert(t *testing.T) {
	var req TranscriptPageRequest
	if req.Direction != "" || req.Limit != 0 || req.ByteBudget != 0 || req.Generation != 0 {
		t.Fatalf("zero value should be inert: %+v", req)
	}
}

func TestTranscriptEventJSONRoundTrip(t *testing.T) {
	event := TranscriptEvent{ID: 7, SessionID: "s", Role: "user", Kind: "message", Content: "hi", CreatedAt: time.Unix(100, 0).UTC()}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var back TranscriptEvent
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != 7 || back.Content != "hi" || !back.CreatedAt.Equal(event.CreatedAt) {
		t.Fatalf("round trip mismatch: %+v", back)
	}
}

func TestTranscriptPageJSONRoundTrip(t *testing.T) {
	page := TranscriptPage{SessionID: "s", Generation: 3, Entries: []TranscriptEvent{{ID: 1, Content: "a"}}, HasOlder: true, RawBytes: 1}
	data, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var back TranscriptPage
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Generation != 3 || len(back.Entries) != 1 || !back.HasOlder {
		t.Fatalf("page round trip mismatch: %+v", back)
	}
}
