package command

import (
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

func (a *Application) persistUsageAndLineage(sessionID string, record contract.UsageRecord) error {
	if err := a.sessions.AppendUsage(sessionID, record); err != nil {
		return err
	}
	if record.Upstream == "" && record.PrefixHash == "" {
		return nil
	}
	config, ok, err := a.sessions.ReadRuntimeConfig(sessionID)
	if err != nil || !ok {
		return err
	}
	lineage := config.ModelLineages[record.Model]
	if record.Upstream != "" {
		lineage.RouteAffinity = record.Upstream
	}
	if record.PrefixHash != "" {
		lineage.PrefixHash = record.PrefixHash
	}
	lineage.UpdatedAt = time.Now().UTC()
	config.ModelLineages[record.Model] = lineage
	return a.sessions.WriteRuntimeConfig(sessionID, config)
}

func (a *Application) persistPrefixShapeAndLineage(sessionID string, snapshot contract.PrefixShapeSnapshot) error {
	if err := a.sessions.WritePrefixShape(sessionID, snapshot); err != nil {
		return err
	}
	config, ok, err := a.sessions.ReadRuntimeConfig(sessionID)
	if err != nil || !ok {
		return err
	}
	lineage := config.ModelLineages[snapshot.ModelID]
	lineage.PrefixHash = snapshot.PrefixHash
	lineage.RouteAffinity = snapshot.Upstream
	lineage.UpdatedAt = time.Now().UTC()
	config.ModelLineages[snapshot.ModelID] = lineage
	return a.sessions.WriteRuntimeConfig(sessionID, config)
}

func (a *Application) persistSessionModelLineage(sessionID, modelID string, cacheEpoch uint64) error {
	existing, _, err := a.sessions.ReadRuntimeConfig(sessionID)
	if err != nil {
		return err
	}
	permission := contract.PermissionNormal
	if existing.PermissionMode != "" {
		permission = existing.PermissionMode
	}
	lineages := existing.ModelLineages
	if lineages == nil {
		lineages = make(map[string]contract.ModelLineage)
	}
	lineage := lineages[modelID]
	lineage.CacheEpoch = cacheEpoch
	lineage.UpdatedAt = time.Now().UTC()
	for _, model := range a.settings.Provider.Models {
		if model.ID == modelID {
			lineage.CompatibilityEpoch = model.CompatibilityEpoch
			break
		}
	}
	lineages[modelID] = lineage
	return a.sessions.WriteRuntimeConfig(sessionID, state.SessionRuntimeConfig{
		Version: state.SessionRuntimeVersion, ModelID: modelID, CacheEpoch: cacheEpoch,
		ModelLineages: lineages, PermissionMode: permission,
	})
}
