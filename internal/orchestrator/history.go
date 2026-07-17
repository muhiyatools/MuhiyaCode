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
	// minPruneBytes (T040, Reasonix prune.go) is the floor below which a tool
	// result is left untouched — trimming a small result is not worth a prefix
	// rewrite.
	minPruneBytes = 1024
)

type HistorySnapshot struct {
	Version           int                `json:"version"`
	CompactSummary    string             `json:"compactSummary,omitempty"`
	Messages          []contract.Message `json:"messages"`
	LastTaskStart     int                `json:"lastTaskStart"`
	RewriteVersion    int                `json:"rewriteVersion,omitempty"`
	LastWindowStart   int                `json:"lastWindowStart,omitempty"`
	WindowInitialized bool               `json:"windowInitialized,omitempty"`
}

type History struct {
	mu                sync.RWMutex
	messages          []contract.Message
	compactSummary    string
	lastTaskStart     int
	rewriteVersion    int
	lastWindowStart   int
	windowInitialized bool
	superseded        map[string]struct{}
	persist           func(HistorySnapshot) error
	// archive (T041) receives the originals of tool results about to be shortened
	// by reclamation, BEFORE the mutation, so they stay recoverable. Optional.
	archive func([]contract.PrunedRecord) error
	// tokPerChar (T038) is the tokens/char ratio calibrated from real provider
	// usage. 0 means uncalibrated → the estimator falls back to the fixed 0.25
	// heuristic. Guarded by mu.
	tokPerChar float64
}

type PressureInput struct {
	Tokens    int  `json:"tokens"`
	Estimated bool `json:"estimated"`
}

type MaintenanceResult struct {
	Changed      bool
	Folded       bool
	FoldedTokens int
	TrimmedTools int
}

type RequestBuild struct {
	Messages      []contract.Message
	WindowDropped bool
	DroppedUnits  int
}

func NewHistory(snapshot HistorySnapshot, persist func(HistorySnapshot) error) *History {
	if snapshot.Version != 1 {
		snapshot = HistorySnapshot{Version: 1}
	}
	start := min(max(snapshot.LastTaskStart, 0), len(snapshot.Messages))
	return &History{
		messages:          cloneMessages(snapshot.Messages),
		compactSummary:    snapshot.CompactSummary,
		lastTaskStart:     start,
		rewriteVersion:    max(0, snapshot.RewriteVersion),
		lastWindowStart:   max(0, snapshot.LastWindowStart),
		windowInitialized: snapshot.WindowInitialized,
		superseded:        make(map[string]struct{}),
		persist:           persist,
	}
}

// SetPruneArchive (T041) wires the archival hook invoked with the originals of
// tool results about to be shortened by reclamation. Called once at engine
// construction; nil disables archival (tests, unit engines).
func (h *History) SetPruneArchive(fn func([]contract.PrunedRecord) error) {
	h.mu.Lock()
	h.archive = fn
	h.mu.Unlock()
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

func (h *History) PressureInput(reported int, available bool) PressureInput {
	if available {
		return PressureInput{Tokens: reported}
	}
	return PressureInput{Tokens: h.EstimatedTokens(), Estimated: true}
}

func (h *History) RewriteVersion() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rewriteVersion
}

func EstimateTokens(text string) int { return (len(text)+3)/4 + 4 }

// TokensForChars estimates the token count of a component chars bytes long using
// the calibrated tokens-per-char ratio when available (feature 008 UD-12, the
// /context usage-by-category rows). Zero chars is zero tokens — category rows
// must not inherit the flat +4 constant the text estimators add.
func (h *History) TokensForChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	h.mu.RLock()
	ratio := h.tokPerChar
	h.mu.RUnlock()
	if ratio <= 0 {
		return (chars + 3) / 4
	}
	return int(float64(chars) * ratio)
}

func EstimateMessageTokens(message contract.Message) int {
	total := EstimateTokens(message.Content)
	for _, call := range message.ToolCalls {
		total += EstimateTokens(call.ToolName()) + EstimateTokens(call.ArgumentsJSON())
	}
	return total
}

func (h *History) BuildRequest(system string, contextLimit, reserve int) []contract.Message {
	return h.BuildRequestWithMetadata(system, contextLimit, reserve).Messages
}

func (h *History) BuildRequestWithMetadata(system string, contextLimit, reserve int) RequestBuild {
	h.mu.Lock()
	defer h.mu.Unlock()
	request, start, _ := assembleRequestWithStart(h.messages, h.compactSummary, system, contextLimit, reserve)
	changed := !h.windowInitialized || start != h.lastWindowStart
	dropped := h.windowInitialized && start > h.lastWindowStart
	droppedUnits := 0
	if dropped {
		droppedUnits = start - h.lastWindowStart
	}
	if h.windowInitialized && start > h.lastWindowStart {
		h.rewriteVersion++
	}
	h.lastWindowStart = start
	h.windowInitialized = true
	if changed {
		h.saveLocked()
	}
	return RequestBuild{Messages: request, WindowDropped: dropped, DroppedUnits: droppedUnits}
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
	trimmed := h.trimAgedLocked(keepFull, trimmedChars, minBatch)
	if trimmed == 0 {
		return false
	}
	h.rewriteVersion++
	h.saveLocked()
	return true
}

func (h *History) trimAgedLocked(keepFull, trimmedChars, minBatch int) int {
	return trimAgedMessages(h.messages, h.superseded, keepFull, trimmedChars, minBatch)
}

// trimAgedMessages trims aged/superseded tool results to head+tail in place.
// Pure over (messages, superseded set) so EstimateMaintainYield can dry-run it
// on a clone without touching live history.
func trimAgedMessages(messages []contract.Message, superseded map[string]struct{}, keepFull, trimmedChars, minBatch int) int {
	var indexes []int
	for i, message := range messages {
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
		message := messages[index]
		_, isAged := aged[index]
		_, isSuperseded := superseded[message.ToolCallID]
		if !(isAged || isSuperseded) {
			continue
		}
		// T040: a 1024-byte floor (do not trim small results) and error-pin
		// (results that carry a failure reach compaction verbatim, since the
		// error text is exactly what the model needs to change approach).
		if len(message.Content) < minPruneBytes || len(message.Content) <= trimmedChars {
			continue
		}
		if strings.Contains(message.Content, TrimNote) || strings.Contains(message.Content, FoldNote) || isErrorResult(message.Content) {
			continue
		}
		eligible = append(eligible, index)
	}
	if len(eligible) < minBatch {
		return 0
	}
	for _, index := range eligible {
		content := messages[index].Content
		// T040: content-aware geometry by the producing tool's kind. The tool
		// name is resolved from the assistant tool_calls by ID (tool-result
		// messages don't carry it), so this changes no wire bytes.
		name := toolNameForResult(messages, messages[index].ToolCallID)
		headBudget, tailBudget := snipHeadTail(name, trimmedChars)
		head := content[:min(len(content), headBudget)]
		tailSize := min(len(content)-len(head), tailBudget)
		tail := content[len(content)-tailSize:]
		messages[index].Content = head + "\n" + TrimNote + "\n" + tail
	}
	return len(eligible)
}

// snipHeadTail (T040) returns the head/tail char budget for trimming a tool
// result, content-aware by the producing tool's kind. Read-only results are
// front-loaded (long head, short tail) because their signal is at the top;
// side-effecting results split evenly because a failure can sit at either end.
// Budgets are scaled to MuhiyaCode's per-effort trim budget rather than
// Reasonix's absolute char counts, so overall reclamation aggressiveness is
// unchanged — only the head/tail SHAPE adapts per kind.
func snipHeadTail(toolName string, budget int) (head, tail int) {
	if readonlyTools[toolName] {
		return budget * 3 / 4, budget / 4
	}
	return budget / 2, budget / 2
}

// toolNameForResult finds the tool that produced a RoleTool result by matching
// its ToolCallID against the assistant tool_calls earlier in the log. Returns
// "" when unknown, which snipHeadTail treats as side-effecting (the
// conservative, balanced default).
func toolNameForResult(messages []contract.Message, toolCallID string) string {
	for _, m := range messages {
		if m.Role != contract.RoleAssistant {
			continue
		}
		for _, call := range m.ToolCalls {
			if call.ID == toolCallID {
				return call.ToolName()
			}
		}
	}
	return ""
}

// isErrorResult reports whether a tool result carries a failure the model needs
// verbatim (so it is pinned from trimming). Covers the main-loop "Tool X
// failed:" shape and the Reasonix error:/blocked: prefixes.
func isErrorResult(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "blocked:") {
		return true
	}
	return strings.HasPrefix(lower, "tool ") && strings.Contains(lower, " failed:")
}

// EstimateMaintainYield (T007 / REV A1) returns how many estimated tokens a
// Maintain(keepFull, trimmedChars, minBatch) call WOULD reclaim right now,
// WITHOUT mutating history. The engine calls this before deciding to run
// Maintain: below the minimum-yield floor it skips entirely, so no settled
// bytes change and no invalidation event is owed. It runs the exact same fold
// and trim algorithms (shared pure functions) on a deep clone, so the estimate
// can never disagree with what Maintain actually does.
func (h *History) EstimateMaintainYield(keepFull, trimmedChars, minBatch int) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clone := cloneMessages(h.messages)
	before := estimateMessagesTokens(clone)
	folded := foldCompletedMessages(clone, min(h.lastTaskStart, len(clone)))
	trimmed := trimAgedMessages(clone, h.superseded, keepFull, trimmedChars, minBatch)
	if !folded && trimmed == 0 {
		return 0
	}
	return max(0, before-estimateMessagesTokens(clone))
}

// estimateMessagesTokens mirrors estimatedLocked's per-message accounting for a
// detached slice (no compactSummary term — the estimator only measures the
// message body that fold/trim can shrink).
func estimateMessagesTokens(messages []contract.Message) int {
	total := 0
	for _, message := range messages {
		total += EstimateMessageTokens(message)
	}
	return total
}

func (h *History) FoldCompletedTasks() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	before := h.estimatedLocked()
	if !h.foldCompletedLocked() {
		return 0
	}
	h.rewriteVersion++
	h.saveLocked()
	return max(0, before-h.estimatedLocked())
}

func (h *History) foldCompletedLocked() bool {
	return foldCompletedMessages(h.messages, min(h.lastTaskStart, len(h.messages)))
}

// foldCompletedMessages folds completed-task tool output and assistant tool-call
// arguments in place within messages[:limit]. It is pure over the slice so the
// live maintenance path and the read-only yield estimator (EstimateMaintainYield)
// share ONE algorithm — the estimate can never diverge from what Maintain does.
func foldCompletedMessages(messages []contract.Message, limit int) bool {
	if limit > len(messages) {
		limit = len(messages)
	}
	changed := false
	for i := 0; i < limit; i++ {
		m := &messages[i]
		if m.Role == contract.RoleTool && len(m.Content) > defaultFoldKeepChars && !strings.Contains(m.Content, FoldNote) {
			headline := strings.SplitN(m.Content, "\n", 2)[0]
			m.Content = contract.TruncateEllipsis(headline, 120) + " " + FoldNote
			changed = true
		} else if m.Role == contract.RoleAssistant {
			folded := foldCalls(m.ToolCalls)
			if callsDiffer(m.ToolCalls, folded) {
				m.ToolCalls = folded
				changed = true
			}
		}
	}
	return changed
}

// Maintain performs folding and trimming under one lock, one persistence
// write, and one rewrite-version increment so a pressure boundary creates one
// attributable prefix reset rather than a series of smaller rewrites.
func (h *History) Maintain(keepFull, trimmedChars, minBatch int) MaintenanceResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	before := h.estimatedLocked()
	// T041: snapshot the messages BEFORE mutating so we can archive the originals
	// of any tool result the fold/trim shortens. Only clone when archival is
	// actually wired — the clone is pure overhead otherwise.
	var originals []contract.Message
	if h.archive != nil {
		originals = cloneMessages(h.messages)
	}
	folded := h.foldCompletedLocked()
	trimmed := h.trimAgedLocked(keepFull, trimmedChars, minBatch)
	if !folded && trimmed == 0 {
		return MaintenanceResult{}
	}
	if originals != nil {
		h.archiveChangedLocked(originals)
	}
	h.rewriteVersion++
	h.saveLocked()
	return MaintenanceResult{Changed: true, Folded: folded, FoldedTokens: max(0, before-h.estimatedLocked()), TrimmedTools: trimmed}
}

// archiveChangedLocked (T041) appends the originals of any RoleTool result whose
// content the just-completed fold/trim shortened. Diff-based, so it needs no
// cooperation from the pure fold/trim functions. Best-effort: an archive write
// failure must never fail reclamation, so the error is swallowed.
func (h *History) archiveChangedLocked(originals []contract.Message) {
	if h.archive == nil {
		return
	}
	var records []contract.PrunedRecord
	for i := range h.messages {
		if i >= len(originals) {
			break
		}
		if h.messages[i].Role != contract.RoleTool || h.messages[i].Content == originals[i].Content {
			continue
		}
		reason := "trim"
		if strings.Contains(h.messages[i].Content, FoldNote) {
			reason = "fold"
		}
		records = append(records, contract.PrunedRecord{
			ToolCallID:      originals[i].ToolCallID,
			ToolName:        toolNameForResult(originals, originals[i].ToolCallID),
			Reason:          reason,
			OriginalBytes:   len(originals[i].Content),
			ReducedToBytes:  len(h.messages[i].Content),
			OriginalContent: originals[i].Content,
		})
	}
	if len(records) > 0 {
		_ = h.archive(records)
	}
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
	// T042: digests ACCUMULATE — a new digest is appended to prior digests
	// (which stay byte-identical) rather than replacing them, so repeated
	// compaction is not lossy re-summarization that drops earlier facts.
	if strings.TrimSpace(h.compactSummary) == "" {
		h.compactSummary = summary
	} else {
		h.compactSummary = h.compactSummary + "\n\n" + summary
	}
	h.lastTaskStart = max(0, h.lastTaskStart-removed)
	h.rewriteVersion++
	h.lastWindowStart = 0
	h.windowInitialized = false
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

func assembleRequestWithStart(messages []contract.Message, summary, system string, contextLimit, reserve int) ([]contract.Message, int, int) {
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
	return result, start, len(units)
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
		folded, _ := json.Marshal(map[string]string{"folded": strings.TrimSpace(call.ToolName() + " " + contract.TruncateEllipsis(target, 80))})
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
		total += h.estimateTextLocked(h.compactSummary)
	}
	for _, message := range h.messages {
		total += h.estimateMessageLocked(message)
	}
	return total
}

// estimateMessageLocked (T038) estimates one message's tokens using the
// calibrated tokens/char ratio when available, else the 0.25 fallback. Framing
// is +4 per message and +8 per tool call (Reasonix constants); reasoning content
// is excluded because it is never re-sent to the provider.
func (h *History) estimateMessageLocked(m contract.Message) int {
	if h.tokPerChar <= 0 {
		return EstimateMessageTokens(m)
	}
	chars := len(m.Content)
	for _, call := range m.ToolCalls {
		chars += len(call.ToolName()) + len(call.ArgumentsJSON())
	}
	return int(float64(chars)*h.tokPerChar) + 4 + 8*len(m.ToolCalls)
}

func (h *History) estimateTextLocked(text string) int {
	if h.tokPerChar <= 0 {
		return EstimateTokens(text)
	}
	return int(float64(len(text))*h.tokPerChar) + 4
}

// Calibrate (T038) updates the tokens/char ratio from a real provider usage:
// promptTokens / (systemChars + all message chars), clamped to (0.05, 2). An
// out-of-range ratio, zero prompt tokens, or zero chars leaves the previous
// ratio (or the 0.25 fallback) in place, so a bad sample never poisons the
// estimator. systemChars is passed in because the system prompt is not stored in
// the message log but IS counted in promptTokens.
func (h *History) Calibrate(promptTokens, systemChars int) {
	if promptTokens <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	chars := systemChars + len(h.compactSummary)
	for _, m := range h.messages {
		chars += len(m.Content)
		for _, call := range m.ToolCalls {
			chars += len(call.ToolName()) + len(call.ArgumentsJSON())
		}
	}
	if chars <= 0 {
		return
	}
	ratio := float64(promptTokens) / float64(chars)
	if ratio < 0.05 || ratio > 2 {
		return
	}
	h.tokPerChar = ratio
}

func (h *History) snapshotLocked() HistorySnapshot {
	return HistorySnapshot{
		Version:           1,
		CompactSummary:    h.compactSummary,
		Messages:          cloneMessages(h.messages),
		LastTaskStart:     h.lastTaskStart,
		RewriteVersion:    h.rewriteVersion,
		LastWindowStart:   h.lastWindowStart,
		WindowInitialized: h.windowInitialized,
	}
}

func (h *History) saveLocked() {
	if h.persist != nil {
		_ = h.persist(h.snapshotLocked())
	}
}
