package orchestrator

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestTaskEpochTransitionsIdentityAndLegacy(t *testing.T) {
	at := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	epoch, err := NewTaskEpoch(NewTaskEpochInput{SessionID: "s1", ModelID: "m1", UpstreamPin: "s1:main", Ordinal: 1, Goal: " fix it ", Class: ClassTiny, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if epoch.Ordinal != 1 || epoch.Goal != "fix it" || epoch.ContextRevision != 0 {
		t.Fatalf("epoch=%+v", epoch)
	}
	if err := epoch.ValidateIdentity("m2", "s1:main"); err == nil {
		t.Fatal("model mutation accepted")
	}
	if err := epoch.ValidateIdentity("m1", "other"); err == nil {
		t.Fatal("upstream mutation accepted")
	}
	if err := epoch.ReplaceContext(); err != nil || epoch.ContextRevision != 1 {
		t.Fatalf("revision=%d err=%v", epoch.ContextRevision, err)
	}
	if err := epoch.Transition(EpochSettling, at); err != nil {
		t.Fatal(err)
	}
	if err := epoch.Transition(EpochSettled, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if epoch.SettledAt == nil || epoch.State != EpochSettled {
		t.Fatalf("settled=%+v", epoch)
	}
	if err := epoch.Transition(EpochOpen, at); err == nil {
		t.Fatal("settled epoch reopened")
	}
	legacy := LegacyTaskEpoch("s1", "m1", "s1:main")
	if legacy.Ordinal != 0 || legacy.ID != "legacy-0" || legacy.State != EpochOpen {
		t.Fatalf("legacy=%+v", legacy)
	}
}

func TestTaskEpochSixTaskOfflineMatrixTwice(t *testing.T) {
	for run := 0; run < 2; run++ {
		followups := []RelatednessInput{
			{Prompt: "make that button blue instead", CurrentGoal: "build settings button", CurrentSettled: true},
			{Prompt: "actually keep the same retry limit", CurrentGoal: "repair payment retry", CurrentSettled: true},
		}
		for _, input := range followups {
			if got := ClassifyRelatedness(input); got.Decision != RelatedContinue {
				t.Fatalf("run %d followup=%+v", run, got)
			}
		}
		at := time.Date(2026, 7, 22, 0, 0, run, 0, time.UTC)
		required, err := BuildTaskCapsule(CapsuleBuildInput{EpochID: "auth", SessionID: "s", WorkspaceID: "w",
			Goal: "authentication database migration", Outcome: CapsuleCompleted,
			Decisions: []DecisionFact{{Key: "database", Value: "sqlite", Historical: true}},
			Terms:     []string{"authentication", "database", "migration", "sqlite"}, CreatedAt: at}, []byte("k"), nil)
		if err != nil {
			t.Fatal(err)
		}
		distractors := []TaskCapsule{required}
		for i := 0; i < 5; i++ {
			distractors = append(distractors, testCapsule(t, "w", "unrelated visual task", nil, []string{"layout", "color"}, at.Add(time.Duration(i+1)*time.Second)))
		}
		selected, _ := BuildCapsuleIndex(distractors).Retrieve(RetrievalInput{Goal: "reuse authentication database migration choice",
			WorkspaceID: "w", TokenBudget: 1_200, MaxItems: 3, SigningKey: []byte("k")})
		if len(selected) == 0 || selected[len(selected)-1].ID != required.ID {
			t.Fatalf("run %d required fact not retrieved: %+v", run, selected)
		}
		fullHistoryTokens := EstimateTokens(strings.Repeat("previous unrelated tool evidence ", 1_500))
		resetTokens := estimateMessagesTokens(renderCapsuleMessages(selected))
		if resetTokens*100 > fullHistoryTokens*40 {
			t.Fatalf("run %d reduction below 60%%: full=%d reset=%d", run, fullHistoryTokens, resetTokens)
		}
		epoch, err := NewTaskEpoch(NewTaskEpochInput{SessionID: "s", ModelID: "fixed", UpstreamPin: "s:main", Ordinal: uint64(run + 1)})
		if err != nil || epoch.ValidateIdentity("fixed", "s:main") != nil {
			t.Fatalf("run %d identity err=%v epoch=%+v", run, err, epoch)
		}
	}
}

func TestEpochHistoryReplacementIsTransactional(t *testing.T) {
	var persisted HistorySnapshot
	history := NewHistory(HistorySnapshot{Version: 1, Messages: []contract.Message{{Role: contract.RoleUser, Content: "old"}}}, func(snapshot HistorySnapshot) error {
		persisted = snapshot
		return nil
	})
	err := history.ReplaceForEpoch("e2", []contract.Message{{Role: contract.RoleUser, Content: "capsule"}}, func() error {
		return errors.New("ledger commit failed")
	})
	if err == nil || history.All()[0].Content != "old" || persisted.Messages[0].Content != "old" {
		t.Fatalf("failed transaction changed history: err=%v memory=%+v persisted=%+v", err, history.All(), persisted)
	}
	if err := history.ReplaceForEpoch("e2", []contract.Message{{Role: contract.RoleUser, Content: "capsule"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := history.Snapshot(); got.TaskEpochID != "e2" || got.Messages[0].Content != "capsule" || got.RewriteVersion != 1 {
		t.Fatalf("committed=%+v", got)
	}
}

func TestTaskEpochOrdinalsAndCanonicalRefs(t *testing.T) {
	epoch, err := NewTaskEpoch(NewTaskEpochInput{SessionID: "s", ModelID: "m", UpstreamPin: "p", Ordinal: 2, RelatedCapsules: []CapsuleRef{{ID: "z"}, {ID: "a"}, {ID: "a", Reason: "exact"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(epoch.RelatedCapsules) != 2 || epoch.RelatedCapsules[0].ID != "a" || epoch.RelatedCapsules[0].Reason != "exact" {
		t.Fatalf("refs=%+v", epoch.RelatedCapsules)
	}
}
