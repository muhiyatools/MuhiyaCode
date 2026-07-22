package orchestrator

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCapsule(t *testing.T, workspace, goal string, paths, terms []string, at time.Time) TaskCapsule {
	t.Helper()
	c, err := BuildTaskCapsule(CapsuleBuildInput{EpochID: goal, SessionID: "s", WorkspaceID: workspace, Goal: goal, Outcome: CapsuleCompleted, Paths: paths, Terms: terms, CreatedAt: at}, []byte("k"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCapsuleRetrievalRejectsStaleOperationalButKeepsHistory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := sha256.Sum256([]byte("old"))
	base := CapsuleBuildInput{EpochID: "e", SessionID: "s", WorkspaceID: "w", Goal: "a.go decision",
		Outcome: CapsuleCompleted, Paths: []string{"a.go"}, ChangedFiles: []FileDeltaRef{{Path: "a.go", AfterFingerprint: fmt.Sprintf("%x", old[:])}}}
	operational, err := BuildTaskCapsule(base, []byte("k"), nil)
	if err != nil {
		t.Fatal(err)
	}
	base.EpochID = "historical"
	base.Decisions = []DecisionFact{{Key: "database", Value: "sqlite", Historical: true}}
	historical, err := BuildTaskCapsule(base, []byte("k"), nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, rows := BuildCapsuleIndex([]TaskCapsule{operational, historical}).Retrieve(RetrievalInput{
		Goal: "a.go database decision", WorkspaceID: "w", TokenBudget: 5000, MaxItems: 5,
		SigningKey: []byte("k"), Validity: CapsuleWorkspaceValidity(root)})
	if len(selected) != 1 || selected[0].ID != historical.ID {
		t.Fatalf("selected=%+v rows=%+v", selected, rows)
	}
}

func TestCapsuleRetrievalFiltersPrioritizesAndBudgets(t *testing.T) {
	at := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	exact := testCapsule(t, "w", "button theme", []string{"ui/button.ts"}, []string{"blue"}, at)
	lexical := testCapsule(t, "w", "button documentation", nil, []string{"button"}, at)
	wrong := testCapsule(t, "other", "button theme", []string{"ui/button.ts"}, nil, at)
	broken := exact
	broken.ID = "broken"
	selected, rows := BuildCapsuleIndex([]TaskCapsule{wrong, lexical, broken, exact}).Retrieve(RetrievalInput{
		Goal: "change ui/button.ts button blue", WorkspaceID: "w", Paths: []string{"ui/button.ts"}, TokenBudget: 5000, MaxItems: 2, SigningKey: []byte("k")})
	if len(selected) != 2 || selected[len(selected)-1].ID != exact.ID {
		t.Fatalf("selected=%+v", selected)
	}
	reasons := map[string]bool{}
	for _, row := range rows {
		reasons[row.ExclusionReason] = true
	}
	if !reasons["workspace_mismatch"] || !reasons["signature_invalid"] {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestCapsuleRetrievalMMRDeterminismAndScale(t *testing.T) {
	at := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	capsules := make([]TaskCapsule, 0, 1000)
	for i := 0; i < 1000; i++ {
		terms := []string{"parser", "error"}
		if i%100 == 0 {
			terms = []string{"parser", "security", fmt.Sprint(i)}
		}
		capsules = append(capsules, testCapsule(t, "w", fmt.Sprintf("parser %04d", i), nil, terms, at.Add(time.Duration(i)*time.Second)))
	}
	input := RetrievalInput{Goal: "parser security error", WorkspaceID: "w", TokenBudget: 5000, MaxItems: 5, SigningKey: []byte("k")}
	a, _ := BuildCapsuleIndex(capsules).Retrieve(input)
	b, _ := BuildCapsuleIndex(capsules).Retrieve(input)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("nondeterministic at %d", i)
		}
	}
}
