package orchestrator

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	TrimNote              = "... (older tool output trimmed) ..."
	FoldNote              = "(folded - full output in session records)"
	OutputTruncatedMarker = "chars truncated - narrow the query"
	defaultFoldKeepChars  = 240
)

type HistorySnapshot struct {
	Version        int                `json:"version"`
	CompactSummary string             `json:"compactSummary,omitempty"`
	Messages       []contract.Message `json:"messages"`
	LastTaskStart  int                `json:"lastTaskStart"`
}

type History struct {
	mu             sync.RWMutex
	messages       []contract.Message
	compactSummary string
	lastTaskStart  int
	superseded     map[string]struct{}
	persist        func(HistorySnapshot) error
}

func NewHistory(snapshot HistorySnapshot, persist func(HistorySnapshot) error) *History {
	if snapshot.Version != 1 {
		snapshot = HistorySnapshot{Version: 1}
	}
	start := min(max(snapshot.LastTaskStart, 0), len(snapshot.Messages))
	return &History{messages: cloneMessages(snapshot.Messages), compactSummary: snapshot.CompactSummary, lastTaskStart: start, superseded: make(map[string]struct{}), persist: persist}
}

func (h *History) Snapshot() HistorySnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.snapshotLocked()
}

func (h *History) MarkTaskStart() {
	h.mu.Lock()
	h.lastTaskStart = len(h.messages)
	h.saveLocked()
	h.mu.Unlock()
}

func (h *History) Append(message contract.Message) {
	h.mu.Lock()
	h.messages = append(h.messages, message)
	h.saveLocked()
	h.mu.Unlock()
}

func (h *History) All() []contract.Message {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return cloneMessages(h.messages)
}

func (h *History) CompactSummary() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.compactSummary
}

func (h *History) EstimatedTokens() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.estimatedLocked()
}

func EstimateTokens(text string) int { return (len(text)+3)/4 + 4 }

func EstimateMessageTokens(message contract.Message) int {
	total := EstimateTokens(message.Content)
	for _, call := range message.ToolCalls {
		total += EstimateTokens(call.ToolName()) + EstimateTokens(call.ArgumentsJSON())
	}
	return total
}

func (h *History) BuildRequest(system string, contextLimit, reserve int) []contract.Message {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return assembleRequest(h.messages, h.compactSummary, system, contextLimit, reserve)
}

func (h *History) MarkSuperseded(callIDs []string) {
	h.mu.Lock()
	for _, id := range callIDs {
		h.superseded[id] = struct{}{}
	}
	h.mu.Unlock()
}

func (h *History) TrimAged(keepFull, trimmedChars, minBatch int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	var indexes []int
	for i, message := range h.messages {
		if message.Role == contract.RoleTool {
			indexes = append(indexes, i)
		}
	}
	agedBefore := max(0, len(indexes)-keepFull)
	aged := make(map[int]struct{}, agedBefore)
	for _, index := range indexes[:agedBefore] {
		aged[index] = struct{}{}
	}
	var eligible []int
	for _, index := range indexes {
		message := h.messages[index]
		_, isAged := aged[index]
		_, isSuperseded := h.superseded[message.ToolCallID]
		if (isAged || isSuperseded) && len(message.Content) > trimmedChars && !strings.Contains(message.Content, TrimNote) && !strings.Contains(message.Content, FoldNote) {
			eligible = append(eligible, index)
		}
	}
	if len(eligible) < minBatch {
		return false
	}
	for _, index := range eligible {
		content := h.messages[index].Content
		head := content[:min(len(content), trimmedChars/2)]
		tailSize := min(len(content)-len(head), trimmedChars/4)
		tail := content[len(content)-tailSize:]
		h.messages[index].Content = head + "\n" + TrimNote + "\n" + tail
	}
	h.saveLocked()
	return true
}

func (h *History) FoldCompletedTasks() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	limit := min(h.lastTaskStart, len(h.messages))
	before := h.estimatedLocked()
	changed := false
	for i := 0; i < limit; i++ {
		m := &h.messages[i]
		if m.Role == contract.RoleTool && len(m.Content) > defaultFoldKeepChars && !strings.Contains(m.Content, FoldNote) {
			headline := strings.SplitN(m.Content, "\n", 2)[0]
			m.Content = truncate(headline, 120) + " " + FoldNote
			changed = true
		} else if m.Role == contract.RoleAssistant {
			folded := foldCalls(m.ToolCalls)
			if callsDiffer(m.ToolCalls, folded) {
				m.ToolCalls = folded
				changed = true
			}
		}
	}
	if !changed {
		return 0
	}
	h.saveLocked()
	return max(0, before-h.estimatedLocked())
}

func (h *History) IsToolResultIntact(callID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, message := range h.messages {
		if message.Role == contract.RoleTool && message.ToolCallID == callID {
			return !strings.Contains(message.Content, TrimNote) && !strings.Contains(message.Content, FoldNote) && !strings.Contains(message.Content, OutputTruncatedMarker)
		}
	}
	return false
}

func (h *History) CompactTo(summary string, keepRecentUnits int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	units := GroupUnits(h.messages)
	if keepRecentUnits > len(units) {
		keepRecentUnits = len(units)
	}
	var kept []contract.Message
	for _, unit := range units[len(units)-keepRecentUnits:] {
		kept = append(kept, unit...)
	}
	removed := len(h.messages) - len(kept)
	h.messages = cloneMessages(kept)
	h.compactSummary = summary
	h.lastTaskStart = max(0, h.lastTaskStart-removed)
	h.saveLocked()
}

func GroupUnits(messages []contract.Message) [][]contract.Message {
	var units [][]contract.Message
	var current []contract.Message
	for _, message := range messages {
		if message.Role == contract.RoleUser && len(current) > 0 {
			units = append(units, current)
			current = nil
		}
		current = append(current, message)
	}
	if len(current) > 0 {
		units = append(units, current)
	}
	return units
}

func assembleRequest(messages []contract.Message, summary, system string, contextLimit, reserve int) []contract.Message {
	budget := max(8_000, contextLimit-reserve)
	header := []contract.Message{{Role: contract.RoleSystem, Content: system}}
	if summary != "" {
		header = append(header, contract.Message{Role: contract.RoleSystem, Content: "Summary of earlier conversation:\n" + summary})
	}
	used := 0
	for _, message := range header {
		used += EstimateMessageTokens(message)
	}
	units := GroupUnits(messages)
	start := len(units)
	for i := len(units) - 1; i >= 0; i-- {
		cost := 0
		for _, message := range units[i] {
			cost += EstimateMessageTokens(message)
		}
		if used+cost > budget && start < len(units) {
			break
		}
		start = i
		used += cost
	}
	result := cloneMessages(header)
	for _, unit := range units[start:] {
		result = append(result, cloneMessages(unit)...)
	}
	return result
}

func foldCalls(calls []contract.ToolCall) []contract.ToolCall {
	result := append([]contract.ToolCall(nil), calls...)
	for i, call := range result {
		args := call.ArgumentsJSON()
		if len(args) <= defaultFoldKeepChars {
			continue
		}
		var value map[string]any
		_ = json.Unmarshal([]byte(args), &value)
		target := ""
		for _, key := range []string{"path", "command", "pattern"} {
			if text, ok := value[key].(string); ok {
				target = text
				break
			}
		}
		folded, _ := json.Marshal(map[string]string{"folded": strings.TrimSpace(call.ToolName() + " " + truncate(target, 80))})
		result[i] = contract.NewToolCall(call.ID, call.ToolName(), string(folded))
	}
	return result
}

func callsDiffer(a, b []contract.ToolCall) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].ArgumentsJSON() != b[i].ArgumentsJSON() {
			return true
		}
	}
	return false
}

func cloneMessages(value []contract.Message) []contract.Message {
	result := make([]contract.Message, len(value))
	copy(result, value)
	for i := range result {
		result[i].ToolCalls = append([]contract.ToolCall(nil), value[i].ToolCalls...)
	}
	return result
}

func (h *History) estimatedLocked() int {
	total := 0
	if h.compactSummary != "" {
		total += EstimateTokens(h.compactSummary)
	}
	for _, message := range h.messages {
		total += EstimateMessageTokens(message)
	}
	return total
}

func (h *History) snapshotLocked() HistorySnapshot {
	return HistorySnapshot{Version: 1, CompactSummary: h.compactSummary, Messages: cloneMessages(h.messages), LastTaskStart: h.lastTaskStart}
}

func (h *History) saveLocked() {
	if h.persist != nil {
		_ = h.persist(h.snapshotLocked())
	}
}

func truncate(value string, size int) string {
	runes := []rune(value)
	if len(runes) <= size {
		return value
	}
	return string(runes[:size])
}
