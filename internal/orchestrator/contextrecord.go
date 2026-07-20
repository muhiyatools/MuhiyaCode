package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 012 (contracts/context-linking.md, data-model.md): the durable
// artifact of a completed subagent run. Stored VERBATIM — the transcript's
// bytes are what continuation replays, so nothing here may be re-rendered,
// redacted, or repaired after capture (R-D1/R-D3). Records persist as
// session sidecars under agents/<runID>.json and live in memory for the
// session's lifetime; absence of records degrades to pre-012 behavior.

// Terminal shapes (R-D4). Only clean-done runs are stream-continuable:
// wrap-up and ceiling transcripts end with injected budget messages and
// unexecuted-tool refusals that would teach a successor wrong behavior.
const (
	terminalCleanDone      = "clean-done"
	terminalWrapupDone     = "wrapup-done"
	terminalPartialCeiling = "partial-ceiling"
	terminalFailed         = "failed"
	terminalCancelled      = "cancelled"
)

// FileFingerprint is the staleness primitive: mtime+size captured by one
// re-stat pass at run COMPLETION (not read time), so a run's own edits are
// never "stale" for its successor (R-D5).
type FileFingerprint struct {
	MTimeMS float64 `json:"mtimeMs"`
	Size    int64   `json:"size"`
}

// ReadWriteSet is the run's touched-file evidence. Keys are workspace-relative,
// slash-normalized, Windows-lowercased (the inspection-ledger convention — the
// single normalization for the feature).
type ReadWriteSet struct {
	Reads  map[string]FileFingerprint `json:"reads,omitempty"`
	Writes map[string]FileFingerprint `json:"writes,omitempty"`
}

func (t ReadWriteSet) touchedPaths() []string {
	seen := make(map[string]bool, len(t.Reads)+len(t.Writes))
	var paths []string
	for path := range t.Reads {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	for path := range t.Writes {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

// SubagentContextRecord is one completed run's continuation material.
type SubagentContextRecord struct {
	RunID   string `json:"runId"`
	Kind    string `json:"kind"`
	ModelID string `json:"modelId"`
	Pin     string `json:"pin"`
	// Transcript holds the run's messages exactly as constructed (raw
	// ReasoningDetails preserved by contract.Message's json.RawMessage field).
	Transcript    []contract.Message `json:"transcript"`
	TerminalShape string             `json:"terminalShape"`
	// SystemHash/ToolsHash anchor the cross-boundary prefix guard (CL-2): a
	// continuation whose recomputed shape differs aborts to digest fallback.
	SystemHash string `json:"systemHash"`
	ToolsHash  string `json:"toolsHash"`
	// Reasoning is the effort tier the run's requests used; EffortPinned
	// profiles make continuations inherit it (R-D12/P3).
	Reasoning             contract.ReasoningTier `json:"reasoning,omitempty"`
	FinalPromptTokens     int                    `json:"finalPromptTokens"`
	FinalCompletionTokens int                    `json:"finalCompletionTokens"`
	Touched               ReadWriteSet           `json:"touched"`
	Result                string                 `json:"result"`
	// TaskLineage identifies the task ordinal that dispatched this run, so
	// same-task phase chains and session follow-ups are distinguishable.
	TaskLineage int    `json:"taskLineage"`
	Linkable    bool   `json:"linkable"`
	LinkNote    string `json:"linkNote,omitempty"`
	CompletedAt int64  `json:"completedAt"`
}

// agentRunCapture collects the per-run read/write evidence while the run
// executes (dispatchScope hooks) and is folded into the record at finalize.
type agentRunCapture struct {
	workspace string
	reads     map[string]bool
	writes    map[string]bool
}

func newAgentRunCapture(workspace string) *agentRunCapture {
	return &agentRunCapture{workspace: workspace, reads: map[string]bool{}, writes: map[string]bool{}}
}

// note records one successful tool call's touched path(s). Only tools with an
// explicit path argument attribute; apply_patch and shell mutations stay
// opaque (their file effects are covered by the completion-time re-stat of
// paths the run also read, and external-change detection never depended on
// them). read_file feeds reads; edit_file/multi_edit/write_file feed writes.
func (c *agentRunCapture) note(name, argumentsJSON string) {
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(argumentsJSON), &input) != nil {
		return
	}
	path := agentPathKey(c.workspace, input.Path)
	if path == "" {
		return
	}
	switch name {
	case "read_file":
		c.reads[path] = true
	case "edit_file", "multi_edit", "write_file":
		c.writes[path] = true
	}
}

// touchedCount is the run's progress signal: how many distinct paths it has
// read or written so far. Only SUCCESSFUL calls reach note(), so this counts
// evidence gathered and work landed, never attempts. The turn ladder extends a
// run whose count is still growing and wraps up one whose count has stalled.
func (c *agentRunCapture) touchedCount() int {
	if c == nil {
		return 0
	}
	return len(c.reads) + len(c.writes)
}

// statFingerprint is the ONE staleness primitive: the current mtime+size of a
// normalized touched-set path, zero when the file is absent (a later existence
// change still reads as "changed"). Capture, staleness comparison, and tests
// all share it so the fingerprint semantics cannot drift.
func statFingerprint(workspace, path string) FileFingerprint {
	info, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path)))
	if err != nil {
		return FileFingerprint{}
	}
	return FileFingerprint{MTimeMS: float64(info.ModTime().UnixNano()) / 1e6, Size: info.Size()}
}

// finalize performs the single end-of-run stat pass (R-D5) and returns the
// touched set with completion-time fingerprints.
func (c *agentRunCapture) finalize() ReadWriteSet {
	set := ReadWriteSet{}
	if len(c.reads) > 0 {
		set.Reads = make(map[string]FileFingerprint, len(c.reads))
		for path := range c.reads {
			set.Reads[path] = statFingerprint(c.workspace, path)
		}
	}
	if len(c.writes) > 0 {
		set.Writes = make(map[string]FileFingerprint, len(c.writes))
		for path := range c.writes {
			set.Writes[path] = statFingerprint(c.workspace, path)
		}
	}
	return set
}

// agentPathKey normalizes a tool-call path to the feature's single key form:
// workspace-relative when inside the workspace, slash-normalized, lowercased
// on Windows (matching the inspection ledger's convention).
func agentPathKey(workspace, raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if filepath.IsAbs(path) && workspace != "" {
		if rel, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// transcriptPairingsComplete verifies every assistant tool_calls entry has a
// following tool result for each announced ID (R-F8): a record failing this
// would be repaired at replay with synthetic bytes, breaking the prefix — so
// it is stored non-linkable instead.
func transcriptPairingsComplete(messages []contract.Message) bool {
	answered := make(map[string]bool)
	for _, message := range messages {
		if message.Role == contract.RoleTool && message.ToolCallID != "" {
			answered[message.ToolCallID] = true
		}
	}
	for _, message := range messages {
		if message.Role != contract.RoleAssistant {
			continue
		}
		for _, call := range message.ToolCalls {
			if call.ID != "" && !answered[call.ID] {
				return false
			}
		}
	}
	return true
}

// parseReportTrailer (feature 012 PH-4) leniently extracts the structured
// trailer lines from a subagent report: `Changed: a.go, b.go`,
// `Verified: ...`, `CarryForward: ...`. Absence of any line degrades to
// today's prose handling — never an error. Changed paths supplement the
// capture's write set so shell-driven edits the run reported still join the
// staleness evidence.
func parseReportTrailer(report string) (changed []string, verified, carry string) {
	for _, line := range strings.Split(report, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "changed:"):
			for _, part := range strings.Split(trimmed[len("changed:"):], ",") {
				if path := strings.TrimSpace(part); path != "" && !strings.EqualFold(path, "none") {
					changed = append(changed, path)
				}
			}
		case strings.HasPrefix(lower, "verified:"):
			verified = strings.TrimSpace(trimmed[len("verified:"):])
		case strings.HasPrefix(lower, "carryforward:"):
			carry = strings.TrimSpace(trimmed[len("carryforward:"):])
		}
	}
	return changed, verified, carry
}

// terminalShapeFor derives the run's terminal shape from its outcome flags
// (R-D4; fixes the R-F16 hazard where turn-exhausted wrap-up runs were
// indistinguishable from clean completions in every durable record).
func terminalShapeFor(status string, wrapUpEntered, ceilingHit bool) string {
	switch {
	case status == "cancelled":
		return terminalCancelled
	case status == "failed":
		return terminalFailed
	case ceilingHit:
		return terminalPartialCeiling
	case wrapUpEntered:
		return terminalWrapupDone
	default:
		return terminalCleanDone
	}
}

// finalizeAgentRecord builds, stores, and persists the run's context record.
// Called once per completed run (any terminal shape); persistence is
// best-effort (a write failure leaves the in-memory record usable).
func (e *Engine) finalizeAgentRecord(ctx context.Context, record *SubagentContextRecord, capture *agentRunCapture) {
	if record == nil {
		return
	}
	if capture != nil {
		// PH-4: report-trailer Changed paths supplement the observed write set
		// (covers shell-driven edits the recorder cannot attribute).
		trailerChanged, _, _ := parseReportTrailer(record.Result)
		for _, path := range trailerChanged {
			if key := agentPathKey(capture.workspace, path); key != "" {
				capture.writes[key] = true
			}
		}
		record.Touched = capture.finalize()
	}
	record.CompletedAt = time.Now().UnixMilli()
	record.Linkable = record.TerminalShape == terminalCleanDone
	if !record.Linkable {
		record.LinkNote = "terminal-shape:" + record.TerminalShape
	}
	if record.Linkable && !transcriptPairingsComplete(record.Transcript) {
		record.Linkable = false
		record.LinkNote = "incomplete-tool-pairings"
	}
	e.taskMu.Lock()
	if e.agentRecords == nil {
		e.agentRecords = make(map[string]*SubagentContextRecord)
	}
	e.agentRecords[record.RunID] = record
	e.agentRecordOrder = append(e.agentRecordOrder, record.RunID)
	e.taskMu.Unlock()
	if e.persistence.WriteAgentRecord != nil {
		_ = e.persistence.WriteAgentRecord(ctx, record.RunID, record)
	}
}

// latestLinkableRecord returns the most recent linkable record matching the
// filter, or nil. Order is completion order (oldest first in the slice).
func (e *Engine) latestLinkableRecord(match func(*SubagentContextRecord) bool) *SubagentContextRecord {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	for index := len(e.agentRecordOrder) - 1; index >= 0; index-- {
		record := e.agentRecords[e.agentRecordOrder[index]]
		if record != nil && record.Linkable && match(record) {
			return record
		}
	}
	return nil
}

// RestoreAgentRecords loads persisted records at session resume (bounded by
// the store's cap). Invalid entries are skipped — a corrupt sidecar never
// blocks resume (FR-017).
func (e *Engine) RestoreAgentRecords(raw [][]byte) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.agentRecords == nil {
		e.agentRecords = make(map[string]*SubagentContextRecord)
	}
	restored := make([]*SubagentContextRecord, 0, len(raw))
	for _, data := range raw {
		var record SubagentContextRecord
		if json.Unmarshal(data, &record) != nil || record.RunID == "" || len(record.Transcript) == 0 {
			continue
		}
		restored = append(restored, &record)
	}
	// Oldest first so agentRecordOrder keeps completion order.
	sort.Slice(restored, func(i, j int) bool { return restored[i].CompletedAt < restored[j].CompletedAt })
	for _, record := range restored {
		if _, exists := e.agentRecords[record.RunID]; exists {
			continue
		}
		e.agentRecords[record.RunID] = record
		e.agentRecordOrder = append(e.agentRecordOrder, record.RunID)
	}
}
