package command

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestJSONLEmitterProducesVersionedOrderedEventsAndTerminal(t *testing.T) {
	var output bytes.Buffer
	emitter := newJSONLEmitter(&output)
	callbacks := emitter.callbacks()
	callbacks.Status("thinking")
	callbacks.ToolStart("read_file", json.RawMessage(`{"path":"x"}`))
	callbacks.ToolEnd("read_file", "read x")
	stats := contract.TaskStats{Status: contract.TaskStatusSucceeded}
	emitter.terminal("done", stats, nil)
	if err := emitter.Err(); err != nil {
		t.Fatal(err)
	}

	var events []headlessEvent
	scanner := bufio.NewScanner(&output)
	for scanner.Scan() {
		var event headlessEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid JSONL row %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("event count = %d, want 4: %s", len(events), output.String())
	}
	for _, event := range events {
		if event.Version != headlessEventVersion || event.At.IsZero() {
			t.Fatalf("event lacks stable envelope: %+v", event)
		}
	}
	terminal := events[len(events)-1]
	if terminal.Type != "task_terminal" || terminal.Answer != "done" || terminal.Stats == nil || terminal.Stats.Status != contract.TaskStatusSucceeded {
		t.Fatalf("terminal event mismatch: %+v", terminal)
	}
}
