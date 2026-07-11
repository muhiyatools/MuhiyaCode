package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// CanonicalPath returns an absolute, symlink-resolved path. For a path that
// does not exist yet, it resolves the nearest existing ancestor and appends
// the missing suffix. This is essential for securely validating new files.
func CanonicalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("workspace: empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Abs(real)
	}

	current := abs
	var suffix []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// The volume/root should always exist. Preserve the original error
			// semantics if an unusual virtual filesystem says otherwise.
			return abs, nil
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
		if real, err := filepath.EvalSymlinks(current); err == nil {
			parts := make([]string, 0, len(suffix)+1)
			parts = append(parts, real)
			for i := len(suffix) - 1; i >= 0; i-- {
				parts = append(parts, suffix[i])
			}
			return filepath.Abs(filepath.Join(parts...))
		}
	}
}

func normalizePath(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

// IsInside reports whether child is parent or a descendant. Both arguments
// should already be canonical when symlink-aware containment is required.
func IsInside(parent, child string) bool {
	p := normalizePath(parent)
	c := normalizePath(child)
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func IsInsideReal(parent, child string) (bool, error) {
	p, err := CanonicalPath(parent)
	if err != nil {
		return false, err
	}
	c, err := CanonicalPath(child)
	if err != nil {
		return false, err
	}
	return IsInside(p, c), nil
}

func Resolve(root, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		requested = "."
	}
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, filepath.FromSlash(requested))
	}
	return filepath.Abs(filepath.Clean(requested))
}

func relativeSlash(root, target string) string {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "" {
		return "."
	}
	return filepath.ToSlash(rel)
}

type pathLedger struct {
	mu    sync.RWMutex
	paths map[string]struct{}
}

func newPathLedger() *pathLedger { return &pathLedger{paths: make(map[string]struct{})} }

func (l *pathLedger) add(path string) {
	l.mu.Lock()
	l.paths[normalizePath(path)] = struct{}{}
	l.mu.Unlock()
}

func (l *pathLedger) has(path string) bool {
	l.mu.RLock()
	_, ok := l.paths[normalizePath(path)]
	l.mu.RUnlock()
	return ok
}

func defaultSensitiveRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	muhiya := os.Getenv("MUHIYA_HOME")
	if muhiya == "" {
		muhiya = filepath.Join(home, ".muhiya")
	}
	return []string{
		muhiya,
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".azure"),
		filepath.Join(home, ".kube"),
	}
}
