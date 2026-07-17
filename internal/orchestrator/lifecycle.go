package orchestrator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Lifecycle (feature 010 US1) is the SINGLE task-lifecycle state machine that
// replaces the four desync-prone engine fields (planMode, pendingPlan,
// planPhase, and the pipeline PipelineState) plus their two phase enums. There
// is exactly one authoritative value, contract.LifecycleState; every consumer
// reads it through the predicate API (State.IsReadOnly/BlocksMutation/… and the
// Lifecycle helpers below) rather than comparing raw states, so the two-truth
// disagreement class the 009 audit found becomes structurally unrepresentable.
//
// Depth doubles as the orchestration signal: a non-empty Depth (light/full)
// means the enforced research→plan→approve→implement→validate pipeline owns
// this lifecycle; an empty Depth is a plain plan-mode (manual /plan) lifecycle.
// That single bit is what selects the pipeline vs plan-mode gate text and the
// per-phase vs per-task subagent budget, exactly as the old
// `pipeline.Phase != direct` check did — without a second field that could drift.
type Lifecycle struct {
	State contract.LifecycleState
	// Depth is "" for a plan-mode-only lifecycle and light/full for an
	// orchestrated pipeline. Orchestrated() reads it; it also persists as the
	// sidecar `depth` so a light pipeline resumes light.
	Depth string
	// Why is the dynamic verdict reason. It rides session state and per-turn
	// briefs, never the cache-stable prefix.
	Why string
	// Gate facts are rebuilt on restore from the durable plan (restore.go).
	ResearchCompleted bool
	PlanWritten       bool
	Approved          bool
	StepsComplete     bool
	Validated         bool
	// Degradations is the bounded-fallback ledger (absorbs the old
	// PipelineDegradation slice).
	Degradations []LifecycleDegradation
}

// LifecycleDegradation records one bounded fallback without pretending the
// preferred phase completed normally. Reasons ride dynamic state, not the prefix.
type LifecycleDegradation struct {
	State  contract.LifecycleState `json:"state"`
	Reason string                  `json:"reason"`
}

var (
	errLifecycleTransition = errors.New("illegal lifecycle transition")
	errLifecycleGate       = errors.New("lifecycle gate not satisfied")
)

// Orchestrated reports whether the enforced pipeline drives this lifecycle (a
// depth was assigned) as opposed to a manual plan-mode lifecycle. It is the one
// discriminator the old code expressed as `pipeline.Phase != direct`.
func (l Lifecycle) Orchestrated() bool { return l.Depth != "" }

// Predicate pass-throughs so consumers can read the Lifecycle value directly.
func (l Lifecycle) IsReadOnly() bool          { return l.State.IsReadOnly() }
func (l Lifecycle) BlocksMutation() bool      { return l.State.BlocksMutation() }
func (l Lifecycle) InvitesProceed() bool      { return l.State.InvitesProceed() }
func (l Lifecycle) IsApprovalPause() bool     { return l.State.IsApprovalPause() }
func (l Lifecycle) IsPipelineResumable() bool { return l.State.IsPipelineResumable() }
func (l Lifecycle) IsTerminal() bool          { return l.State.IsTerminal() }
func (l Lifecycle) IsActive() bool            { return l.State.IsActive() }

// HasAnyDegradation reports whether any phase recorded a bounded fallback — the
// "recorded-degradation" signal the feature-010 recovery invariant reads
// (contract fault-injection FI-2).
func (l Lifecycle) HasAnyDegradation() bool { return len(l.Degradations) > 0 }

func (l Lifecycle) IsDegraded(state contract.LifecycleState) bool {
	for _, item := range l.Degradations {
		if item.State == state {
			return true
		}
	}
	return false
}

// ResearchReady / DoneReady are the gate facts the research→plan and
// validate→done edges consult (identical semantics to the old pipeline gates).
func (l Lifecycle) ResearchReady() bool {
	return l.ResearchCompleted || l.IsDegraded(contract.LifecycleResearch)
}

func (l Lifecycle) DoneReady() bool { return l.StepsComplete && l.Validated }

// RecordDegradation appends a bounded fallback, de-duplicated by (state, reason).
func (l *Lifecycle) RecordDegradation(state contract.LifecycleState, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("lifecycle degradation reason is required")
	}
	for _, item := range l.Degradations {
		if item.State == state && item.Reason == reason {
			return nil
		}
	}
	l.Degradations = append(l.Degradations, LifecycleDegradation{State: state, Reason: reason})
	return nil
}

// Transition applies the single legal-edge table, merging the old
// PipelineState.Transition edges with the plan-lifecycle approval/steer/park
// edges. Gate failures are distinguishable from illegal edges so callers can
// recover in a bounded way. Terminal states have NO outgoing edges here — a
// fresh lifecycle starts through Set/beginPipeline, never a Transition, so the
// terminal-sticky guarantee is enforced in exactly one direction. `direct` also
// has no Transition edges (a lifecycle begins via Set/beginPipeline).
func (l *Lifecycle) Transition(next contract.LifecycleState) error {
	if l == nil {
		return errors.New("nil lifecycle")
	}
	legal, gateOK := false, true
	switch l.State {
	case contract.LifecycleResearch:
		legal = next == contract.LifecyclePlanning
		gateOK = l.ResearchReady()
	case contract.LifecyclePlanning:
		legal = next == contract.LifecycleApproval
		gateOK = l.PlanWritten
	case contract.LifecycleApproval:
		legal = next == contract.LifecycleImplementing || next == contract.LifecyclePlanning ||
			next == contract.LifecycleResearch || next == contract.LifecyclePending ||
			next == contract.LifecycleDirect
		if next == contract.LifecycleImplementing {
			gateOK = l.Approved && l.PlanWritten
		}
	case contract.LifecyclePending:
		legal = next == contract.LifecycleImplementing || next == contract.LifecycleSuperseded
	case contract.LifecycleImplementing:
		legal = next == contract.LifecycleValidating || next == contract.LifecycleInterrupted
		if next == contract.LifecycleValidating {
			gateOK = l.StepsComplete
		}
	case contract.LifecycleValidating:
		legal = next == contract.LifecycleFinished || next == contract.LifecycleInterrupted
		if next == contract.LifecycleFinished {
			gateOK = l.DoneReady()
		}
	case contract.LifecycleInterrupted:
		legal = next == contract.LifecycleImplementing || next == contract.LifecycleSuperseded
	}
	// Universal edges from any NON-terminal state: /plan clear discards, and a
	// fresh plan supersedes the old one. These are UNCONDITIONAL, so they also
	// clear any gate the specific-edge switch above set for a different `next`
	// (e.g. Research's ResearchReady gate for the Research→Planning edge must not
	// block a Research→Discarded transition).
	if !l.State.IsTerminal() && (next == contract.LifecycleDiscarded || next == contract.LifecycleSuperseded) {
		legal = true
		gateOK = true
	}
	if !legal {
		return fmt.Errorf("%w: %s -> %s", errLifecycleTransition, l.State, next)
	}
	if !gateOK {
		return fmt.Errorf("%w: %s -> %s", errLifecycleGate, l.State, next)
	}
	l.State = next
	return nil
}

// Set applies a plan-lifecycle affordance change (drafting/ready/pending/
// executing/interrupted/finished and fresh-start transitions). It is ungated —
// it trusts the caller's intent (a user "proceed", a step-progress backstop, an
// end-of-task stamp) — but keeps the ONE terminal-sticky rule: a finished/
// superseded/discarded plan is never revived into an executable state; only a
// fresh planning cycle (or going idle to direct) may leave a terminal state.
func (l *Lifecycle) Set(next contract.LifecycleState) {
	if l.State.IsTerminal() && next != contract.LifecyclePlanning && next != contract.LifecycleDirect {
		return
	}
	l.State = next
}

// pipelineLabel maps a unified state to the legacy pipeline-phase string used in
// model-facing gate text, restore/attribution notices, agent-event phase tags,
// and persisted pipeline events — preserving those bytes across the unification
// (the LifecycleState values differ: planning≠plan, awaiting-approval≠approve).
func pipelineLabel(state contract.LifecycleState) string {
	switch state {
	case contract.LifecycleResearch:
		return "research"
	case contract.LifecyclePlanning:
		return "plan"
	case contract.LifecycleApproval:
		return "approve"
	case contract.LifecycleImplementing:
		return "implement"
	case contract.LifecycleValidating:
		return "validate"
	case contract.LifecycleFinished:
		return "done"
	default:
		return string(state)
	}
}

// disclosesIncomplete reports the states whose finalize discloses open plan
// steps (the old executing/interrupted plan phases — executing covered both the
// implement and validate pipeline phases).
func disclosesIncomplete(state contract.LifecycleState) bool {
	return state == contract.LifecycleImplementing ||
		state == contract.LifecycleValidating ||
		state == contract.LifecycleInterrupted
}
