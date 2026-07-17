package orchestrator

import (
	"encoding/json"
	"strings"
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
		{ClassStandard, true, PipelineDepthLight},
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

func TestPipelineResearchScopesUseExplicitIndependentDirectories(t *testing.T) {
	scopes, explicit := pipelineResearchScopes("Audit auth/, billing/, reports/, and notify/ independently.")
	if !explicit {
		t.Fatal("four named directories must be reported as explicit disjoint scopes")
	}
	if len(scopes) != 4 {
		t.Fatalf("explicit scopes = %d, want 4: %+v", len(scopes), scopes)
	}
	for index, want := range []string{"auth/", "billing/", "reports/", "notify/"} {
		if !strings.Contains(scopes[index].title, want) || !strings.Contains(scopes[index].focus, "only the independent scope "+want) {
			t.Fatalf("scope %d = %+v, want %s", index, scopes[index], want)
		}
	}
}

func TestPipelineParallelGroupsRequireDisjointExactTargets(t *testing.T) {
	plan := contract.Plan{Steps: []contract.PlanStep{
		{Title: "[parallel] internal/orchestrator/pipeline.go function A [F1] Acceptance: A passes", Status: contract.PlanPending},
		{Title: "[parallel] internal/orchestrator/pipeline.go function B [F2] Acceptance: B passes", Status: contract.PlanPending},
	}}
	groups := groupPipelineSteps(plan, 2)
	if pipelineGroupsDisjoint(plan, groups) {
		t.Fatal("same-file plan steps were allowed to mutate concurrently")
	}
	plan.Steps[1].Title = "[parallel] internal/gateway/provider.go function B [F2] Acceptance: B passes"
	if !pipelineGroupsDisjoint(plan, groupPipelineSteps(plan, 2)) {
		t.Fatal("distinct exact-file plan steps lost safe parallelism")
	}
}
