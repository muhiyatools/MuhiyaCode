package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// MCPServerInfo is the view model for one MCP server: its configuration plus
// the manager's live connection status.
type MCPServerInfo struct {
	Name      string
	Transport string // "stdio" | "http"
	Target    string // command+args, or url
	Enabled   bool
	OAuth     bool
	State     string // "ready" | "error" | "auth_required" | "disabled" | ...
	Status    string // human-readable message
	ToolCount int
}

// MCPAddSpec describes a server to register.
type MCPAddSpec struct {
	Name      string
	Transport string
	Command   string
	Args      []string
	URL       string
	OAuth     bool
}

// MCPActions is the typed backend the MCP management modal drives. Each call is
// safe to invoke from the TUI goroutine via actionCommand.
type MCPActions struct {
	List       func(context.Context) ([]MCPServerInfo, error)
	Add        func(context.Context, MCPAddSpec) error
	Remove     func(context.Context, string) error
	SetEnabled func(context.Context, string, bool) error
	Authorize  func(context.Context, string) error
	Test       func(context.Context, string) ([]MCPServerInfo, error)
}

// mcpActionResult is what an MCP mutation returns to handleAction: a message to
// flash plus the refreshed server list so the modal can redraw.
type mcpActionResult struct {
	message string
	servers []MCPServerInfo
	err     error
}

func (m *Model) openMCP() tea.Cmd {
	return actionCommand("mcp-list", func() (any, error) { return m.actions.MCP.List(m.ctx) })
}

// showMCPList renders the top-level server list once List resolves.
func (m *Model) showMCPList(value any) tea.Cmd {
	servers, _ := value.([]MCPServerInfo)
	choices := make([]contract.QuestionChoice, 0, len(servers)+2)
	for _, server := range servers {
		choices = append(choices, contract.QuestionChoice{Label: mcpLabel(server), Description: mcpDescription(server)})
	}
	choices = append(choices,
		contract.QuestionChoice{Label: "＋ Add server", Description: "Register a new stdio or HTTP MCP server"},
		contract.QuestionChoice{Label: "Close", Description: "Return to the session"},
	)
	captured := append([]MCPServerInfo(nil), servers...)
	m.openChoice("MCP servers", mcpSummary(captured), choices, func(index int) tea.Cmd {
		switch {
		case index < len(captured):
			m.openMCPServer(captured[index])
			return nil
		case index == len(captured):
			m.openMCPAdd()
			return nil
		default:
			return nil
		}
	})
	return nil
}

// openMCPServer shows the per-server action menu.
func (m *Model) openMCPServer(server MCPServerInfo) {
	toggle := "Disable"
	if !server.Enabled {
		toggle = "Enable"
	}
	choices := []contract.QuestionChoice{
		{Label: toggle, Description: "Turn this server on or off"},
		{Label: "Test connection", Description: "Reconnect and refresh status"},
	}
	if server.OAuth {
		choices = append(choices, contract.QuestionChoice{Label: "Authorize (OAuth)", Description: "Open the browser to sign in"})
	}
	choices = append(choices,
		contract.QuestionChoice{Label: "Remove", Description: "Delete this server and its stored credentials"},
		contract.QuestionChoice{Label: "Back", Description: "Return to the server list"},
	)
	name := server.Name
	enabled := server.Enabled
	oauth := server.OAuth
	m.openChoice("MCP · "+name, mcpDetail(server), choices, func(index int) tea.Cmd {
		label := choices[index].Label
		switch label {
		case "Enable", "Disable":
			target := !enabled
			return m.mcpAction(fmt.Sprintf("%sd %s.", strings.TrimSuffix(label, "e"), name), func() error {
				return m.actions.MCP.SetEnabled(m.ctx, name, target)
			})
		case "Test connection":
			return m.mcpTest(name)
		case "Authorize (OAuth)":
			if !oauth {
				return nil
			}
			return m.mcpAction("Authorized "+name+".", func() error { return m.actions.MCP.Authorize(m.ctx, name) })
		case "Remove":
			return m.mcpAction("Removed "+name+".", func() error { return m.actions.MCP.Remove(m.ctx, name) })
		default:
			return m.openMCP()
		}
	})
}

// openMCPAdd walks transport choice → details entry.
func (m *Model) openMCPAdd() {
	choices := []contract.QuestionChoice{
		{Label: "stdio", Description: "Local process, e.g. npx -y @modelcontextprotocol/server-filesystem ."},
		{Label: "http", Description: "Streamable HTTP endpoint (HTTPS, or localhost for development)"},
	}
	m.openChoice("Add MCP server", "Choose the transport.", choices, func(index int) tea.Cmd {
		if index == 0 {
			m.openText("Add stdio server", "Enter: <name> <command> [args...]\nExample: files npx -y @modelcontextprotocol/server-filesystem .", false, func(value string) tea.Cmd {
				return m.mcpAddStdio(value)
			})
		} else {
			m.openText("Add HTTP server", "Enter: <name> <url> [--oauth]\nExample: supabase https://mcp.supabase.com/mcp --oauth", false, func(value string) tea.Cmd {
				return m.mcpAddHTTP(value)
			})
		}
		return nil
	})
}

func (m *Model) mcpAddStdio(value string) tea.Cmd {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		m.warn("Add stdio server needs a name and a command.")
		return m.openMCP()
	}
	spec := MCPAddSpec{Name: fields[0], Transport: "stdio", Command: fields[1], Args: fields[2:]}
	return m.mcpAction("Registered "+fields[0]+".", func() error { return m.actions.MCP.Add(m.ctx, spec) })
}

func (m *Model) mcpAddHTTP(value string) tea.Cmd {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		m.warn("Add HTTP server needs a name and a URL.")
		return m.openMCP()
	}
	spec := MCPAddSpec{Name: fields[0], Transport: "http", URL: fields[1]}
	for _, arg := range fields[2:] {
		if arg == "--oauth" {
			spec.OAuth = true
		}
	}
	return m.mcpAction("Registered "+fields[0]+".", func() error { return m.actions.MCP.Add(m.ctx, spec) })
}

// mcpAction runs a mutation, then refreshes the list and reopens it.
func (m *Model) mcpAction(success string, run func() error) tea.Cmd {
	return actionCommand("mcp-action", func() (any, error) {
		if err := run(); err != nil {
			return mcpActionResult{err: err}, nil
		}
		servers, err := m.actions.MCP.List(m.ctx)
		return mcpActionResult{message: success, servers: servers}, err
	})
}

func (m *Model) mcpTest(name string) tea.Cmd {
	return actionCommand("mcp-action", func() (any, error) {
		servers, err := m.actions.MCP.Test(m.ctx, name)
		if err != nil {
			return mcpActionResult{err: err}, nil
		}
		message := "Tested " + name + "."
		for _, server := range servers {
			if server.Name == name {
				message = name + ": " + server.State
				if server.Status != "" {
					message += " · " + server.Status
				}
				break
			}
		}
		return mcpActionResult{message: message, servers: servers}, nil
	})
}

// afterMCPAction flashes the outcome and reopens the refreshed list.
func (m *Model) afterMCPAction(value any) tea.Cmd {
	result, ok := value.(mcpActionResult)
	if !ok {
		return nil
	}
	if result.err != nil {
		m.warn(result.err.Error())
	} else if result.message != "" {
		m.notify(result.message)
	}
	return m.showMCPList(result.servers)
}

func mcpLabel(server MCPServerInfo) string {
	dot := "○"
	switch server.State {
	case "ready", "connected":
		dot = "●"
	case "error", "auth_required":
		dot = "×"
	}
	if !server.Enabled {
		dot = "·"
	}
	return fmt.Sprintf("%s %s", dot, server.Name)
}

func mcpDescription(server MCPServerInfo) string {
	state := server.State
	if !server.Enabled {
		state = "disabled"
	}
	parts := []string{server.Transport, state}
	if server.ToolCount > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", server.ToolCount))
	}
	if server.Status != "" && server.State != "ready" && server.State != "connected" {
		parts = append(parts, server.Status)
	}
	return strings.Join(parts, " · ")
}

func mcpDetail(server MCPServerInfo) string {
	lines := []string{
		"Transport: " + server.Transport,
		"Target: " + server.Target,
		"Enabled: " + boolText(server.Enabled),
	}
	if server.OAuth {
		lines = append(lines, "OAuth: enabled")
	}
	status := server.State
	if server.Status != "" {
		status += " · " + server.Status
	}
	lines = append(lines, "Status: "+status)
	return strings.Join(lines, "\n")
}

func mcpSummary(servers []MCPServerInfo) string {
	if len(servers) == 0 {
		return "No MCP servers registered yet. Add one to expose its tools to the agent."
	}
	ready := 0
	for _, server := range servers {
		if server.Enabled && (server.State == "ready" || server.State == "connected") {
			ready++
		}
	}
	return fmt.Sprintf("%d server(s) configured · %d connected. Choose one to manage it.", len(servers), ready)
}

func boolText(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
