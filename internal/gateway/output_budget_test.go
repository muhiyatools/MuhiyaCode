package gateway

import (
	"math"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TB02: the output window is an explicit, per-model number instead of whatever
// default the provider applies. A single-file write must fit.

func TestOutputBudgetPrefersCatalogThenProfile(t *testing.T) {
	deepseek := ResolveModelProfile("deepseek-v4-pro")
	if got := deepseek.OutputBudget(0); got != 32_000 {
		t.Fatalf("family default should be the raised 32k, got %d", got)
	}
	if got := deepseek.OutputBudget(64_000); got != 64_000 {
		t.Fatalf("a catalog-reported cap must win, got %d", got)
	}
	if got := deepseek.OutputBudget(9_000_000); got != 384_000 {
		t.Fatalf("the documented ceiling must clamp, got %d", got)
	}
}

// MiniMax counts max_tokens against its shared context window, so its budget
// must NOT be raised with the others.
func TestOutputBudgetLeavesMiniMaxConservative(t *testing.T) {
	minimax := ResolveModelProfile("minimax-m3")
	if got := minimax.OutputBudget(0); got != 16_000 {
		t.Fatalf("MiniMax must keep its conservative 16k default, got %d", got)
	}
	if got := minimax.OutputBudget(196_608); got != 16_000 {
		t.Fatalf("a shared-window catalog ceiling must not become the request default, got %d", got)
	}
	if got := minimax.OutputBudget(8_000); got != 8_000 {
		t.Fatalf("a lower catalog ceiling must constrain the request, got %d", got)
	}
}

func TestWriteSizedFamiliesRaisedFromSixteenK(t *testing.T) {
	for _, name := range []string{"glm-4", "some-other-model", "deepseek-v4-flash"} {
		if got := ResolveModelProfile(name).OutputBudget(0); got < 32_000 {
			t.Fatalf("%s output budget still too small for a file write: %d", name, got)
		}
	}
}

func TestPhaseOutputBudgetHonorsSafeFloorCeilingAndContextAllowance(t *testing.T) {
	minimax := ResolveModelProfile("minimax-m3")
	if minimax.OutputPolicyVersion == "" {
		t.Fatal("MiniMax output policy is not versioned")
	}
	if got := minimax.PhaseOutputBudget(contract.ExecutionPhaseInspect, 100, 10_000); got < 1_024 {
		t.Fatalf("MiniMax thinking-safe floor lost: %d", got)
	}
	generic := ResolveModelProfile("generic")
	if got := generic.PhaseOutputBudget(contract.ExecutionPhaseFinish, 0, 10_000); got > 2_000 {
		t.Fatalf("finish cap is not economical: %d", got)
	}
	if got := generic.PhaseOutputBudget(contract.ExecutionPhaseChange, math.MaxInt, 700); got != 700 {
		t.Fatalf("context allowance did not clamp safely: %d", got)
	}
	if got := generic.PhaseOutputBudget(contract.ExecutionPhaseChange, math.MaxInt, math.MaxInt); got > generic.OutputTokenLimit && generic.OutputTokenLimit > 0 {
		t.Fatalf("provider ceiling exceeded: %d > %d", got, generic.OutputTokenLimit)
	}
}
