package command

import (
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

func TestLoadConfigHydratesGatewayCatalog(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	settings := state.DefaultSettings()
	settings.Provider.ActiveModelID = "minimax-m3"
	settings.Provider.Models = nil
	if err := state.SaveSettings(settings, paths); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveModelCatalog(state.ModelCatalogCache{
		Version: 2, RefreshedAt: time.Now().UTC(),
		Models: []contract.Model{{
			ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000,
		}},
	}, paths); err != nil {
		t.Fatal(err)
	}

	_, loaded, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	active, ok := state.ActiveModel(loaded)
	if !ok || active.ContextLimit != 1_000_000 {
		t.Fatalf("active model = %+v, found=%v", active, ok)
	}
}
