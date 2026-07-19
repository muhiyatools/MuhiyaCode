package state

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 012 T013: the contextLinking config key round-trips and rejects
// unknown values (wiring-inventory row "Settings Field | contextLinking").
func TestApplyConfigContextLinking(t *testing.T) {
	settings := DefaultSettings()
	var secrets contract.Secrets
	for _, value := range []string{"off", "default", ""} {
		if err := SetConfig("contextLinking", value, &settings, &secrets); err != nil {
			t.Fatalf("contextLinking=%q rejected: %v", value, err)
		}
		if settings.ContextLinking != value {
			t.Fatalf("contextLinking=%q not applied, got %q", value, settings.ContextLinking)
		}
	}
	if err := SetConfig("contextLinking", "aggressive", &settings, &secrets); err == nil {
		t.Fatal("unknown contextLinking value must be rejected")
	}
}

// Feature 012 T009: agent context records round-trip byte-exactly through the
// agents/ sidecar store — including raw ReasoningDetails (json.RawMessage),
// whose bytes MUST survive untouched or MiniMax continuation replay breaks
// prefix identity (research R-F6).
func TestAgentRecordRoundTripPreservesBytes(t *testing.T) {
	db, err := Open(context.Background(), testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := &Sessions{DB: db}
	session, err := sessions.GetOrCreate(context.Background(), t.TempDir(), "t")
	if err != nil {
		t.Fatal(err)
	}
	type record struct {
		RunID      string             `json:"runId"`
		Transcript []contract.Message `json:"transcript"`
	}
	raw := json.RawMessage(`[{"type":"text","text":"settled reasoning","idx":3}]`)
	original := record{RunID: "run1", Transcript: []contract.Message{
		{Role: contract.RoleSystem, Content: "sys"},
		{Role: contract.RoleAssistant, Content: "answer", ReasoningDetails: raw},
	}}
	if err := sessions.WriteAgentRecord(session.ID, "run1", original); err != nil {
		t.Fatal(err)
	}
	records, err := sessions.ReadAgentRecords(session.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("read: %v (%d records)", err, len(records))
	}
	var restored record
	if err := json.Unmarshal(records[0], &restored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Transcript[1].ReasoningDetails, raw) {
		t.Fatalf("ReasoningDetails bytes changed:\n%s\n%s", raw, restored.Transcript[1].ReasoningDetails)
	}
	before, _ := json.Marshal(original)
	after, _ := json.Marshal(restored)
	if !bytes.Equal(before, after) {
		t.Fatalf("record did not round-trip byte-exactly:\n%s\n%s", before, after)
	}
	if err := sessions.WriteAgentRecord(session.ID, "../escape", original); err == nil {
		t.Fatal("invalid record id must be rejected")
	}
}
