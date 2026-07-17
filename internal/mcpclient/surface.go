package mcpclient

import (
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

type BoundaryChange struct {
	Tools []contract.Tool
	Scope string
}

// serverForTool extracts the MCP server name from a tool name. Names follow
// the `mcp__<server>__<tool>` convention; the prefix is mandatory whenever a
// tool name has been registered through the MCP machinery.
func serverForTool(toolName string) string {
	rest := strings.TrimPrefix(toolName, "mcp__")
	if rest == toolName {
		return ""
	}
	index := strings.Index(rest, "__")
	if index <= 0 {
		return ""
	}
	return rest[:index]
}

// PinnedTools loads the deterministic cached surface before the session's
// first request. Live handshakes do not mutate this surface for known servers.
func (m *Manager) PinnedTools() ([]contract.Tool, error) {
	definitions, known, err := m.cachedDefinitions()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.pinned = definitions
	m.applied = cloneDefinitionsByName(definitions)
	m.knownAtStart = known
	tools := m.forwardingToolsLocked()
	m.mu.Unlock()
	return tools, nil
}

// RequestSurfaceRefresh asks the next task boundary to adopt the effective
// cached surface after an explicit mutating /mcp action.
func (m *Manager) RequestSurfaceRefresh(scope string) {
	m.mu.Lock()
	m.pendingSurface = true
	m.pendingForce = true
	if strings.TrimSpace(scope) != "" {
		m.pendingScopes = append(m.pendingScopes, scope)
	}
	m.mu.Unlock()
}

func (m *Manager) TakeBoundaryChange() (BoundaryChange, bool, error) {
	m.mu.RLock()
	pending, force := m.pendingSurface, m.pendingForce
	m.mu.RUnlock()
	if !pending {
		return BoundaryChange{}, false, nil
	}
	var desired map[string]contract.ToolDefinition
	if force {
		loaded, _, err := m.cachedDefinitions()
		if err != nil {
			return BoundaryChange{}, false, err
		}
		desired = loaded
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.pendingSurface {
		return BoundaryChange{}, false, nil
	}
	if desired != nil {
		m.pinned = desired
	}
	changed := !definitionsEqual(m.applied, m.pinned)
	scope := strings.Join(uniqueStrings(m.pendingScopes), "; ")
	m.pendingSurface, m.pendingForce, m.pendingScopes = false, false, nil
	if !changed {
		return BoundaryChange{}, false, nil
	}
	m.applied = cloneDefinitionsByName(m.pinned)
	if scope == "" {
		scope = "MCP tool surface changed"
	}
	return BoundaryChange{Tools: m.forwardingToolsLocked(), Scope: scope}, true, nil
}

func (m *Manager) cachedDefinitions() (map[string]contract.ToolDefinition, map[string]bool, error) {
	return m.surfaceForCurrent(true)
}

// surfaceForCurrent rebuilds the cached surface for the active MCP config,
// optionally limiting the work to the "known" servers (those already on disk
// from past sessions). knownOnly=false prefills the `known` map with every
// enabled server — useful so M5 can decide whether the lazy path is safe.
func (m *Manager) surfaceForCurrent(knownOnly bool) (map[string]contract.ToolDefinition, map[string]bool, error) {
	config, err := state.LoadMCPConfig(m.paths)
	if err != nil {
		return nil, nil, err
	}
	secrets, err := state.LoadMCPSecrets(m.paths)
	if err != nil {
		return nil, nil, err
	}
	definitions := make(map[string]contract.ToolDefinition)
	known := make(map[string]bool)
	if m.surfaceStore == nil {
		return definitions, known, nil
	}
	for _, server := range config.Servers {
		if !server.Enabled {
			continue
		}
		fingerprint := state.MCPServerFingerprint(server, secrets.Env[server.Name])
		snapshot, ok := m.surfaceStore.Get(fingerprint)
		if knownOnly {
			known[fingerprint] = ok
		} else if !ok {
			continue
		}
		for _, definition := range snapshot.Tools {
			definitions[definition.Function.Name] = canonicalDefinition(definition)
		}
	}
	return definitions, known, nil
}

// AllConfiguredServersHaveSurface (M5) reports whether every enabled MCP
// server in the current config has a cached pinned surface. Session boot can
// safely skip the eager Refresh and rely on the lazy per-tool path when this
// returns true. Unknown auth state or store errors fall back to "no" to avoid
// suppressing the warm-up round.
func (m *Manager) AllConfiguredServersHaveSurface() bool {
	_, known, err := m.surfaceForCurrent(false)
	if err != nil || len(known) == 0 {
		return false
	}
	for _, ok := range known {
		if !ok {
			return false
		}
	}
	return true
}

func (m *Manager) forwardingToolsLocked() []contract.Tool {
	names := make([]string, 0, len(m.pinned))
	for name := range m.pinned {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make([]contract.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, &forwardingTool{manager: m, definition: canonicalDefinition(m.pinned[name])})
	}
	return tools
}

// refreshDedup tracks the last refresh start time so M7 can short-circuit
// near-simultaneous callers instead of cancelling the in-flight round.
type refreshDedup struct {
	started  time.Time
	done     chan struct{}
	serverFP string
}

var debounceWindow = time.Second
