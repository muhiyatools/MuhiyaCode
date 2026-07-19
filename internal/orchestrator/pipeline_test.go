package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestNeedsPlanMatrix(t *testing.T) {
	tests := []struct {
		class TaskClass
		want  bool
		depth string
	}{
		{ClassChat, false, ""},
		{ClassTiny, false, ""},
		{ClassSmall, false, ""},
		// Feature 014: standard work runs DIRECT — a "fix two bugs" request
		// gets fixed, not ceremonied. Plans come from explicit requests
		// (PlanRequest) or corroborated large/epic classification only.
		{ClassStandard, false, ""},
		{ClassLarge, true, PipelineDepthFull},
		{ClassEpic, true, PipelineDepthFull},
	}
	for _, tc := range tests {
		t.Run(string(tc.class), func(t *testing.T) {
			got := NeedsPlan(Assessment{Class: tc.class, Reason: "because"})
			if got.NeedsPlan != tc.want || got.Depth != tc.depth || got.Reason != "because" {
				t.Fatalf("NeedsPlan(%s) = %+v", tc.class, got)
			}
		})
	}
}

// The former TestPipelineLegalTransitionsAndDrivenPlanPhase / SteerEdges /
// IllegalTransitions / GatesAndResearchDegradation moved to lifecycle_test.go
// (feature 010 T011): they now exercise the unified Lifecycle.Transition and
// its gates. DrivenPlanPhase was deleted with the two-machine projection.

func TestPipelinePhasePersistsInPlanStateSnapshot(t *testing.T) {
	want := contract.PlanStateSnapshot{PlanMode: true, Phase: contract.PlanPhaseDrafting, PipelinePhase: contract.PipelinePhaseResearch}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got contract.PlanStateSnapshot
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	var legacy contract.PlanStateSnapshot
	if err := json.Unmarshal([]byte(`{"planMode":true,"pendingPlan":false,"phase":"drafting"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.PipelinePhase != "" {
		t.Fatalf("legacy pipeline phase = %q", legacy.PipelinePhase)
	}
}

// The research-scope and step-grouping helpers were removed with feature 013's
// model-driven dispatch: the model decomposes and delegates its own way; the
// harness no longer fans out or groups on its behalf.
