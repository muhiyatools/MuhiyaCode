package command

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestBenchJSONIncludesTaskEpochEconomy(t *testing.T) {
	stats := contract.TaskStats{TaskClass: "small", TaskEpochCount: 3, CapsuleCount: 2,
		SelectedCapsuleRefs: []string{"capsule-a"}, EpochResetReasons: []string{"explicit_switch"},
		EpochFirstPromptSavings: 4200}
	var output bytes.Buffer
	emitBenchSummary(&output, stats, nil)
	var payload map[string]struct {
		Economy benchEconomy `json:"economy"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	got := payload["muhiya_bench"].Economy
	if got.TaskEpochCount != 3 || got.CapsuleCount != 2 || got.EpochFirstPromptSavings != 4200 ||
		len(got.SelectedCapsuleRefs) != 1 || len(got.EpochResetReasons) != 1 {
		t.Fatalf("economy=%+v", got)
	}
}
