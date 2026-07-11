package taskboard

import "testing"

func TestValidateRejectsDuplicateIDs(t *testing.T) {
	err := Validate([]Task{
		{ID: 1, Title: "one", Status: StatusTodo},
		{ID: 1, Title: "duplicate", Status: StatusDone},
	})
	if err == nil {
		t.Fatal("expected duplicate ID error")
	}
}

func TestCountByStatusIncludesZeroBuckets(t *testing.T) {
	counts := CountByStatus([]Task{{ID: 1, Title: "one", Status: StatusTodo}})
	if counts[StatusTodo] != 1 || counts[StatusDoing] != 0 || counts[StatusDone] != 0 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
}
