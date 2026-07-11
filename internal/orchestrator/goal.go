package orchestrator

import (
	"fmt"
	"regexp"
	"strings"
)

// Goal is a durable objective the agent works toward across multiple turns,
// adapted from Reasonix's goal machine. The goal text rides on the user
// message (never the system prompt), so activating or clearing a goal does not
// disturb the cache-stable prefix. Each turn the model must end with exactly
// one control marker; the engine reads it to decide whether to auto-continue.
type Goal struct {
	Text      string
	Status    GoalStatus
	AutoTurns int
	Blocked   string
}

type GoalStatus string

const (
	GoalActive   GoalStatus = "active"
	GoalComplete GoalStatus = "complete"
	GoalBlocked  GoalStatus = "blocked"
)

// maxGoalAutoTurns bounds autonomous continuation so a goal can never loop
// forever without the user.
const maxGoalAutoTurns = 12

var goalMarkerRE = regexp.MustCompile(`(?i)\[goal:(continue|complete|blocked)(?::\s*([^\]]*))?\]`)

// SetGoal activates a new objective, replacing any previous one.
func (e *Engine) SetGoal(text string) {
	text = strings.TrimSpace(text)
	e.goalMu.Lock()
	if text == "" {
		e.goal = nil
	} else {
		e.goal = &Goal{Text: text, Status: GoalActive}
	}
	e.goalMu.Unlock()
}

// ClearGoal removes the active objective.
func (e *Engine) ClearGoal() {
	e.goalMu.Lock()
	e.goal = nil
	e.goalMu.Unlock()
}

// GoalSnapshot returns a copy of the current goal, if any.
func (e *Engine) GoalSnapshot() (Goal, bool) {
	e.goalMu.Lock()
	defer e.goalMu.Unlock()
	if e.goal == nil {
		return Goal{}, false
	}
	return *e.goal, true
}

// goalBlock is the per-turn instruction block appended to the task brief while
// a goal is active. It is deliberately placed on the user message.
func (e *Engine) goalBlock() string {
	e.goalMu.Lock()
	defer e.goalMu.Unlock()
	if e.goal == nil || e.goal.Status != GoalActive {
		return ""
	}
	return "[active-goal: " + oneLineGoal(e.goal.Text) + "]\n" +
		"Work autonomously toward this goal. End every reply with exactly one marker: " +
		"[goal:continue] if more work remains, [goal:complete] once the goal is fully met and verified, " +
		"or [goal:blocked: <reason>] if you cannot proceed."
}

// advanceGoal reads the control marker from the final assistant text and
// decides whether the engine should auto-continue. It returns a synthetic
// continuation prompt (non-empty ⇒ keep working) or "" to stop.
func (e *Engine) advanceGoal(finalText string) string {
	e.goalMu.Lock()
	defer e.goalMu.Unlock()
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
	}
	switch verb {
	case "complete":
		e.goal.Status = GoalComplete
		return ""
	case "blocked":
		e.goal.Status = GoalBlocked
		e.goal.Blocked = reason
		return ""
	default: // continue (also the default when no marker was emitted)
		e.goal.AutoTurns++
		if e.goal.AutoTurns >= maxGoalAutoTurns {
			e.goal.Status = GoalBlocked
			e.goal.Blocked = "reached the autonomous-turn limit"
			return ""
		}
		return fmt.Sprintf("[goal] Continue toward the active goal: %s\nKeep going without asking; end with a goal marker.", oneLineGoal(e.goal.Text))
	}
}

func oneLineGoal(text string) string {
	return truncate(strings.Join(strings.Fields(text), " "), 200)
}
