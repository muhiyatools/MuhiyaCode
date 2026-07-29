package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const HomeEnvironment = "MUHIYA_HOME"

// Paths is the stable on-disk compatibility contract shared with the original
// MuhiyaCode implementation. New state must continue to use these names so an
// upgrade can open an existing ~/.muhiya directory in place.
type Paths struct {
	Home             string
	SettingsFile     string
	SecretsFile      string
	MCPFile          string
	MCPSecretsFile   string
	StateDir         string
	DBFile           string
	SessionsDir      string
	ProjectsDir      string
	CacheDir         string
	ModelCatalogFile string
	LogsDir          string
	TmpDir           string
}

// DefaultPaths resolves MUHIYA_HOME on every call, which makes tests and
// embedded callers deterministic without package-global path caches.
func DefaultPaths() (Paths, error) {
	home := os.Getenv(HomeEnvironment)
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user home: %w", err)
		}
		home = filepath.Join(userHome, ".muhiya")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve Muhiya home: %w", err)
	}
	home = filepath.Clean(abs)
	stateDir := filepath.Join(home, "state")
	return Paths{
		Home:             home,
		SettingsFile:     filepath.Join(home, "settings.json"),
		SecretsFile:      filepath.Join(home, "secrets.json"),
		MCPFile:          filepath.Join(home, "mcp.json"),
		MCPSecretsFile:   filepath.Join(home, "mcp-secrets.json"),
		StateDir:         stateDir,
		DBFile:           filepath.Join(stateDir, "muhiyacode.sqlite"),
		SessionsDir:      filepath.Join(home, "sessions"),
		ProjectsDir:      filepath.Join(home, "projects"),
		CacheDir:         filepath.Join(home, "cache"),
		ModelCatalogFile: filepath.Join(home, "cache", "model-catalog-v2.json"),
		LogsDir:          filepath.Join(home, "logs"),
		TmpDir:           filepath.Join(home, "tmp"),
	}, nil
}

func EnsurePaths(paths ...Paths) (Paths, error) {
	p, err := selectPaths(paths)
	if err != nil {
		return Paths{}, err
	}
	for _, dir := range []string{p.Home, p.StateDir, p.SessionsDir, p.ProjectsDir, p.CacheDir, p.LogsDir, p.TmpDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create state directory %s: %w", dir, err)
		}
	}
	return p, nil
}

func selectPaths(paths []Paths) (Paths, error) {
	if len(paths) > 1 {
		return Paths{}, fmt.Errorf("expected at most one Paths value, got %d", len(paths))
	}
	if len(paths) == 1 {
		if paths[0].Home == "" {
			return Paths{}, errors.New("home path is empty")
		}
		return paths[0], nil
	}
	return DefaultPaths()
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func SessionDir(p Paths, sessionID string) (string, error) {
	if !safeID.MatchString(sessionID) {
		return "", fmt.Errorf("invalid session id: %s", sessionID)
	}
	return filepath.Join(p.SessionsDir, sessionID), nil
}

// projectSlug collapses any run of non-[a-z0-9] characters to a single dash.
var projectSlug = regexp.MustCompile(`[^a-z0-9]+`)

// ProjectID maps a canonical workspace key (an already-normalized absolute path —
// see workspace.WorkspaceKey) to a deterministic, human-findable, collision-safe
// directory name for that project's memory store (Experience Overhaul B1). It is
// "<sanitized-basename>-<sha256[:8]>": the basename makes the directory legible,
// the hash disambiguates two projects that share a basename. Because the input is
// already case-normalized upstream, two casings of one path yield one ID.
func ProjectID(workspaceKey string) string {
	sum := sha256.Sum256([]byte(workspaceKey))
	hash8 := hex.EncodeToString(sum[:])[:8]
	base := projectSlug.ReplaceAllString(strings.ToLower(filepath.Base(workspaceKey)), "-")
	base = strings.Trim(base, "-")
	if len(base) > 32 {
		base = strings.Trim(base[:32], "-")
	}
	if base == "" {
		base = "project"
	}
	return base + "-" + hash8
}

// ProjectMemoryDir is the per-project memory store directory:
// ~/.muhiya/projects/<project-id>/memory. It is not created here; the first write
// (save_memory / migration) creates it.
func ProjectMemoryDir(p Paths, workspaceKey string) string {
	return filepath.Join(p.ProjectsDir, ProjectID(workspaceKey), "memory")
}
