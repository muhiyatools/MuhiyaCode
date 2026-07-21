package gateway

import (
	"strings"
	"testing"
)

// TB01: a tool call cut off at the output cap must be reported as truncated, so
// the orchestrator can recover instead of dispatching half-written JSON.

// feedCallDelta pushes one streamed tool-call argument fragment into an
// accumulator as an SSE data line, mirroring an OpenAI-compatible delta chunk.
func feedCallDelta(t *testing.T, acc *StreamAccumulator, id, name, argsFragment, finish string) {
	t.Helper()
	chunk := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id + `","function":{"name":"` + name + `","arguments":` + quoteJSON(argsFragment) + `}}]}`
	if finish != "" {
		chunk += `,"finish_reason":"` + finish + `"`
	}
	chunk += `}]}`
	if err := acc.ConsumeLine("data: " + chunk); err != nil {
		t.Fatalf("consume: %v", err)
	}
}

func quoteJSON(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value) + `"`
}

func newTestAccumulator() *StreamAccumulator {
	return NewStreamAccumulatorForProfile(ModelProfile{}, nil, nil)
}

func TestTruncatedToolCallIsFlaggedAtTheCap(t *testing.T) {
	acc := newTestAccumulator()
	// A large write_file whose arguments stop mid-string, ended by the cap.
	feedCallDelta(t, acc, "w1", "write_file", `{"path":"snake.html","content":"<html><body`, "length")
	result := acc.Result()
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected the call to still be reported, got %d", len(result.ToolCalls))
	}
	if len(result.TruncatedCalls) != 1 || result.TruncatedCalls[0] != "w1" {
		t.Fatalf("truncated call not flagged: %+v", result.TruncatedCalls)
	}
}

func TestCompleteCallAtTheCapIsNotFlagged(t *testing.T) {
	acc := newTestAccumulator()
	// finish_reason=length can also mean the PROSE was cut after a whole call.
	feedCallDelta(t, acc, "r1", "read_file", `{"path":"a.go"}`, "length")
	result := acc.Result()
	if len(result.TruncatedCalls) != 0 {
		t.Fatalf("a complete call must not be flagged truncated: %+v", result.TruncatedCalls)
	}
}

func TestIncompleteCallWithoutLengthFinishIsNotFlagged(t *testing.T) {
	acc := newTestAccumulator()
	// A normal stop with odd arguments is a model error, not a cap truncation:
	// the existing malformed-argument path owns it.
	feedCallDelta(t, acc, "x1", "write_file", `{"path":"a.go"`, "stop")
	result := acc.Result()
	if len(result.TruncatedCalls) != 0 {
		t.Fatalf("only a length-finish counts as truncation: %+v", result.TruncatedCalls)
	}
}
