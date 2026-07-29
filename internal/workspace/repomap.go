package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// RepoMapEntry represents the top-level symbol outline of one file for the repo map.
type RepoMapEntry struct {
	RelPath string
	Symbols []string
	Score   int
}

// GenerateRepoMap constructs a compressed structural outline of top-level symbols
// across the workspace, budgeted strictly under maxTokens (default 1000 tokens).
func (w *Workspace) GenerateRepoMap(rootPath string, maxTokens int) (string, error) {
	return w.GenerateRankedRepoMap(rootPath, maxTokens, "")
}

// GenerateRankedRepoMap prioritizes paths and symbols related to the task query
// while preserving deterministic tie ordering.
func (w *Workspace) GenerateRankedRepoMap(rootPath string, maxTokens int, query string) (string, error) {
	if maxTokens <= 0 {
		maxTokens = 1000
	}

	target, err := w.authorizePath(context.Background(), ActionSearch, rootPath)
	if err != nil {
		return "", err
	}

	files, err := CollectSourceFiles(target, false)
	if err != nil || len(files) == 0 {
		return "Repo Map: No indexable source files found.", nil
	}

	var entries []RepoMapEntry

	for _, file := range files {
		relPath := relativeSlash(w.root, file)
		outline, err := w.sourceCache.outline(file)
		if err != nil || outline == nil || len(outline.Symbols) == 0 {
			continue
		}

		var syms []string
		for _, sym := range outline.Symbols {
			if sym.Name != "" {
				syms = append(syms, fmt.Sprintf("%s L%d: %s", sym.Kind, sym.Line, sym.Name))
			}
		}

		if len(syms) > 0 {
			entries = append(entries, RepoMapEntry{
				RelPath: relPath,
				Symbols: syms,
				Score:   repoMapScore(relPath, syms, query),
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].RelPath < entries[j].RelPath
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Workspace Scope Map (%s) ===\n", filepath.Base(w.root)))

	// Budget approx 4 chars per token -> maxChars = maxTokens * 4
	maxChars := maxTokens * 4
	writtenChars := b.Len()

	for _, entry := range entries {
		lineHeader := fmt.Sprintf("\n📄 %s\n", entry.RelPath)
		if writtenChars+len(lineHeader) >= maxChars {
			b.WriteString("\n... (repo map truncated to stay inside token budget)\n")
			break
		}
		b.WriteString(lineHeader)
		writtenChars += len(lineHeader)

		for _, sym := range entry.Symbols {
			symLine := fmt.Sprintf("  ├─ %s\n", sym)
			if writtenChars+len(symLine) >= maxChars {
				b.WriteString("  └─ ... (more symbols truncated)\n")
				writtenChars += 35
				break
			}
			b.WriteString(symLine)
			writtenChars += len(symLine)
		}
	}

	return b.String(), nil
}

func repoMapScore(path string, symbols []string, query string) int {
	lowerPath := strings.ToLower(path)
	score := max(0, 20-strings.Count(filepath.ToSlash(path), "/")*2)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		term = strings.Trim(term, ".,:;()[]{}")
		if len(term) < 2 {
			continue
		}
		if strings.Contains(lowerPath, term) {
			score += 40
		}
		for _, symbol := range symbols {
			if strings.Contains(strings.ToLower(symbol), term) {
				score += 12
			}
		}
	}
	return score
}
