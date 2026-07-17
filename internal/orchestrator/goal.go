package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// Goal is a durable objective the agent works toward across multiple turns,
// adapted from Reasonix's goal machine. The goal text rides on the user
// message (never the system prompt), so activating or clearing a goal does not
// disturb the cache-stable prefix. Each turn the model must end with exactly
// one control marker; the engine reads it to decide whether to auto-continue.
type Goal struct {
	Text                string
	Status              GoalStatus
	AutoTurns           int // resets at each task boundary (G5)
	IdleTurns           int // (G5/B4) CONSECUTIVE auto-continues with no tool calls
	MissingMarkerStreak int // (B4) consecutive replies with no goal marker
	Blocked             string
	LastMarker          string
}

type GoalStatus string

const (
	GoalActive   GoalStatus = "active"
	GoalComplete GoalStatus = "complete"
	GoalBlocked  GoalStatus = "blocked"
)

// maxGoalAutoTurns bounds autonomous continuation so a goal can never loop
// forever without the user. The cap is per task (G5) now, not per goal.
const maxGoalAutoTurns = 12

// maxGoalIdleTurns (G5): consecutive auto-continues that produced zero tool
// calls. When a model is done or stuck, continuing burns tokens without
// forward motion.
const maxGoalIdleTurns = 2

var goalMarkerRE = regexp.MustCompile(`(?i)\[goal:(continue|complete|blocked)(?::\s*([^\]]*))?\]`)

// SetGoal activates a new objective, replacing any previous one. When a goal is
// set while plan mode is active, G3 forces plan mode off (cache-safe at the
// tail, since the plan block is appended on a per-Read brief). The returned
// notice is for the TUI to display — it carries the diff so the user sees
// what changed. G4: the active-goal sidecar is persisted after the mode lock
// is released so a slow disk sync never blocks mode transitions.
func (e *Engine) SetGoal(text string) string {
	text = strings.TrimSpace(text)
	e.modeMu.Lock()
	notice := ""
	active := false
	planCleared := false
	if text == "" {
		notice = e.clearGoalLocked("")
		e.goal = nil
	} else if st := e.lifecycle.State; st.IsActive() && !st.IsReadOnly() && !st.IsTerminal() {
		// DG2: a goal must not fight an in-flight plan. When the lifecycle is
		// pipeline-active or awaiting a proceed (pending / implementing / validating /
		// interrupted — anything active that is neither the read-only planning phase
		// G3 takes over, nor a terminal state), refuse and tell the user to resolve
		// the plan first. Read-only planning is handled by the G3 takeover below.
		e.modeMu.Unlock()
		return "Finish, proceed, or discard the current plan before setting a goal."
	} else {
		// G6: a replaced-active-goal notice shows before/after.
		notice = e.setGoalLocked(text)
		active = true
		// G3: enforcing plan⇄goal mutual exclusion. Plan wins on tie-breaks, but a
		// goal activation is the user's intent — it takes over from a read-only
		// plan-mode lifecycle, abandoning the draft to direct work so the read-only
		// gate and the plan block are released (UL-13).
		if e.lifecycle.State.IsReadOnly() {
			e.lifecycle = Lifecycle{State: contract.LifecycleDirect}
			planCleared = true
			notice += " (Plan mode disabled — goal mode is now active.)"
		}
	}
	e.modeMu.Unlock()
	if active {
		e.persistGoalState()
	} else {
		// Clearing via SetGoal("") drops the sidecar so the goal does not
		// resurrect on resume.
		e.clearGoalSidecar()
	}
	if planCleared {
		e.persistPlanState()
	}
	return notice
}

// ClearGoal removes the active objective and drops the persisted sidecar (G4).
func (e *Engine) ClearGoal() {
	e.modeMu.Lock()
	e.clearGoalLocked("")
	e.goal = nil
	e.modeMu.Unlock()
	e.clearGoalSidecar()
}

// reconcileGoalOnTaskEnd (Ultimate Polish DG1) blocks a goal that is still active
// when a task ends — the H5 failure terminator, the hard turn ceiling, or any exit
// that bypasses the marker-based resolution — and drops its sidecar, so an abandoned
// goal can never resurrect as active on the next task or on resume. A goal that
// already resolved (e.goal == nil) is a no-op, so a normal completion is untouched.
func (e *Engine) reconcileGoalOnTaskEnd() {
	e.modeMu.Lock()
	if e.goal == nil {
		e.modeMu.Unlock()
		return
	}
	e.goal.Status = GoalBlocked
	e.goal.Blocked = "the task ended before the goal reported completion"
	e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalBlocked, Blocked: e.goal.Blocked}
	e.goal = nil
	e.modeMu.Unlock()
	e.clearGoalSidecar()
}

// GoalSnapshot returns a copy of the current goal, plus a "last result" tomb
// if the user asked for an inactive state (G2). The active=true flag in the
// return distinguishes "is currently active" from "finished long ago".
func (e *Engine) GoalSnapshot() (Goal, bool) {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal == nil {
		return Goal{}, false
	}
	return *e.goal, true
}

// LastGoalResult returns the tombstone of the most recent completed or blocked
// goal so /goal status without args can surface it (G2). The bool is "a
// tombstone exists" (present), not "active" — a tombstone is never active by
// definition; callers distinguish active vs. finished via GoalSnapshot first.
func (e *Engine) LastGoalResult() (Goal, bool) {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.lastGoal == nil {
		return Goal{}, false
	}
	return *e.lastGoal, true
}

// setGoalLocked carries the previous-goal-before-replacement logic.
func (e *Engine) setGoalLocked(text string) string {
	notice := ""
	if e.goal != nil && e.goal.Status == GoalActive {
		notice = fmt.Sprintf("Replaced active goal: %s \u2192 %s", contract.Digest(e.goal.Text, 200), contract.Digest(text, 200))
	}
	e.goal = &Goal{Text: text, Status: GoalActive}
	return notice
}

// clearGoalLocked moves e.goal into the tombstone slot and returns a notice
// when there was something to clear.
func (e *Engine) clearGoalLocked(reason string) string {
	if e.goal == nil {
		return ""
	}
	status, text, blocked := e.goal.Status, e.goal.Text, e.goal.Blocked
	e.lastGoal = &Goal{Text: text, Status: status, Blocked: blocked}
	e.goal = nil
	if status == GoalActive {
		return "Goal cleared."
	}
	if status == GoalComplete {
		return "Goal complete: " + contract.Digest(text, 200)
	}
	if status == GoalBlocked {
		if reason != "" {
			reason = "; " + reason
		}
		if blocked != "" {
			blocked = " " + blocked + reason
		}
		return "Goal blocked: " + contract.Digest(text, 200) + blocked
	}
	return ""
}

// PlanMode reports whether the lifecycle is in a read-only (plan-mode) state —
// research, planning, or awaiting-approval. It is derived from the single
// lifecycle state, so it can never disagree with the gate. Plan⇄goal exclusion
// (G3) moves the lifecycle out of read-only when a goal is set.
func (e *Engine) PlanMode() bool {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return e.lifecycle.State.IsReadOnly()
}

// Ultimate Polish P1: SetPlanMode is removed. Planning is entered ONLY through the
// classifier pipeline (beginPipeline → transitionLifecycle); there is no manual
// plan toggle. The plan⇄goal exclusion survives via SetGoal (a goal takes over a
// read-only planning phase) and the DG2 brief backstop (a plan/pipeline block drops
// the goal block). PlanMode() below still reports the read-only planning phase for
// the footer badge and the plan-mode instruction gating.

// goalBlock is the per-turn instruction block appended to the task brief while
// a goal is active. It rides on the user message so toggling it does not
// disturb the cache-stable prefix.
func (e *Engine) goalBlock() string {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal == nil || e.goal.Status != GoalActive {
		return ""
	}
	return "[active-goal: " + contract.Digest(e.goal.Text, 200) + "]\n" + instructions.GoalBlockInstructionBody
}

// scanGoalMarker parses a single goal control marker and, if it is a terminal
// one (complete/blocked), applies the full terminal transition: status flip,
// G2 tombstone, and e.goal = nil. It does NOT advance AutoTurns / emit
// continuation prompts \u2014 those remain the responsibility of advanceGoal in
// the no-tool branch. (G1.) Returns true when a terminal transition fired so
// callers can persist the sidecar clear (G4) exactly once per transition.
func (e *Engine) scanGoalMarker(text string) bool {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal == nil || e.goal.Status != GoalActive {
		return false
	}
	marker := goalMarkerRE.FindStringSubmatch(text)
	if len(marker) == 0 {
		return false
	}
	verb := strings.ToLower(marker[1])
	switch verb {
	case "complete":
		e.goal.Status = GoalComplete
		e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalComplete}
		e.goal = nil
		return true
	case "blocked":
		reason := ""
		if len(marker) > 2 && marker[2] != "" {
			reason = strings.TrimSpace(marker[2])
		}
		e.goal.Status = GoalBlocked
		e.goal.Blocked = reason
		e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalBlocked, Blocked: reason}
		e.goal = nil
		return true
	}
	return false
}

// StripGoalMarkers removes a goal control marker from text without changing
// the marker-bearing assistant message persisted to history. (G1, mirrors
// Reasonix goal_display.go:12-32.)
func StripGoalMarkers(text string) string {
	return goalMarkerRE.ReplaceAllString(text, "")
}

// advanceGoal reads the control marker from the final assistant text and
// decides whether the engine should auto-continue. (G1, G5)
func (e *Engine) advanceGoal(finalText string, madeToolCall bool) string {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal == nil || e.goal.Status != GoalActive {
		return ""
	}
	marker := goalMarkerRE.FindStringSubmatch(finalText)
	verb := "continue"
	reason := ""
	if len(marker) > 0 {
		verb = strings.ToLower(marker[1])
		if len(marker) > 2 {
			reason = strings.TrimSpace(marker[2])
		}
		e.goal.LastMarker = verb
	}
	switch verb {
	case "complete":
		e.goal.Status = GoalComplete
		e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalComplete}
		e.goal = nil
		return ""
	case "blocked":
		e.goal.Status = GoalBlocked
		e.goal.Blocked = reason
		e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalBlocked, Blocked: reason}
		e.goal = nil
		return ""
	}
	// Default: no marker OR explicit continue. (madeToolCall is always false at
	// the sole call site — this branch only runs on a no-tool turn; IdleTurns is
	// reset to 0 by markGoalToolProgress on any turn that DID make tool calls, so
	// it stays a CONSECUTIVE count. B4.)
	_ = madeToolCall
	hadMarker := len(marker) > 0
	if hadMarker {
		e.goal.MissingMarkerStreak = 0
	} else {
		e.goal.MissingMarkerStreak++
		if e.goal.MissingMarkerStreak >= 2 {
			// B4: two consecutive replies with no marker — stop auto-continuing and
			// hand back to the user instead of force-continuing to the cap.
			e.goal.Status = GoalBlocked
			e.goal.Blocked = "no goal marker on consecutive replies — stopping to ask"
			e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalBlocked, Blocked: e.goal.Blocked}
			e.goal = nil
			return ""
		}
	}
	e.goal.IdleTurns++
	if e.goal.IdleTurns >= maxGoalIdleTurns {
		e.goal.Status = GoalBlocked
		e.goal.Blocked = "no forward motion across the last autonomous turns"
		e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalBlocked, Blocked: e.goal.Blocked}
		e.goal = nil
		return ""
	}
	e.goal.AutoTurns++
	if e.goal.AutoTurns >= maxGoalAutoTurns {
		e.goal.Status = GoalBlocked
		e.goal.Blocked = "reached the autonomous-turn limit"
		e.lastGoal = &Goal{Text: e.goal.Text, Status: GoalBlocked, Blocked: e.goal.Blocked}
		e.goal = nil
		return ""
	}
	prompt := fmt.Sprintf("[goal] Continue toward the active goal: %s\nKeep going without asking; end with a goal marker.", contract.Digest(e.goal.Text, 200))
	if !hadMarker {
		// B4: nudge on the first missing-marker reply (streak == 1).
		prompt += "\nEnd your reply with exactly one goal marker."
	}
	return prompt
}

// markGoalToolProgress (B4) resets the goal's consecutive-idle counter when a
// turn made tool calls, so IdleTurns counts CONSECUTIVE no-tool auto-continues
// rather than a lifetime total. A productive tool turn between two text replies
// must not push the goal toward the idle stop.
func (e *Engine) markGoalToolProgress() {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal != nil && e.goal.Status == GoalActive {
		e.goal.IdleTurns = 0
	}
}

// RestoredGoalNotice (G4) returns the one-shot notice surfaced when an active
// goal was restored from the goal.json sidecar on session resume, then clears
// it so it is shown only once. The application/TUI reads this at startup and
// after a mid-session resume.
func (e *Engine) RestoredGoalNotice() string {
	e.modeMu.Lock()
	notice := e.restoredGoalNotice
	e.restoredGoalNotice = ""
	e.modeMu.Unlock()
	return notice
}

// persistGoalState (G4) writes the active-goal sidecar off the hot path. It
// captures the current goal snapshot under modeMu, then writes (or clears) the
// sidecar under a separate writeMu so a slow disk sync never blocks modeMu.
// No-op when no persistence hooks are wired (unit-test engines).
func (e *Engine) persistGoalState() {
	snapshot, active := e.goalSnapshotForPersist()
	if !active {
		e.clearGoalSidecar()
		return
	}
	e.writeGoalSidecar(snapshot)
}

// goalSnapshotForPersist reads the active goal under modeMu and returns its
// persisted shape. Only active goals are snapshotted; terminal/cleared goals
// return active=false so the caller clears the sidecar.
func (e *Engine) goalSnapshotForPersist() (contract.GoalSnapshot, bool) {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal == nil || e.goal.Status != GoalActive {
		return contract.GoalSnapshot{}, false
	}
	return contract.GoalSnapshot{Text: e.goal.Text, Status: string(e.goal.Status), AutoTurns: e.goal.AutoTurns, Blocked: e.goal.Blocked}, true
}

// writeGoalSidecar serializes the sidecar write under writeMu and swallows the
// error: a failed goal persist must never break a goal activation — the
// in-memory goal stays active regardless of disk state.
func (e *Engine) writeGoalSidecar(snapshot contract.GoalSnapshot) {
	if e.persistence.WriteGoal == nil {
		return
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_ = e.persistence.WriteGoal(context.Background(), snapshot)
}

// clearGoalSidecar removes the sidecar under writeMu. Idempotent and
// fault-tolerant: a missing or failing ClearGoal hook never blocks goal
// transitions.
func (e *Engine) clearGoalSidecar() {
	if e.persistence.ClearGoal == nil {
		return
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_ = e.persistence.ClearGoal(context.Background())
}
