package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyLifecycleTokens are the pre-feature-010 lifecycle types that survive
// ONLY as sidecar-migration inputs (a task's state is now the single
// contract.LifecycleState). They must appear in exactly one production file.
var legacyLifecycleTokens = []string{"contract.PlanPhase", "contract.PipelinePhase"}

// compatBoundaryAllowlist is the ONLY production file allowed to reference the
// legacy lifecycle types: the sidecar migration loader (feature 010 UL-8/T016).
// The list must not grow — a new referencer means the two-truth state leaked
// back out of the compatibility layer.
var compatBoundaryAllowlist = map[string]bool{
	"internal/state/lifecycle_migrate.go": true,
}

// TestLegacyLifecycleConfinedToCompatLayer is the UL-8 grep proof (SC-001):
// the legacy PlanPhase/PipelinePhase machinery lives only inside the migration
// loader, so exactly one authoritative lifecycle state remains everywhere else.
func TestLegacyLifecycleConfinedToCompatLayer(t *testing.T) {
	root := moduleRoot(t)
	base := filepath.Join(root, "internal")
	err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		// The contract package DEFINES the legacy types (unqualified) for the
		// migration to consume; qualified `contract.PlanPhase` references are what
		// we confine, so the definitions never match here.
		if compatBoundaryAllowlist[rel] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(data)
		for _, tok := range legacyLifecycleTokens {
			if strings.Contains(src, tok) {
				t.Errorf("%s references legacy lifecycle type %q — it must live only in the compat migration loader (UL-8). Use contract.LifecycleState", rel, tok)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
}
