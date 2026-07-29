package command

import (
	"errors"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

// hydrateSessionRuntime loads (or creates) the durable session runtime record
// and stamps its model/cache-epoch/permission onto the session struct. A new
// session inherits the global model + permission defaults; an existing session
// restores its saved values (UMI-06, UMI-15).
func (a *Application) hydrateSessionRuntime(session contract.Session) (contract.Session, error) {
	config, ok, err := a.sessions.ReadRuntimeConfig(session.ID)
	if err != nil {
		return contract.Session{}, err
	}
	if !ok {
		modelID := strings.TrimSpace(a.defaultModelID)
		if modelID == "" {
			return contract.Session{}, errors.New("no default model is configured for new sessions")
		}
		// UMI-06: a new session inherits the global permission default (Normal
		// unless the user has explicitly configured a trusted default). The
		// global setting is the default for a NEW session only — once written,
		// the runtime record is the authority and survives restart/resume.
		config = state.SessionRuntimeConfig{
			Version: state.SessionRuntimeVersion, ModelID: modelID, CacheEpoch: 0,
			ModelLineages:  map[string]contract.ModelLineage{modelID: {CacheEpoch: 0}},
			PermissionMode: a.defaultPermissionMode(),
		}
		if err := a.sessions.WriteRuntimeConfig(session.ID, config); err != nil {
			return contract.Session{}, err
		}
	}
	session.ModelID, session.CacheEpoch, session.PermissionMode = config.ModelID, config.CacheEpoch, config.PermissionMode
	session.ModelLineages = config.ModelLineages
	return session, nil
}

// defaultPermissionMode returns the global permission default for a new session.
// The global setting is the default ONLY; an existing session's runtime record
// is the authority (UMI-06). Normal is the safe fallback.
func (a *Application) defaultPermissionMode() contract.PermissionMode {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settings.PermissionMode == "" {
		return contract.PermissionNormal
	}
	return a.settings.PermissionMode
}
