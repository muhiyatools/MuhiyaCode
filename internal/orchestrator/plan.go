package orchestrator

// Plan mode, adapted from Reasonix. While it is on, the agent researches
// read-only and produces a concrete plan; the engine blocks every mutating
// tool at execution time, and a plan-mode marker rides on the user message
// (never the system prompt) so toggling it does not disturb the prefix cache.

// SetPlanMode turns plan mode on or off for the session.
func (e *Engine) SetPlanMode(on bool) {
	e.planMu.Lock()
	e.planMode = on
	e.planMu.Unlock()
}

// PlanMode reports whether plan mode is active.
func (e *Engine) PlanMode() bool {
	e.planMu.Lock()
	defer e.planMu.Unlock()
	return e.planMode
}

// planBlock is the per-turn instruction appended to the task brief in plan mode.
func (e *Engine) planBlock() string {
	if !e.PlanMode() {
		return ""
	}
	return "[plan-mode: investigate read-only and produce a concrete, ordered plan — exact files, changes, and how you will verify. Do NOT edit, write, patch, or run mutating commands yet. Keep update_plan current and end by asking to proceed.]"
}
