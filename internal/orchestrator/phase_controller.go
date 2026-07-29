package orchestrator

type taskPhase string

const (
	phaseExplore  taskPhase = "explore"
	phaseExecute  taskPhase = "execute"
	phaseVerify   taskPhase = "verify"
	phaseFinalize taskPhase = "finalize"
)

// phaseController is the typed control state for one task. It does not ask a
// model to classify phases; observed actions drive transitions.
type phaseController struct {
	phase    taskPhase
	evidence *progressEvidenceTracker
}

func newPhaseController() *phaseController {
	return &phaseController{phase: phaseExplore, evidence: newProgressEvidenceTracker()}
}

func (controller *phaseController) Observe(outcomes []toolOutcome) {
	if controller == nil || controller.phase == phaseFinalize {
		return
	}
	controller.evidence.Observe(outcomes)
	for _, outcome := range outcomes {
		switch {
		case isCheckCall(outcome.Call):
			controller.transition(phaseVerify)
		case outcome.Succeeded() && isMutation(outcome.Call.ToolName()):
			controller.transition(phaseExecute)
		}
	}
}

func (controller *phaseController) transition(next taskPhase) {
	if controller == nil || controller.phase == next || controller.phase == phaseFinalize {
		return
	}
	switch next {
	case phaseExplore:
		return
	case phaseExecute:
		// A failed verification may legitimately return to execution.
		controller.phase = next
	case phaseVerify:
		controller.phase = next
	case phaseFinalize:
		controller.phase = next
	}
}

func (controller *phaseController) EvidenceRevision() int {
	if controller == nil {
		return 0
	}
	return controller.evidence.Revision()
}
