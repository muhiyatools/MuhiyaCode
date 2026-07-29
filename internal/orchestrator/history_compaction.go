package orchestrator

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	maxCompactionDigestChars  = 6000
	maxCompactionSummaryChars = 64000
	maxCompactionRecords      = 256
)

// CompactionRecord proves which message prefix a bounded summary replaced.
// It intentionally stores hashes and sizes rather than another transcript.
type CompactionRecord struct {
	Sequence       int    `json:"sequence"`
	At             string `json:"at"`
	Reason         string `json:"reason,omitempty"`
	SourceHash     string `json:"sourceHash"`
	SourceMessages int    `json:"sourceMessages"`
	SourceChars    int    `json:"sourceChars"`
	SummaryHash    string `json:"summaryHash"`
	SummaryChars   int    `json:"summaryChars"`
}

func (h *History) CompactionRecords() []CompactionRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]CompactionRecord(nil), h.compactions...)
}

func (h *History) CompactTo(summary string, keepRecentUnits int) {
	h.CompactToWithReason(summary, keepRecentUnits, "")
}

func (h *History) CompactToWithReason(summary string, keepRecentUnits int, reason string) CompactionRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	units := GroupUnits(h.messages)
	keepRecentUnits = min(max(keepRecentUnits, 0), len(units))
	var kept []contract.Message
	for _, unit := range units[len(units)-keepRecentUnits:] {
		kept = append(kept, unit...)
	}
	removed := len(h.messages) - len(kept)
	removedMessages := cloneMessages(h.messages[:removed])
	h.messages = cloneMessages(kept)
	h.invalidateRequestUnitsLocked()
	summary = truncateUTF8Bytes(strings.TrimSpace(summary), maxCompactionDigestChars)
	h.compactSummary = appendBoundedCompactionSummary(h.compactSummary, summary)
	sequence := 1
	if len(h.compactions) > 0 {
		sequence = h.compactions[len(h.compactions)-1].Sequence + 1
	}
	record := newCompactionRecord(sequence, reason, removedMessages, summary)
	h.compactions = append(h.compactions, record)
	if len(h.compactions) > maxCompactionRecords {
		h.compactions = append([]CompactionRecord(nil), h.compactions[len(h.compactions)-maxCompactionRecords:]...)
	}
	h.lastTaskStart = max(0, h.lastTaskStart-removed)
	h.rewriteVersion++
	h.lastWindowStart = 0
	h.windowInitialized = false
	h.saveLocked()
	return record
}

func appendBoundedCompactionSummary(existing, next string) string {
	if next == "" {
		return truncateUTF8Tail(existing, maxCompactionSummaryChars)
	}
	combined := next
	if strings.TrimSpace(existing) != "" {
		combined = existing + "\n\n" + next
	}
	if len(combined) <= maxCompactionSummaryChars {
		return combined
	}
	overflow := len(combined) - maxCompactionSummaryChars
	if boundary := strings.Index(combined[overflow:], "\n\n"); boundary >= 0 {
		return combined[overflow+boundary+2:]
	}
	return truncateUTF8Tail(combined, maxCompactionSummaryChars)
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	const marker = "..."
	if limit <= len(marker) {
		return marker[:limit]
	}
	end := limit - len(marker)
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + marker
}

func truncateUTF8Tail(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	const marker = "..."
	if limit <= len(marker) {
		return marker[:limit]
	}
	start := len(value) - (limit - len(marker))
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return marker + value[start:]
}

func newCompactionRecord(sequence int, reason string, source []contract.Message, summary string) CompactionRecord {
	sourceChars := 0
	for _, message := range source {
		sourceChars += len(message.Content) + len(message.ToolCallID) + len(message.ReasoningDetails)
		if message.ReasoningContent != nil {
			sourceChars += len(*message.ReasoningContent)
		}
		for _, call := range message.ToolCalls {
			sourceChars += len(call.ID) + len(call.Type) + len(call.ToolName()) + len(call.ArgumentsJSON())
		}
	}
	return CompactionRecord{
		Sequence:       sequence,
		At:             time.Now().UTC().Format(time.RFC3339Nano),
		Reason:         strings.TrimSpace(reason),
		SourceHash:     compactionSourceHash(source),
		SourceMessages: len(source),
		SourceChars:    sourceChars,
		SummaryHash:    hashBytes([]byte(summary)),
		SummaryChars:   len(summary),
	}
}

func compactionSourceHash(messages []contract.Message) string {
	var payload []byte
	appendField := func(value string) {
		payload = strconv.AppendInt(payload, int64(len(value)), 10)
		payload = append(payload, ':')
		payload = append(payload, value...)
	}
	appendField(strconv.Itoa(len(messages)))
	for _, message := range messages {
		appendField(string(message.Role))
		appendField(message.Content)
		appendField(message.ToolCallID)
		appendField(string(message.ReasoningDetails))
		if message.ReasoningContent != nil {
			appendField(*message.ReasoningContent)
		} else {
			appendField("")
		}
		appendField(strconv.Itoa(len(message.ToolCalls)))
		for _, call := range message.ToolCalls {
			appendField(call.ID)
			appendField(call.Type)
			appendField(call.ToolName())
			appendField(call.ArgumentsJSON())
		}
	}
	return hashBytes(payload)
}
