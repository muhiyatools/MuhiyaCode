package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// toolEvent is one audited tool-call attempt, from either scope.
type toolEvent struct {
	At            time.Time `json:"at"`
	Scope         string    `json:"scope"` // "main" | "subagent"
	RunID         string    `json:"runId,omitempty"`
	Phase         string    `json:"phase,omitempty"`
	Role          string    `json:"role,omitempty"`
	Tool          string    `json:"tool"`
	Path          string    `json:"path,omitempty"`          // write_file only
	ExistedBefore *bool     `json:"existedBefore,omitempty"` // write_file only
	DuplicateRead bool      `json:"duplicateRead,omitempty"`
}

type phaseTransitionEvent struct {
	At     time.Time `json:"at"`
	Phase  string    `json:"phase"`
	Notice string    `json:"notice"`
}

type phaseAgentEvent struct {
	At               time.Time `json:"at"`
	Kind             string    `json:"kind"`
	RunID            string    `json:"runId"`
	Phase            string    `json:"phase"`
	Role             string    `json:"role"`
	Model            string    `json:"model,omitempty"`
	Status           string    `json:"status,omitempty"`
	HandoffCompliant bool      `json:"handoffCompliant,omitempty"`
}

type pipelineAudit struct {
	Transitions                     []phaseTransitionEvent `json:"transitions"`
	AgentEvents                     []phaseAgentEvent      `json:"agentEvents"`
	RunsByPhase                     map[string]int         `json:"runsByPhase"`
	ResearchRunsBeforeFirstMutation int                    `json:"researchRunsBeforeFirstMutation"`
	PlanFileModifiedAt              *time.Time             `json:"planFileModifiedAt,omitempty"`
	FirstMutationAt                 *time.Time             `json:"firstMutationAt,omitempty"`
	PlanBeforeFirstMutation         *bool                  `json:"planBeforeFirstMutation,omitempty"`
	ValidationRunPresent            bool                   `json:"validationRunPresent"`
	ValidationBeforeFinalAnswer     bool                   `json:"validationBeforeFinalAnswer"`
	HandoffsAudited                 int                    `json:"handoffsAudited"`
	HandoffsCompliant               int                    `json:"handoffsCompliant"`
}

type writeFileAudit struct {
	// TotalCalls counts every attempted write_file call in both scopes.
	TotalCalls int `json:"totalCalls"`
	// OverwritesOfExistingFiles counts attempted write_file calls whose target
	// already existed at call time — the SC-008 whole-file-rewrite metric.
	OverwritesOfExistingFiles int `json:"overwritesOfExistingFiles"`
	// Mechanism documents how the numbers were derived.
	Mechanism string `json:"mechanism"`
}

type duplicateReadAudit struct {
	Total    int `json:"total"`
	Main     int `json:"main"`
	Subagent int `json:"subagent"`
}

type agentRunMeta struct {
	phase, role string
}

type benchmarkObserver struct {
	mu                  sync.Mutex
	auditor             *toolAuditor
	sessionsDir         string
	transitions         []phaseTransitionEvent
	agents              []phaseAgentEvent
	runs                map[string]agentRunMeta
	latestPhase         string
	planAtFirstMutation *time.Time
}

func newBenchmarkObserver(auditor *toolAuditor, sessionsDir string) *benchmarkObserver {
	return &benchmarkObserver{auditor: auditor, sessionsDir: sessionsDir, runs: make(map[string]agentRunMeta)}
}

func (o *benchmarkObserver) observeNotice(notice string) {
	const prefix = "Pipeline phase → "
	if !strings.HasPrefix(notice, prefix) {
		return
	}
	label := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(notice, prefix), ":", 2)[0])
	phase := map[string]string{"planning": "plan", "awaiting approval": "approve", "implementing": "implement", "validating": "validate"}[label]
	if phase == "" {
		phase = label
	}
	event := phaseTransitionEvent{At: time.Now().UTC(), Phase: phase, Notice: notice}
	o.mu.Lock()
	o.latestPhase = phase
	o.transitions = append(o.transitions, event)
	o.mu.Unlock()
}

func (o *benchmarkObserver) observeMainTool(name, arguments string) {
	o.mu.Lock()
	phase := o.latestPhase
	if benchmarkMutationTool(name) && o.planAtFirstMutation == nil {
		o.planAtFirstMutation = latestPlanModTime(o.sessionsDir)
	}
	o.mu.Unlock()
	o.auditor.observe("main", "", phase, "main", name, arguments)
}

func (o *benchmarkObserver) observeAgent(event contract.AgentEvent) {
	now := time.Now().UTC()
	o.mu.Lock()
	meta := o.runs[event.RunID]
	if event.Kind == "start" {
		meta = agentRunMeta{role: event.Role}
		o.runs[event.RunID] = meta
		o.agents = append(o.agents, phaseAgentEvent{
			At: now, Kind: event.Kind, RunID: event.RunID, Role: event.Role, Model: event.Model,
			HandoffCompliant: handoffCompliant(event.Handoff),
		})
	} else if event.Kind == "done" {
		if event.Role != "" {
			meta.role = event.Role
		}
		o.agents = append(o.agents, phaseAgentEvent{At: now, Kind: event.Kind, RunID: event.RunID, Role: meta.role, Status: event.Status})
	}
	if event.Kind == "tool_start" && benchmarkMutationTool(event.Tool) && o.planAtFirstMutation == nil {
		o.planAtFirstMutation = latestPlanModTime(o.sessionsDir)
	}
	o.mu.Unlock()
	if event.Kind == "tool_start" {
		o.auditor.observe("subagent", event.RunID, meta.phase, meta.role, event.Tool, event.Arguments)
	}
}

func handoffCompliant(handoff string) bool {
	for _, field := range []string{"Role:", "Scope:", "Context:", "Deliverable:", "OutputFormat:"} {
		if !strings.Contains(handoff, field) {
			return false
		}
	}
	return true
}

func benchmarkMutationTool(name string) bool {
	return name == "edit_file" || name == "multi_edit" || name == "write_file" || name == "apply_patch" || name == "run_shell" || name == "save_memory" || strings.HasPrefix(name, "mcp__")
}

func latestPlanModTime(sessionsDir string) *time.Time {
	var latest time.Time
	_ = filepath.WalkDir(sessionsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "plan.md" {
			return err
		}
		if info, statErr := entry.Info(); statErr == nil && info.ModTime().After(latest) {
			latest = info.ModTime().UTC()
		}
		return nil
	})
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func (o *benchmarkObserver) summarize(audit *pipelineAudit, sessionsDir string, finishedAt time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	audit.Transitions = append([]phaseTransitionEvent(nil), o.transitions...)
	audit.AgentEvents = append([]phaseAgentEvent(nil), o.agents...)
	audit.RunsByPhase = make(map[string]int)
	firstMutation := o.auditor.firstMutationAt()
	for _, event := range o.agents {
		if event.Kind == "start" {
			audit.RunsByPhase[event.Phase]++
			audit.HandoffsAudited++
			if event.HandoffCompliant {
				audit.HandoffsCompliant++
			}
		}
		if event.Kind == "done" && event.Phase == "research" && (firstMutation == nil || event.At.Before(*firstMutation)) {
			audit.ResearchRunsBeforeFirstMutation++
		}
		if event.Kind == "done" && event.Phase == "validate" && event.Status == "done" {
			audit.ValidationRunPresent = true
			audit.ValidationBeforeFinalAnswer = event.At.Before(finishedAt)
		}
	}
	audit.FirstMutationAt = firstMutation
	planTime := o.planAtFirstMutation
	if planTime == nil {
		planTime = latestPlanModTime(sessionsDir)
	}
	audit.PlanFileModifiedAt = planTime
	if planTime != nil && firstMutation != nil {
		before := !planTime.After(*firstMutation)
		audit.PlanBeforeFirstMutation = &before
	}
}

// readonlyAuditTools mirrors the workspace read-only tool set for the
// duplicate-read audit. MCP tools are excluded (unknown semantics).
var readonlyAuditTools = map[string]bool{
	"read_file": true, "list_files": true, "grep": true,
	"search_text": true, "glob": true, "git_status": true, "git_diff": true,
}

// toolAuditor collects attempted tool calls from both dispatch scopes. The
// callbacks fire synchronously before the tool executes, so a write_file
// target stat taken here reflects pre-write existence. Subagents can run
// concurrently, hence the mutex.
type toolAuditor struct {
	mu        sync.Mutex
	workspace string
	events    []toolEvent
	seenReads map[string]int
}

func newToolAuditor(workspace string) *toolAuditor {
	return &toolAuditor{workspace: workspace, seenReads: map[string]int{}}
}

func (a *toolAuditor) observe(scope, runID, phase, role, tool, arguments string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	event := toolEvent{At: time.Now().UTC(), Scope: scope, RunID: runID, Phase: phase, Role: role, Tool: tool}
	if tool == "write_file" {
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(arguments), &args)
		if args.Path != "" {
			event.Path = filepath.ToSlash(args.Path)
			if target := a.resolve(args.Path); target != "" {
				existed := fileExists(target)
				event.ExistedBefore = &existed
			}
		}
	}
	if readonlyAuditTools[tool] {
		signature := tool + "\x00" + arguments
		a.seenReads[signature]++
		if a.seenReads[signature] > 1 {
			event.DuplicateRead = true
		}
	}
	a.events = append(a.events, event)
}

func (a *toolAuditor) firstMutationAt() *time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, event := range a.events {
		if benchmarkMutationTool(event.Tool) {
			at := event.At
			return &at
		}
	}
	return nil
}

// resolve maps a tool-call path onto the benchmark workspace. Absolute paths
// outside the workspace are ignored (they cannot be fixture overwrites).
func (a *toolAuditor) resolve(path string) string {
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if !filepath.IsAbs(cleaned) {
		return filepath.Join(a.workspace, cleaned)
	}
	if within(a.workspace, cleaned) {
		return cleaned
	}
	return ""
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (a *toolAuditor) summarize(result *runResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result.ToolEvents = append([]toolEvent(nil), a.events...)
	for _, event := range a.events {
		if event.Scope == "subagent" {
			result.SubagentToolCalls++
		} else {
			result.MainToolCalls++
		}
		if event.Tool == "write_file" {
			result.WriteFileAudit.TotalCalls++
			if event.ExistedBefore != nil && *event.ExistedBefore {
				result.WriteFileAudit.OverwritesOfExistingFiles++
			}
		}
		if event.DuplicateRead {
			result.DuplicateReads.Total++
			if event.Scope == "subagent" {
				result.DuplicateReads.Subagent++
			} else {
				result.DuplicateReads.Main++
			}
		}
	}
	result.ToolCalls = result.MainToolCalls + result.SubagentToolCalls
	result.WriteFileAudit.Mechanism = "read-only callbacks (Callbacks.ToolStart + AgentEvent tool_start) fired by the shared dispatch gate before execution; target existence stat'd in the benchmark workspace at call time; counts attempted calls in both scopes"
}

// hashTree fingerprints every regular file under root (path -> sha256).
func hashTree(root string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(relative)] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	return hashes, err
}

// diffTrees returns added/modified paths and deleted paths, sorted.
func diffTrees(before, after map[string]string) (changed, deleted []string) {
	for path, hash := range after {
		if previous, ok := before[path]; !ok || previous != hash {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	sort.Strings(changed)
	sort.Strings(deleted)
	return changed, deleted
}
