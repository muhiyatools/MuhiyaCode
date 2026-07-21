package command

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// A model the gateway stops offering must be pruned from settings on refresh,
// except when it is the model the session is running on — that one is kept (so
// the running session is never stranded) and reported. Manually-added models
// are never pruned.
func TestPruneStaleDiscoveredModels(t *testing.T) {
	settings := &contract.Settings{}
	settings.Provider.ActiveModelID = "keep-active"
	settings.Provider.Models = []contract.Model{
		{ID: "fresh", Source: "endpoint"},
		{ID: "gone", Source: "endpoint"},
		{ID: "keep-active", Source: "endpoint"},
		{ID: "manual", Source: "manual"},
	}
	fresh := []contract.Model{{ID: "fresh"}}

	stranded := pruneStaleDiscoveredModels(settings, fresh)

	present := map[string]bool{}
	for _, m := range settings.Provider.Models {
		present[m.ID] = true
	}
	if present["gone"] {
		t.Fatal("a stale endpoint model absent from the fresh list must be pruned")
	}
	for _, id := range []string{"fresh", "manual", "keep-active"} {
		if !present[id] {
			t.Fatalf("model %q was wrongly pruned: %v", id, present)
		}
	}
	strandedSet := map[string]bool{}
	for _, s := range stranded {
		strandedSet[s] = true
	}
	if !strandedSet["keep-active"] {
		t.Fatalf("the stranded active model must be reported, got %v", stranded)
	}
	if strandedSet["gone"] || strandedSet["manual"] {
		t.Fatalf("only a still-selected model should be reported stranded, got %v", stranded)
	}
}

// An empty successful fetch is treated as "unknown", not "unpublish everything":
// nothing is pruned, so a transient empty catalog never wipes the picker.
func TestPruneStaleNoOpOnEmptyFresh(t *testing.T) {
	settings := &contract.Settings{}
	settings.Provider.Models = []contract.Model{{ID: "a", Source: "endpoint"}, {ID: "b", Source: "endpoint"}}
	stranded := pruneStaleDiscoveredModels(settings, nil)
	if len(settings.Provider.Models) != 2 || len(stranded) != 0 {
		t.Fatalf("empty fresh list must not prune anything, models=%d stranded=%v", len(settings.Provider.Models), stranded)
	}
}
