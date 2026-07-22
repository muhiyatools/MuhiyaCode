package orchestrator

import (
	"reflect"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestEconomyPhaseTraces(t *testing.T) {
	changeFlow := []PhaseOutcomeKind{PhaseOutcomeInspected, PhaseOutcomeChanged, PhaseOutcomeVerified, PhaseOutcomeFinalized}
	tests := []struct {
		name         string
		requirements PhaseRequirements
		outcomes     []PhaseOutcomeKind
		want         []contract.ExecutionPhase
	}{
		{name: "greeting", outcomes: []PhaseOutcomeKind{PhaseOutcomeFinalized}, want: []contract.ExecutionPhase{contract.ExecutionPhaseOrient, contract.ExecutionPhaseFinish}},
		{name: "named-file typo", requirements: PhaseRequirements{Change: true, Verification: true}, outcomes: changeFlow, want: standardChangeTrace()},
		{name: "css tweak", requirements: PhaseRequirements{Change: true, Verification: true}, outcomes: changeFlow, want: standardChangeTrace()},
		{name: "one-file bug", requirements: PhaseRequirements{Change: true, Verification: true}, outcomes: changeFlow, want: standardChangeTrace()},
		{name: "greenfield tic-tac-toe", requirements: PhaseRequirements{Change: true, Verification: true}, outcomes: changeFlow, want: standardChangeTrace()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := NewPhaseController(test.requirements)
			for requestIndex, outcome := range test.outcomes {
				if err := controller.Record(outcome, uint64(requestIndex+1)); err != nil {
					t.Fatal(err)
				}
			}
			if got := controller.Trace(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("trace=%v, want %v", got, test.want)
			}
		})
	}
}

func TestEconomyPhaseRequiresVerificationAndBoundsRecovery(t *testing.T) {
	controller := NewPhaseController(PhaseRequirements{Change: true, Verification: true, MaximumRecoveryAttempts: 1})
	if err := controller.Record(PhaseOutcomeInspected, 1); err != nil {
		t.Fatal(err)
	}
	if err := controller.Record(PhaseOutcomeChanged, 2); err != nil {
		t.Fatal(err)
	}
	if err := controller.Record(PhaseOutcomeFinalized, 3); err == nil {
		t.Fatal("finalization bypassed required verification")
	}
	if err := controller.Record(PhaseOutcomeFailed, 3); err != nil {
		t.Fatal(err)
	}
	if err := controller.Record(PhaseOutcomeRecoveryChanged, 4); err != nil {
		t.Fatal(err)
	}
	if err := controller.Record(PhaseOutcomeFailed, 5); err == nil {
		t.Fatal("second recovery attempt exceeded the bounded recovery policy")
	}
}

func standardChangeTrace() []contract.ExecutionPhase {
	return []contract.ExecutionPhase{
		contract.ExecutionPhaseOrient,
		contract.ExecutionPhaseInspect,
		contract.ExecutionPhaseChange,
		contract.ExecutionPhaseVerify,
		contract.ExecutionPhaseFinish,
	}
}

func TestPhaseGraphAuthorityRollout(t *testing.T) {
	tests := []struct {
		mode  string
		class TaskClass
		want  bool
	}{
		{mode: "off", class: ClassSmall},
		{mode: "observe", class: ClassSmall},
		{mode: "balanced", class: ClassSmall, want: true},
		{mode: "balanced", class: ClassStandard},
		{mode: "aggressive", class: ClassTiny, want: true},
	}
	for _, test := range tests {
		if got := phaseGraphAuthoritative(test.mode, test.class); got != test.want {
			t.Fatalf("mode=%s class=%s authoritative=%t, want %t", test.mode, test.class, got, test.want)
		}
	}
}
