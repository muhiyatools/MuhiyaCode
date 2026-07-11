package state

import (
	"fmt"
	"regexp"
	"strings"
)

type MCPConfig struct {
	Version int         `json:"version"`
	Servers []MCPServer `json:"servers"`
}

type MCPServer struct {
	Transport string            `json:"transport"`
	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	TimeoutMS int               `json:"timeoutMs"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	OAuth     *MCPOAuth         `json:"oauth,omitempty"`
}

type MCPOAuth struct {
	Enabled      bool   `json:"enabled"`
	Scope        string `json:"scope,omitempty"`
	RedirectPort int    `json:"redirectPort"`
}

type MCPSecrets struct {
	OAuth map[string]map[string]any    `json:"oauth"`
	Env   map[string]map[string]string `json:"env"`
}

func DefaultMCPConfig() MCPConfig { return MCPConfig{Version: 1, Servers: []MCPServer{}} }
func DefaultMCPSecrets() MCPSecrets {
	return MCPSecrets{OAuth: make(map[string]map[string]any), Env: make(map[string]map[string]string)}
}

func LoadMCPConfig(paths ...Paths) (MCPConfig, error) {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return MCPConfig{}, err
	}
	config, err := readJSON(p.MCPFile, DefaultMCPConfig())
	if err != nil {
		return MCPConfig{}, err
	}
	if config.Version != 1 {
		return MCPConfig{}, fmt.Errorf("unsupported mcp.json version %d", config.Version)
	}
	for i := range config.Servers {
		normalizeMCPServer(&config.Servers[i])
		if err := validateMCPServer(config.Servers[i]); err != nil {
			return MCPConfig{}, fmt.Errorf("invalid MCP server %d: %w", i, err)
		}
	}
	return config, nil
}

func SaveMCPConfig(config MCPConfig, paths ...Paths) error {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return err
	}
	if config.Version == 0 {
		config.Version = 1
	}
	if config.Servers == nil {
		config.Servers = []MCPServer{}
	}
	for i := range config.Servers {
		normalizeMCPServer(&config.Servers[i])
		if err := validateMCPServer(config.Servers[i]); err != nil {
			return err
		}
	}
	return writeJSON(p.MCPFile, config, false)
}

func LoadMCPSecrets(paths ...Paths) (MCPSecrets, error) {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return MCPSecrets{}, err
	}
	secrets, err := readJSON(p.MCPSecretsFile, DefaultMCPSecrets())
	if secrets.OAuth == nil {
		secrets.OAuth = make(map[string]map[string]any)
	}
	if secrets.Env == nil {
		secrets.Env = make(map[string]map[string]string)
	}
	return secrets, err
}

func SaveMCPSecrets(secrets MCPSecrets, paths ...Paths) error {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return err
	}
	if secrets.OAuth == nil {
		secrets.OAuth = make(map[string]map[string]any)
	}
	if secrets.Env == nil {
		secrets.Env = make(map[string]map[string]string)
	}
	return writeJSON(p.MCPSecretsFile, secrets, true)
}

func UpsertMCPServer(server MCPServer, paths ...Paths) error {
	server.Name = SanitizeMCPName(server.Name)
	normalizeMCPServer(&server)
	if err := validateMCPServer(server); err != nil {
		return err
	}
	config, err := LoadMCPConfig(paths...)
	if err != nil {
		return err
	}
	found := false
	for i := range config.Servers {
		if config.Servers[i].Name == server.Name {
			config.Servers[i] = server
			found = true
			break
		}
	}
	if !found {
		config.Servers = append(config.Servers, server)
	}
	return SaveMCPConfig(config, paths...)
}

func RemoveMCPServer(name string, paths ...Paths) (bool, error) {
	config, err := LoadMCPConfig(paths...)
	if err != nil {
		return false, err
	}
	next := config.Servers[:0]
	removed := false
	for _, server := range config.Servers {
		if server.Name == name {
			removed = true
			continue
		}
		next = append(next, server)
	}
	if !removed {
		return false, nil
	}
	config.Servers = next
	if err := SaveMCPConfig(config, paths...); err != nil {
		return false, err
	}
	secrets, err := LoadMCPSecrets(paths...)
	if err != nil {
		return false, err
	}
	delete(secrets.OAuth, name)
	delete(secrets.Env, name)
	return true, SaveMCPSecrets(secrets, paths...)
}

func SetMCPEnv(name string, values map[string]string, paths ...Paths) error {
	secrets, err := LoadMCPSecrets(paths...)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		delete(secrets.Env, name)
	} else {
		secrets.Env[name] = values
	}
	return SaveMCPSecrets(secrets, paths...)
}

var mcpNameCleaner = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func normalizeMCPServer(server *MCPServer) {
	server.Transport = strings.ToLower(strings.TrimSpace(server.Transport))
	if server.TimeoutMS <= 0 {
		server.TimeoutMS = 30_000
	}
	if server.Transport == "stdio" && server.Env == nil {
		server.Env = map[string]string{}
	}
	if server.Transport == "http" {
		if server.OAuth == nil {
			server.OAuth = &MCPOAuth{RedirectPort: 35698}
		} else if server.OAuth.RedirectPort == 0 {
			server.OAuth.RedirectPort = 35698
		}
	}
}

func SanitizeMCPName(value string) string {
	value = strings.Trim(mcpNameCleaner.ReplaceAllString(strings.TrimSpace(value), "_"), "_")
	if len(value) > 48 {
		value = value[:48]
	}
	return value
}

func validateMCPServer(server MCPServer) error {
	if server.Name == "" || SanitizeMCPName(server.Name) != server.Name {
		return fmt.Errorf("server name is invalid")
	}
	if server.TimeoutMS <= 0 {
		return fmt.Errorf("timeoutMs must be positive")
	}
	switch server.Transport {
	case "stdio":
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("stdio command is required")
		}
	case "http":
		if !strings.HasPrefix(server.URL, "https://") && !strings.HasPrefix(server.URL, "http://127.0.0.1") && !strings.HasPrefix(server.URL, "http://localhost") {
			return fmt.Errorf("HTTP MCP URL must use HTTPS (localhost is allowed for development)")
		}
		if server.OAuth == nil {
			return fmt.Errorf("HTTP OAuth configuration is missing")
		}
		if server.OAuth.RedirectPort < 1024 || server.OAuth.RedirectPort > 65535 {
			return fmt.Errorf("OAuth redirect port must be 1024-65535")
		}
	default:
		return fmt.Errorf("transport must be stdio or http")
	}
	return nil
}
