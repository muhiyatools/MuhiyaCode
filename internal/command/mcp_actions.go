package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/mcpclient"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/muhiya/muhiyacode/internal/tui"
)

func (a *Application) mcpActions() tui.MCPActions {
	return tui.MCPActions{
		List: func(_ context.Context) ([]tui.MCPServerInfo, error) { return a.mcpServerInfos() },
		// Refresh (D2/T033) kicks a background manager refresh when the modal
		// opens so lazy-connect servers begin connecting; the modal's M2 tick then
		// observes them settle. Short-budget (non-blocking) like the startup path.
		Refresh: func(ctx context.Context) { a.refreshMCP(ctx, "mcp modal open") },
		Add: func(ctx context.Context, spec tui.MCPAddSpec) error {
			name := state.SanitizeMCPName(spec.Name)
			if name == "" {
				return errors.New("MCP server name is invalid")
			}
			var server state.MCPServer
			switch spec.Transport {
			case "stdio":
				if strings.TrimSpace(spec.Command) == "" {
					return errors.New("stdio server needs a command")
				}
				server = state.MCPServer{Name: name, Enabled: true, TimeoutMS: 30_000, Transport: "stdio", Command: spec.Command, Args: spec.Args}
			case "http":
				server = state.MCPServer{Name: name, Enabled: true, TimeoutMS: 30_000, Transport: "http", URL: spec.URL, OAuth: &state.MCPOAuth{Enabled: spec.OAuth, RedirectPort: 35698}}
			default:
				return errors.New("transport must be stdio or http")
			}
			if err := state.UpsertMCPServer(server, a.paths); err != nil {
				return err
			}
			a.refreshMCP(ctx, "mcp add "+name)
			return nil
		},
		Remove: func(ctx context.Context, name string) error {
			removed, err := state.RemoveMCPServer(name, a.paths)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("MCP server not found: %s", name)
			}
			a.refreshMCP(ctx, "mcp remove "+name)
			return nil
		},
		SetEnabled: func(ctx context.Context, name string, enabled bool) error {
			if err := a.setMCPEnabled(name, enabled); err != nil {
				return err
			}
			a.refreshMCP(ctx, fmt.Sprintf("mcp %s %s", map[bool]string{true: "enable", false: "disable"}[enabled], name))
			return nil
		},
		Authorize: func(ctx context.Context, name string) error {
			a.mu.Lock()
			manager := a.activeMCP
			a.mu.Unlock()
			if manager == nil {
				return errors.New("MCP is disabled for this session")
			}
			if err := manager.Authorize(ctx, name, mcpclient.AuthorizeOptions{OnURL: func(url string) {
				if a.callbacks.Status != nil {
					a.callbacks.Status("Open the browser to authorize MCP: " + url)
				}
			}}); err != nil {
				return err
			}
			// M1: blocking refresh so the modal snapshot the test code
			// reads right after shows `connected`, not `connecting`.
			// D3/T034: bound the blocking wait to 30s so a wedged server cannot
			// stall the Authorize action indefinitely.
			waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			manager.RefreshBlocking(waitCtx)
			cancel()
			a.emitMCPProblems(manager)
			return nil
		},
		Test: func(ctx context.Context, _ string) ([]tui.MCPServerInfo, error) {
			a.mu.Lock()
			manager := a.activeMCP
			a.mu.Unlock()
			if manager != nil {
				// M1: blocking refresh so the modal reflects the final
				// state immediately rather than capturing mid-connect.
				// D3/T034: bound the wait to 30s so a wedged server cannot stall
				// the Test action indefinitely.
				waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				manager.RefreshBlocking(waitCtx)
				cancel()
				a.emitMCPProblems(manager)
			}
			return a.mcpServerInfos()
		},
	}
}

// mcpServerInfos merges the persisted MCP configuration with the manager's live
// connection status for the management modal.
func (a *Application) mcpServerInfos() ([]tui.MCPServerInfo, error) {
	config, err := state.LoadMCPConfig(a.paths)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	manager := a.activeMCP
	a.mu.Unlock()
	statuses := map[string]mcpclient.Status{}
	if manager != nil {
		for _, status := range manager.Statuses() {
			statuses[status.Name] = status
		}
	}
	infos := make([]tui.MCPServerInfo, 0, len(config.Servers))
	for _, server := range config.Servers {
		info := tui.MCPServerInfo{Name: server.Name, Transport: server.Transport, Enabled: server.Enabled}
		if server.Transport == "stdio" {
			info.Target = strings.TrimSpace(server.Command + " " + strings.Join(server.Args, " "))
		} else {
			info.Target = server.URL
			info.OAuth = server.OAuth != nil && server.OAuth.Enabled
		}
		switch {
		case !server.Enabled:
			info.State = "disabled"
		default:
			info.State = "not connected"
		}
		if status, ok := statuses[server.Name]; ok {
			info.State, info.Status, info.ToolCount = status.State, status.Message, status.ToolCount
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (a *Application) setMCPEnabled(name string, enabled bool) error {
	config, err := state.LoadMCPConfig(a.paths)
	if err != nil {
		return err
	}
	for i := range config.Servers {
		if config.Servers[i].Name == name {
			config.Servers[i].Enabled = enabled
			return state.SaveMCPConfig(config, a.paths)
		}
	}
	return fmt.Errorf("MCP server not found: %s", name)
}

func (a *Application) refreshMCP(ctx context.Context, scope string) {
	a.mu.Lock()
	manager := a.activeMCP
	a.mu.Unlock()
	if manager == nil {
		return
	}
	manager.RequestSurfaceRefresh(scope)
	manager.Refresh(ctx, a.mcpWait)
	a.emitMCPProblems(manager)
}

// emitMCPProblems surfaces only servers that failed or need authorization, so
// healthy servers stay silent and the notice line is not spammed on startup.
func (a *Application) emitMCPProblems(manager *mcpclient.Manager) {
	if a.callbacks.MCPStatus == nil || manager == nil {
		return
	}
	for _, status := range manager.Statuses() {
		if status.State == "error" || status.State == "auth_required" {
			a.callbacks.MCPStatus(fmt.Sprintf("%s: %s", status.Name, status.Message))
		}
	}
}

func formatMCPList(paths state.Paths) (string, error) {
	config, err := state.LoadMCPConfig(paths)
	if err != nil {
		return "", err
	}
	if len(config.Servers) == 0 {
		return "No MCP servers registered.", nil
	}
	lines := make([]string, 0, len(config.Servers))
	for _, server := range config.Servers {
		target := server.URL
		auth := ""
		if server.Transport == "stdio" {
			target = strings.TrimSpace(server.Command + " " + strings.Join(server.Args, " "))
		} else if server.OAuth != nil && server.OAuth.Enabled {
			auth = " oauth"
		}
		status := "off"
		if server.Enabled {
			status = "on "
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s%s  %s", status, server.Name, server.Transport, auth, target))
	}
	return strings.Join(lines, "\n"), nil
}
