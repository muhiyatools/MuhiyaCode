package arch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// maxFileLines is the per-file budget (feature 010 MS-1): ~¼ of the pre-rework
// largest file (engine.go, 3152). Every non-test .go file under the roots below
// must fit, EXCEPT the explicit allowlist — which the US4 split tasks empty.
const maxFileLines = 800

// sizeAllowlist maps a module-relative (forward-slash) path to its grandfathered
// line count. The feature-010 US4 split tasks (T030-T033) reduced every
// originally-seeded offender under the 800-line budget, so the ratchet is now
// EMPTY — the size test enforces ≤800 with zero exceptions. Do NOT add entries
// for new files — split them instead.
var sizeAllowlist = map[string]int{}

var sizeRoots = []string{"internal", "cmd", "benchmarks"}

func TestNoSourceFileExceedsBudget(t *testing.T) {
	root := moduleRoot(t)
	for _, dir := range sizeRoots {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel := filepath.ToSlash(mustRel(t, root, path))
			lines := countLines(t, path)
			budget := maxFileLines
			if allow, ok := sizeAllowlist[rel]; ok {
				budget = allow
			}
			if lines > budget {
				if _, allowed := sizeAllowlist[rel]; allowed {
					t.Errorf("%s grew to %d lines, over its grandfathered ceiling %d — split it (allowlist entries only shrink)", rel, lines, budget)
				} else {
					t.Errorf("%s is %d lines, over the %d budget — split it into single-responsibility files (MS-1). Do not add an allowlist entry for a new file", rel, lines, maxFileLines)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return n
}

func mustRel(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatalf("rel %s %s: %v", base, target, err)
	}
	return rel
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod, so the arch tests are path-independent.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}
