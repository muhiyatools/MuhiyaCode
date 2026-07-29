package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// fileLeaseSet prevents concurrent workers from writing overlapping files or
// directory trees. A "*" lease represents an unscoped shell/MCP mutation.
type fileLeaseSet struct {
	mu     sync.Mutex
	leases map[string][]string
}

func newFileLeaseSet() *fileLeaseSet {
	return &fileLeaseSet{leases: make(map[string][]string)}
}

func (set *fileLeaseSet) acquire(owner, workspace string, paths []string) error {
	normalized := normalizeLeasePaths(workspace, paths)
	set.mu.Lock()
	defer set.mu.Unlock()
	for other, held := range set.leases {
		if other == owner {
			continue
		}
		for _, requested := range normalized {
			for _, existing := range held {
				if leasePathsOverlap(requested, existing) {
					return fmt.Errorf("write lease conflict with %s on %s", other, requested)
				}
			}
		}
	}
	set.leases[owner] = normalized
	return nil
}

func (set *fileLeaseSet) release(owner string) {
	set.mu.Lock()
	delete(set.leases, owner)
	set.mu.Unlock()
}

func normalizeLeasePaths(workspace string, paths []string) []string {
	if len(paths) == 0 {
		return []string{"*"}
	}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(workspace, path)
		}
		result = append(result, filepath.Clean(path))
	}
	return result
}

func leasePathsOverlap(left, right string) bool {
	if left == "*" || right == "*" {
		return true
	}
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}
