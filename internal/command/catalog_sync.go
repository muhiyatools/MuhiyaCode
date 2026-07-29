package command

import (
	"context"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

func (a *Application) startCatalogPoller() {
	a.catalogOnce.Do(func() {
		ctx, cancel := context.WithCancel(a.ctx)
		a.catalogCancel = cancel
		go func() {
			ticker := time.NewTicker(catalogRefreshTTL)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					refreshCtx, refreshCancel := context.WithTimeout(ctx, 20*time.Second)
					_ = a.refreshCatalogAtBoundary(refreshCtx)
					refreshCancel()
				}
			}
		}()
	})
}

func (a *Application) refreshCatalogAtBoundary(ctx context.Context) error {
	a.mu.Lock()
	engine := a.runtime.Engine
	a.mu.Unlock()
	if engine != nil && engine.IsBusy() {
		return nil
	}
	models, err := a.provider.ListModels(ctx)
	if err != nil || len(models) == 0 {
		return err
	}
	if err := state.SaveModelCatalog(state.ModelCatalogCache{
		Version: 2, RefreshedAt: time.Now().UTC(), Models: models,
	}, a.paths); err != nil {
		return err
	}
	if engine != nil && engine.IsBusy() {
		return nil
	}

	a.mu.Lock()
	selected := a.settings.Provider.ActiveModelID
	addDiscoveredModels(a.settings, append([]contract.Model(nil), models...))
	a.settings.Provider.ActiveModelID = selected
	settingsSnapshot := *a.settings
	apiKey := a.secrets.ProviderAPIKey
	activeEngine := a.runtime.Engine
	a.mu.Unlock()

	a.provider.UpdateConfig(settingsSnapshot, apiKey)
	if activeEngine != nil {
		activeEngine.UpdateCatalog(settingsSnapshot.Provider.Models)
	}
	return nil
}
