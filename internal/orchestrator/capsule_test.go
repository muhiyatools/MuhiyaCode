package orchestrator

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskCapsuleCanonicalizationSignatureAndEvidence(t *testing.T) {
	at := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	input := CapsuleBuildInput{EpochID: "e1", SessionID: "s1", WorkspaceID: "w1", Goal: "ship",
		Outcome: CapsuleCompleted, CreatedAt: at, Paths: []string{"z.go", "a.go", "a.go"},
		EvidenceRefs: []string{"b", "a"}, DependsOn: []string{"prior"},
		ChangedFiles: []FileDeltaRef{{Path: "z.go"}, {Path: "a.go"}},
		Decisions:    []DecisionFact{{Key: "db", Value: "sqlite", Historical: true}}}
	exists := func(string) bool { return true }
	a, err := BuildTaskCapsule(input, []byte("key"), exists)
	if err != nil {
		t.Fatal(err)
	}
	input.Paths = []string{"a.go", "z.go"}
	input.EvidenceRefs = []string{"a", "b"}
	b, err := BuildTaskCapsule(input, []byte("key"), exists)
	if err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) || a.ID == "" || !a.Verify([]byte("key")) {
		t.Fatalf("noncanonical a=%s b=%s", ja, jb)
	}
	a.Goal = "tampered"
	if a.Verify([]byte("key")) {
		t.Fatal("tampering accepted")
	}
	if _, err := BuildTaskCapsule(input, nil, func(ref string) bool { return ref != "b" }); err == nil {
		t.Fatal("missing evidence accepted")
	}
}

func TestTaskCapsuleBounds(t *testing.T) {
	paths := make([]string, 200)
	for i := range paths {
		paths[i] = "file.go"
	}
	c, err := BuildTaskCapsule(CapsuleBuildInput{EpochID: "e", SessionID: "s", WorkspaceID: "w", Goal: string(make([]byte, 3000)), Outcome: CapsulePartial, Paths: paths}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Goal) > 2000 || len(c.Paths) != 1 {
		t.Fatalf("bounds goal=%d paths=%d", len(c.Goal), len(c.Paths))
	}
}
