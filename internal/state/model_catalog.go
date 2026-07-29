package state

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type ModelCatalogCache struct {
	Version     int              `json:"version"`
	ETag        string           `json:"etag,omitempty"`
	RefreshedAt time.Time        `json:"refreshedAt"`
	Models      []contract.Model `json:"models"`
}

func LoadModelCatalog(paths ...Paths) (ModelCatalogCache, error) {
	p, err := selectPaths(paths)
	if err != nil {
		return ModelCatalogCache{}, err
	}
	cache, err := readJSON(modelCatalogPath(p), ModelCatalogCache{Version: 2})
	if err != nil {
		return ModelCatalogCache{}, err
	}
	if cache.Version != 2 {
		return ModelCatalogCache{Version: 2}, nil
	}
	if err := validateCatalogModels(cache.Models); err != nil {
		// Invalid downloads never replace the last-known-good file. A file that
		// became invalid on disk is ignored but preserved by readJSON's corrupt
		// handling when its JSON itself is malformed.
		return ModelCatalogCache{Version: 2}, nil
	}
	return cache, nil
}

func SaveModelCatalog(cache ModelCatalogCache, paths ...Paths) error {
	p, err := selectPaths(paths)
	if err != nil {
		return err
	}
	if cache.Version != 2 {
		return fmt.Errorf("unsupported model catalog cache version %d", cache.Version)
	}
	if cache.RefreshedAt.IsZero() {
		return fmt.Errorf("model catalog refresh time is required")
	}
	if err := validateCatalogModels(cache.Models); err != nil {
		return err
	}
	return writeJSON(modelCatalogPath(p), cache, false)
}

func modelCatalogPath(paths Paths) string {
	if paths.ModelCatalogFile != "" {
		return paths.ModelCatalogFile
	}
	return filepath.Join(paths.CacheDir, "model-catalog-v2.json")
}

func validateCatalogModels(models []contract.Model) error {
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			return fmt.Errorf("catalog model id is required")
		}
		if seen[id] {
			return fmt.Errorf("duplicate catalog model %q", id)
		}
		seen[id] = true
		if model.RecordID != "" {
			if model.TargetModel == "" || model.ProviderFamily == "" ||
				model.AdapterVersion == "" || model.CompatibilityEpoch <= 0 {
				return fmt.Errorf("catalog v2 model %q is incomplete", id)
			}
		}
	}
	return nil
}
