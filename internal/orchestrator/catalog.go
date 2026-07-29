package orchestrator

import (
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// DiscoveredModels returns a snapshot of the gateway catalog. Catalog entries
// describe models; they never choose a model for a task.
func (e *Engine) DiscoveredModels() []contract.Model {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]contract.Model(nil), e.settings.Provider.Models...)
}

// UpdateCatalog replaces model metadata without changing the session's
// explicitly selected model.
func (e *Engine) UpdateCatalog(models []contract.Model) {
	e.mu.Lock()
	e.settings.Provider.Models = append([]contract.Model(nil), models...)
	e.mu.Unlock()
}

// reconcileCatalog validates the explicit session model before its first
// request. Missing catalog entries are a hard, actionable error: silently
// substituting a different model violates the session contract and destroys
// reproducibility.
func (e *Engine) reconcileCatalog() error {
	configured := strings.TrimSpace(e.settings.Provider.ActiveModelID)
	if configured == "" {
		return fmt.Errorf("no model is selected for this session")
	}
	if len(e.settings.Provider.Models) == 0 {
		// Manually configured OpenAI-compatible endpoints may not expose a model
		// catalog. The explicit ID remains authoritative in that case.
		return nil
	}
	for _, model := range e.settings.Provider.Models {
		if model.ID == configured {
			return nil
		}
	}
	return fmt.Errorf("selected session model %q is not available in the current gateway catalog; choose another model explicitly", configured)
}

func (e *Engine) CatalogModelName(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, model := range e.settings.Provider.Models {
		if model.ID == id {
			if strings.TrimSpace(model.Name) != "" {
				return model.Name
			}
			return model.ID
		}
	}
	return id
}
