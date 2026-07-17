package orchestrator

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Dynamic research fan-out: the number of explore subagents the pipeline
// dispatches should reflect the real workspace, not a fixed per-class constant.
// The most wasteful static case is a from-scratch build in an empty folder — the
// old path still fanned out 2–3 explore agents to "map the relevant packages" of
// a directory that has none. workspaceHasCodeToResearch lets runPipelineResearch
// skip the fan-out entirely when there is nothing to inspect, the way a capable
// agent decides no research is warranted rather than delegating blindly.

// researchWorthinessMinFiles is the number of existing source files at or above
// which a workspace is considered to have real code worth researching. Below it
// (an empty or scaffold-only folder — nothing, or just go.mod/README) the
// research phase is skipped and planning proceeds on the task description.
const researchWorthinessMinFiles = 3

// researchSourceExtensions are the file kinds that count as "existing code to
// research". Config/lockfiles/docs are deliberately excluded so a folder holding
// only go.mod + README still reads as greenfield.
var researchSourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".cs": true,
	".cpp": true, ".cc": true, ".c": true, ".h": true, ".hpp": true, ".php": true,
	".swift": true, ".scala": true, ".vue": true, ".svelte": true, ".css": true,
	".scss": true, ".html": true, ".sql": true, ".sh": true, ".lua": true, ".dart": true,
}

// researchSkipDirs are directories that never hold the user's own source and
// would only inflate the probe (or make it slow). Hidden dirs are skipped too.
var researchSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	".next": true, ".nuxt": true, ".venv": true, "venv": true, "__pycache__": true,
	"bin": true, "obj": true, "out": true, "coverage": true, ".turbo": true,
}

// Codebase-size buckets for scaling the generic research lenses. The generic
// lenses (architecture / change surface / acceptance) each re-read largely the
// same files, so a small project needs only a single pass while a larger one
// warrants two or three distinct angles.
const (
	researchLensProbeCeiling    = 26 // bounded probe: enough to tell small/medium/large apart
	researchSmallCodebaseFiles  = 6  // ≤ this ⇒ one lens suffices
	researchMediumCodebaseFiles = 20 // ≤ this ⇒ two lenses
)

// implementationCoherentMax is the largest number of DEPENDENT pending steps the
// implementation phase runs as a single coherent agent rather than fragmenting
// across isolated subagents. Above it, a dependent plan keeps its bounded
// sequential chunking so no single agent is overloaded.
const implementationCoherentMax = 6

// dynamicResearchLensCount scales the number of generic research lenses to the
// size of the existing codebase, so a small project is not fanned out into three
// near-identical full-repo reads (a live run once burned ~915k tokens doing
// exactly that over a 14-file project). Explicit disjoint directory scopes bypass
// this entirely — they genuinely fan out per directory. Only reached for a
// non-greenfield workspace, so the count is always ≥ 1.
func (e *Engine) dynamicResearchLensCount() int {
	root := strings.TrimSpace(e.session.WorkspacePath)
	if root == "" {
		return 1
	}
	switch files := countWorkspaceSourceFiles(root, researchLensProbeCeiling); {
	case files <= researchSmallCodebaseFiles:
		return 1
	case files <= researchMediumCodebaseFiles:
		return 2
	default:
		return 3
	}
}

// workspaceHasCodeToResearch reports whether the workspace holds enough existing
// source to warrant dispatching explore subagents. It returns false for an empty
// or scaffold-only folder (a greenfield build), so the research phase can skip
// straight to planning instead of mapping a directory with no code. The scan is
// shallow-bounded — it stops the moment it has seen researchWorthinessMinFiles
// source files — so it never becomes a cost of its own on a large repo.
func (e *Engine) workspaceHasCodeToResearch() bool {
	root := strings.TrimSpace(e.session.WorkspacePath)
	if root == "" {
		return false
	}
	return countWorkspaceSourceFiles(root, researchWorthinessMinFiles) >= researchWorthinessMinFiles
}

// countWorkspaceSourceFiles walks root counting source files, skipping vendor and
// hidden directories, and stops as soon as the count reaches stopAt (so the probe
// is O(stopAt), not O(repo)). An unreadable entry is skipped, never fatal — the
// probe must never break the pipeline.
func countWorkspaceSourceFiles(root string, stopAt int) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if researchSkipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if researchSourceExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
			count++
			if count >= stopAt {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return count
}
