package tui

import "testing"

// TestBridgeCoalescesAssistantChunks (US2 T029/T031): multiple assistant/reasoning
// tokens accumulate behind a single outstanding flush rather than one send each.
func TestBridgeCoalescesAssistantChunks(t *testing.T) {
	b := NewBridge()
	cb := b.Callbacks()
	cb.Token("Hel")
	cb.Token("lo")
	cb.ReasoningToken("think")
	cb.ReasoningToken("ing")

	b.mu.Lock()
	pending, reasoning, scheduled := b.pending, b.reasoning, b.scheduled
	b.mu.Unlock()
	if pending != "Hello" || reasoning != "thinking" {
		t.Fatalf("assistant chunks not coalesced: pending=%q reasoning=%q", pending, reasoning)
	}
	if !scheduled {
		t.Fatal("expected exactly one outstanding flush to be scheduled")
	}
}

// TestBridgeCoalescesToolOutputPerTool (US2 T029/T031): tool/shell chunks buffer
// per tool in first-seen order and share the one-outstanding flush.
func TestBridgeCoalescesToolOutputPerTool(t *testing.T) {
	b := NewBridge()
	cb := b.Callbacks()
	cb.ToolOutput("run_shell", "line1\n")
	cb.ToolOutput("run_shell", "line2\n")
	cb.ToolOutput("grep", "match\n")

	b.mu.Lock()
	shell, grep := b.toolBuf["run_shell"], b.toolBuf["grep"]
	order := append([]string(nil), b.toolOrder...)
	scheduled := b.scheduled
	b.mu.Unlock()
	if shell != "line1\nline2\n" {
		t.Fatalf("run_shell chunks not coalesced: %q", shell)
	}
	if grep != "match\n" {
		t.Fatalf("grep output missing: %q", grep)
	}
	if len(order) != 2 || order[0] != "run_shell" || order[1] != "grep" {
		t.Fatalf("tool order not preserved: %v", order)
	}
	if !scheduled {
		t.Fatal("expected one outstanding flush for tool output")
	}
}

// TestBridgeFlushClearsBuffers (US2 T029): the terminal flush path (invoked at
// tool/task boundaries) empties every buffer and re-arms scheduling.
func TestBridgeFlushClearsBuffers(t *testing.T) {
	b := NewBridge()
	cb := b.Callbacks()
	cb.Token("partial")
	cb.ToolOutput("run_shell", "out")

	b.flush() // program is nil, so this only drains internal state

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending != "" || b.reasoning != "" || len(b.toolOrder) != 0 || len(b.toolBuf) != 0 || b.scheduled {
		t.Fatalf("flush did not clear buffers: pending=%q tools=%v scheduled=%v", b.pending, b.toolOrder, b.scheduled)
	}
}
