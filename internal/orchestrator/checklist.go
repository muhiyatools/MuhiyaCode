package orchestrator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The workspace checklist (NATIVE_AGENT_PLAN.md §4). The model keeps a plain
// markdown checklist at the workspace root and edits it with ordinary file
// tools; the harness never authors it. Whenever a tool call writes that file
// the engine re-parses it and pushes the result to the UI, so the live to-do
// panel and the completion-honesty guard both read one source of truth.
//
// Deliberately NOT in the system prompt: the checklist changes constantly, and
// injecting it into the cached prefix would invalidate the cache every turn.
// The model reads it on demand like any other file.

// ChecklistFileName is the workspace-root file the model maintains.
const ChecklistFileName = "tasks.md"

// checklistLineRE matches a GitHub-style checklist item. The status suffix is
// OPTIONAL: an unchecked box is pending, a checked box is completed, and an
// explicit "(in progress)" marker overrides to in-progress. Requiring a status
// parenthetical (as the old plan.md parser did) would mean a plain checklist —
// exactly what the instructions teach the model to write — parsed to nothing.
var checklistLineRE = regexp.MustCompile(`^[-*]\s+\[([ xX])\]\s+(.+?)\s*$`)

// inProgressSuffixRE strips a trailing status marker from an item title and
// reports whether it named in-progress. Both "(in progress)" and the legacy
// "(in_progress)" are accepted; "(pending)" and "(completed)" are tolerated as
// redundant with the checkbox.
var inProgressSuffixRE = regexp.MustCompile(`(?i)\s*\((in[ _]progress|pending|completed|done)\)$`)

// ParseChecklist reads a markdown checklist into the shared Plan carrier. Lines
// that are not checklist items become the note, so surrounding prose survives a
// round trip. An empty or item-less file yields a zero Plan.
func ParseChecklist(content string) contract.Plan {
	var plan contract.Plan
	var notes []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		match := checklistLineRE.FindStringSubmatch(trimmed)
		if len(match) != 3 {
			if trimmed != "" {
				notes = append(notes, trimmed)
			}
			continue
		}
		title := match[2]
		status := contract.PlanPending
		if strings.EqualFold(match[1], "x") {
			status = contract.PlanCompleted
		}
		if marker := inProgressSuffixRE.FindStringSubmatch(title); len(marker) == 2 {
			title = strings.TrimSpace(inProgressSuffixRE.ReplaceAllString(title, ""))
			if lower := strings.ToLower(marker[1]); lower == "in progress" || lower == "in_progress" {
				status = contract.PlanInProgress
			}
		}
		if title == "" {
			continue
		}
		plan.Steps = append(plan.Steps, contract.PlanStep{Title: title, Status: status})
	}
	if len(plan.Steps) > 0 {
		plan.Note = strings.Join(notes, "\n")
		plan.UpdatedAt = time.Now().UTC()
	}
	return plan
}

// checklistPath is the absolute path of the ACTIVE checklist: the tasks.md most
// recently written in this session, or the workspace-root one when none has been
// written yet. It is "" when the session has no workspace.
//
// TA01: this used to be hard-wired to the workspace root while the WRITE side
// (touchesChecklist) already accepted a tasks.md in any directory. A checklist
// created beside the work — the natural place, and now what the prompt asks for —
// therefore passed the role gate, landed on disk, and was never read again: the
// to-do panel stayed empty and the completion-honesty guard went blind.
func (e *Engine) checklistPath() string {
	if strings.TrimSpace(e.session.WorkspacePath) == "" {
		return ""
	}
	e.mu.Lock()
	active := e.activeChecklistPath
	e.mu.Unlock()
	if active != "" {
		return active
	}
	return filepath.Join(e.session.WorkspacePath, ChecklistFileName)
}

// adoptChecklistPath remembers the tasks.md a successful write just targeted, so
// later refreshes read THAT file. Relative paths resolve against the workspace
// root exactly as the file tools resolve them.
//
// Not persisted across a restart on purpose: a resumed session falls back to the
// workspace root until the next checklist write re-establishes the location,
// which costs one write and keeps zero new session state to migrate.
func (e *Engine) adoptChecklistPath(call contract.ToolCall) {
	root := strings.TrimSpace(e.session.WorkspacePath)
	if root == "" {
		return
	}
	for _, path := range contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON())) {
		if !strings.EqualFold(filepath.Base(filepath.Clean(path)), ChecklistFileName) {
			continue
		}
		resolved := path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(root, resolved)
		}
		e.mu.Lock()
		e.activeChecklistPath = filepath.Clean(resolved)
		e.mu.Unlock()
		return
	}
}

// touchesChecklist reports whether a successful tool call wrote the workspace
// checklist. Paths are compared case-insensitively on their cleaned form so a
// relative "tasks.md", an absolute path, and Windows case variants all match.
func (e *Engine) touchesChecklist(call contract.ToolCall) bool {
	paths := contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON()))
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if strings.EqualFold(filepath.Base(filepath.Clean(path)), ChecklistFileName) {
			return true
		}
	}
	return false
}

// refreshChecklist re-reads the workspace checklist and publishes it. Called
// from the shared dispatch gate after any successful mutation that touched the
// file, so an edit from the main loop AND one from a subagent both land. A
// missing or unreadable file clears the panel rather than erroring: the
// checklist is the model's artifact, not harness state.
func (e *Engine) refreshChecklist() {
	path := e.checklistPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	plan := contract.Plan{}
	if err == nil {
		plan = ParseChecklist(string(data))
	}
	e.mu.Lock()
	changed := !sameChecklist(e.checklist, plan)
	e.checklist = plan
	e.mu.Unlock()
	// Only publish real changes: a re-read that finds the same items (or no
	// checklist at all, as at session open in a project that has none) must not
	// wake the UI.
	if changed && e.callbacks.PlanUpdate != nil {
		e.callbacks.PlanUpdate(plan)
	}
}

// sameChecklist compares the item list only — the note and timestamp are
// derived, so they must not trigger a spurious update.
func sameChecklist(a, b contract.Plan) bool {
	if len(a.Steps) != len(b.Steps) {
		return false
	}
	for i := range a.Steps {
		if a.Steps[i] != b.Steps[i] {
			return false
		}
	}
	return true
}

// openChecklistItems returns the titles of items still open, for the
// completion-honesty disclosure.
func (e *Engine) openChecklistItems() []string {
	e.mu.Lock()
	steps := e.checklist.Steps
	e.mu.Unlock()
	var open []string
	for _, step := range steps {
		if step.Status != contract.PlanCompleted {
			open = append(open, step.Title)
		}
	}
	return open
}
