package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The handoff-fidelity guards (G1). Each one covers a way the main model and the
// execution agent could silently stop understanding each other.

// THE TRUST-PROTOCOL DEFECT. An executor's report ends with its verification
// section and its STATUS line — the two things the caller's trust rule reads.
// Tail-cutting truncation deleted exactly those on any report over the bound, so
// a long, productive run came back looking unverified and the caller re-ran work
// that was already done.
func TestLongReportKeepsItsStatusLine(t *testing.T) {
	body := strings.Repeat("Changed internal/thing.go:12 to fix the loader default. ", 120) // ~6.7k chars
	report := body + "\nVerification: go test ./internal/... passed (ok, 4.1s).\nSTATUS: COMPLETE"
	engine, _ := windowEngine(t, "longreport", 1_000_000, contract.ChatResponse{Content: report})

	out, err := engine.runSubagentInput(context.Background(), subagentInput{Agent: "general", Task: "fix the loader default"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "STATUS: COMPLETE") {
		t.Fatalf("the STATUS line did not survive truncation — the caller will re-verify finished work:\n%s", out)
	}
	if !strings.Contains(out, "Verification: go test") {
		t.Fatalf("the verification section did not survive truncation:\n%s", out)
	}
	// Still bounded, and the elision is explicit rather than silent.
	if len(out) > 2800 {
		t.Fatalf("parent handoff exceeded the digest bound: %d chars", len(out))
	}
	if !strings.Contains(out, "trimmed from the middle") {
		t.Fatal("truncation must announce itself, not silently drop content")
	}
	// And the status the trust rule parses is genuinely COMPLETE, not UNKNOWN.
	if got := parseReportStatus(out); got != ReportStatusComplete {
		t.Fatalf("parsed status = %q, want complete", got)
	}
}

// The handoff carried the task twice — once truncated into Scope, once in full
// under TASK: — so every dispatch paid for the same text two times.
func TestHandoffDoesNotSendTheTaskTwice(t *testing.T) {
	task := strings.Repeat("rebuild the invoice export pipeline end to end. ", 40) // ~1.9k chars
	handoff := handoffFor(subagentInput{Agent: "general", Task: task}, "")
	message := subagentUserMessage(handoff, task)

	if strings.Count(message, "rebuild the invoice export pipeline") < 2 {
		t.Fatal("fixture is wrong: the task should appear in the TASK block")
	}
	// Scope is a label now. If it were still a 1200-char copy the message would
	// be ~3.1k; with a one-line digest it stays close to the task itself.
	if len(message) > len(task)+900 {
		t.Fatalf("handoff still duplicates the task: message=%d task=%d", len(message), len(task))
	}
	if strings.Contains(handoff.Scope, "\n") {
		t.Fatalf("Scope must be a single-line label, got %q", handoff.Scope)
	}
}

// Steering typed while an agent is running used to sit in the queue until the
// main loop resumed — minutes of watching an agent do something you already
// asked it to stop. It now reaches the run in flight, and the parent is told.
func TestSteeringReachesARunningSubagent(t *testing.T) {
	engine, provider := windowEngine(t, "steer", 1_000_000,
		contract.ChatResponse{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}},
		contract.ChatResponse{Content: "Adjusted per your note. STATUS: COMPLETE"},
	)
	// Queue the interjection before the run starts; the loop drains it at the top
	// of each turn, so it lands on turn 1. Set directly rather than through
	// QueueUserMessage, which requires a live Run to have claimed the task slot —
	// this test drives executeSubagent on its own.
	engine.mu.Lock()
	engine.steering = []string{"actually use the v2 endpoint, not v1"}
	engine.mu.Unlock()

	result := engine.executeSubagent(context.Background(), "s1", subagentInput{Agent: "general", Title: "t", Task: "wire the endpoint"}, engine.subagentSpecs()["general"])
	if result.Status != "done" {
		t.Fatalf("status = %q", result.Status)
	}

	delivered := 0
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "v2 endpoint") {
				delivered++
				break // count requests carrying it, not repeats within one transcript
			}
		}
	}
	if delivered == 0 {
		t.Fatal("the interjection never reached the running agent")
	}
	// It must read as a course correction, not a replacement task.
	sawFraming := false
	for _, request := range provider.requests {
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "course correction") {
				sawFraming = true
			}
		}
	}
	if !sawFraming {
		t.Fatal("the interjection was delivered without its course-correction framing")
	}
	// The parent must learn its dispatch was steered, or it will resume with a
	// stale idea of what it asked for.
	parentNotified := false
	for _, message := range engine.history.All() {
		if strings.Contains(message.Content, "[steering]") && strings.Contains(message.Content, "v2 endpoint") {
			parentNotified = true
		}
	}
	if !parentNotified {
		t.Fatal("the parent was never told its running agent had been redirected")
	}
	// And the queue must be empty afterwards, or the main loop replays it.
	if engine.hasSteering() {
		t.Fatal("steering was forwarded but not consumed — the main loop will deliver it a second time")
	}
}
