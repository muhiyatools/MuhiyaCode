package orchestrator

import (
	"errors"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type PhaseRequirements struct {
	Change                  bool
	Verification            bool
	MaximumRecoveryAttempts int
}

type PhaseOutcomeKind string

const (
	PhaseOutcomeInspected       PhaseOutcomeKind = "inspected"
	PhaseOutcomeChanged         PhaseOutcomeKind = "changed"
	PhaseOutcomeVerified        PhaseOutcomeKind = "verified"
	PhaseOutcomeFinalized       PhaseOutcomeKind = "finalized"
	PhaseOutcomeFailed          PhaseOutcomeKind = "failed"
	PhaseOutcomeRecoveryChanged PhaseOutcomeKind = "recovery_changed"
)

type PhaseController struct {
	requirements     PhaseRequirements
	state            ExecutionPhaseState
	trace            []contract.ExecutionPhase
	recoveryAttempts int
	changed          bool
	verified         bool
}

func NewPhaseController(requirements PhaseRequirements) *PhaseController {
	if requirements.MaximumRecoveryAttempts <= 0 {
		requirements.MaximumRecoveryAttempts = 1
	}
	state, _ := (ExecutionPhaseState{}).Transition(contract.ExecutionPhaseOrient, 0, TransitionUserChange)
	return &PhaseController{requirements: requirements, state: state, trace: []contract.ExecutionPhase{contract.ExecutionPhaseOrient}}
}

func (controller *PhaseController) Current() contract.ExecutionPhase {
	return controller.state.Phase
}

func (controller *PhaseController) Trace() []contract.ExecutionPhase {
	return append([]contract.ExecutionPhase(nil), controller.trace...)
}

func (controller *PhaseController) NeedsVerification() bool {
	return controller != nil && controller.requirements.Verification && controller.changed && !controller.verified
}

func (controller *PhaseController) LogicalStepID() string {
	if controller == nil {
		return "orient:0"
	}
	return fmt.Sprintf("%s:%d", controller.state.Phase, controller.state.EnteredAtRequest)
}

func (controller *PhaseController) Record(outcome PhaseOutcomeKind, requestSeq uint64) error {
	next, reason, err := controller.nextPhase(outcome)
	if err != nil {
		return err
	}
	if next == controller.state.Phase {
		return nil
	}
	state, err := controller.state.Transition(next, requestSeq, reason)
	if err != nil {
		return err
	}
	controller.state = state
	controller.trace = append(controller.trace, next)
	return nil
}

func (controller *PhaseController) nextPhase(outcome PhaseOutcomeKind) (contract.ExecutionPhase, PhaseTransitionReason, error) {
	switch outcome {
	case PhaseOutcomeInspected:
		return contract.ExecutionPhaseInspect, TransitionEvidenceComplete, nil
	case PhaseOutcomeChanged:
		controller.changed = true
		return contract.ExecutionPhaseChange, TransitionUserChange, nil
	case PhaseOutcomeVerified:
		controller.verified = true
		return contract.ExecutionPhaseVerify, TransitionEvidenceComplete, nil
	case PhaseOutcomeFinalized:
		if controller.requirements.Verification && !controller.verified {
			return "", "", errors.New("required verification is not complete")
		}
		return contract.ExecutionPhaseFinish, TransitionFinalize, nil
	case PhaseOutcomeFailed:
		if controller.recoveryAttempts >= controller.requirements.MaximumRecoveryAttempts {
			return "", "", errors.New("phase recovery budget exhausted")
		}
		controller.recoveryAttempts++
		return contract.ExecutionPhaseRecover, TransitionFailureRecovery, nil
	case PhaseOutcomeRecoveryChanged:
		if controller.state.Phase != contract.ExecutionPhaseRecover {
			return "", "", errors.New("material recovery requires recover phase")
		}
		return controller.state.PreviousPhase, TransitionFailureRecovery, nil
	default:
		return "", "", fmt.Errorf("unknown phase outcome %q", outcome)
	}
}

func phasePointer(controller *PhaseController) *string {
	if controller == nil {
		return nil
	}
	phase := string(controller.Current())
	return &phase
}

func recordFinalPhase(controller *PhaseController, requestSeq uint64) error {
	if controller == nil {
		return nil
	}
	return controller.Record(PhaseOutcomeFinalized, requestSeq)
}

func recordToolPhases(controller *PhaseController, outcomes []toolOutcome, requestSeq uint64) error {
	if controller == nil || len(outcomes) == 0 {
		return nil
	}
	if allFailed(outcomes) {
		return controller.Record(PhaseOutcomeFailed, requestSeq)
	}
	if controller.Current() == contract.ExecutionPhaseRecover {
		if err := controller.Record(PhaseOutcomeRecoveryChanged, requestSeq); err != nil {
			return err
		}
	}
	if controller.Current() == contract.ExecutionPhaseOrient {
		if err := controller.Record(PhaseOutcomeInspected, requestSeq); err != nil {
			return err
		}
	}
	for _, outcome := range outcomes {
		if !outcome.Failed && isMutation(outcome.Call.ToolName()) {
			if err := controller.Record(PhaseOutcomeChanged, requestSeq); err != nil {
				return err
			}
		}
	}
	for _, outcome := range outcomes {
		if !outcome.Failed && isCheckCall(outcome.Call) {
			if err := controller.Record(PhaseOutcomeVerified, requestSeq); err != nil {
				return err
			}
		}
	}
	return nil
}

func phaseGraphAuthoritative(mode string, class TaskClass) bool {
	if mode != "balanced" && mode != "aggressive" {
		return false
	}
	return class == ClassChat || class == ClassTiny || class == ClassSmall
}
