package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TB03: an output-cap truncation must cost ONE turn with usable guidance — never
// a dispatched broken call, and never a payload re-billed on every later request.

// hugeContent stands in for a real file body: big enough that re-billing it
// across turns would dominate the request.
var hugeContent = strings.Repeat("x", 4000)

func TestTruncatedWriteIsNeverDispatchedAndNotReBilled(t *testing.T) {
	engine, provider, dir := fieldTestEngine(t,
		// The executor's write is cut off at the cap: the arguments are incomplete
		// JSON and the gateway tags the call id.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("d1", "run_subagent", `{"agent":"general","role":"builder","task":"Create app.html"}`),
		}},
		contract.ChatResponse{
			ToolCalls:      []contract.ToolCall{contract.NewToolCall("w1", "write_file", `{"path":"app.html","content":"`+hugeContent)},
			FinishReason:   "length",
			TruncatedCalls: []string{"w1"},
		},
		// Recovery: a bounded opening section, then the report.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("w2", "write_file", `{"path":"app.html","content":"<html>part one</html>"}`),
		}},
		contract.ChatResponse{Content: "Changes made with file:line: app.html:1 created.\nVerification: read the file back.\nProblems: none.\nSTATUS: COMPLETE"},
		contract.ChatResponse{Content: "Created app.html."},
	)

	if _, _, err := engine.Run(context.Background(), "Create app.html"); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// The truncated call never reached the workspace: only the recovery content
	// is on disk.
	body, err := os.ReadFile(filepath.Join(dir, "app.html"))
	if err != nil {
		t.Fatalf("recovery write did not land: %v", err)
	}
	if strings.Contains(string(body), hugeContent[:100]) {
		t.Fatal("the truncated payload was dispatched to the workspace")
	}

	// The recovery guidance reached the model, and it is the chunked-write
	// protocol rather than the old "send a shorter version" advice.
	var sawProtocol bool
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if message.Role == contract.RoleTool && strings.Contains(message.Content, "Write the file in parts") {
				sawProtocol = true
			}
			if strings.Contains(message.Content, "send a shorter version") {
				t.Fatal("the retired update_plan advice is still being sent")
			}
		}
	}
	if !sawProtocol {
		t.Fatal("the chunked-write protocol was never delivered to the model")
	}

	// The half-written payload is billed once (in the response we scripted), never
	// re-sent: no REQUEST may carry it.
	for index, request := range provider.requests {
		payload, _ := json.Marshal(request.Messages)
		if strings.Contains(string(payload), hugeContent[:200]) {
			t.Fatalf("request %d re-billed the truncated payload", index+1)
		}
		if strings.Contains(string(payload), truncatedArgumentsMarker) {
			continue // the compact marker is what should be stored instead
		}
	}
}

// A truncation must accumulate on the storm breaker so repeats escalate rather
// than riding to the turn ceiling. Class-keyed: two cuts at different offsets
// are different strings but the same failure.
func TestRepeatedTruncationsEscalate(t *testing.T) {
	counters := newCallCounters()
	var escalated []string
	scope := dispatchScope{counters: counters, escalate: func(notice string) { escalated = append(escalated, notice) }}
	engine := &Engine{}

	for i := 0; i < 3; i++ {
		// Each attempt is cut at a different offset, so only a class key can count them.
		call := contract.NewToolCall("w", "write_file", `{"path":"a.html","content":"`+strings.Repeat("y", 10+i))
		engine.truncatedOutcomes(scope, []contract.ToolCall{call})
	}
	if len(escalated) == 0 {
		t.Fatal("three truncations of the same tool must escalate through the loop guard")
	}
}

func TestTruncationRecoveryBodyIsToolAppropriate(t *testing.T) {
	if !strings.Contains(truncationRecoveryBody("write_file"), "Write the file in parts") {
		t.Fatal("a file write must get the chunked-write protocol")
	}
	if strings.Contains(truncationRecoveryBody("grep"), "Write the file in parts") {
		t.Fatal("a non-write tool must not be told to write files in parts")
	}
}

func TestSanitizeReplacesOnlyTruncatedArguments(t *testing.T) {
	calls := []contract.ToolCall{
		contract.NewToolCall("a", "read_file", `{"path":"keep.go"}`),
		contract.NewToolCall("b", "write_file", `{"path":"cut.html","content":"`+hugeContent),
	}
	sanitized := sanitizeTruncatedCalls(calls, map[string]bool{"b": true})
	if sanitized[0].ArgumentsJSON() != `{"path":"keep.go"}` {
		t.Fatalf("an intact call was rewritten: %q", sanitized[0].ArgumentsJSON())
	}
	if sanitized[1].ArgumentsJSON() != truncatedArgumentsMarker {
		t.Fatalf("the truncated call kept its payload: %q", sanitized[1].ArgumentsJSON())
	}
	if sanitized[1].ToolName() != "write_file" || sanitized[1].ID != "b" {
		t.Fatal("sanitizing must preserve the call's identity so pairing still works")
	}
}
