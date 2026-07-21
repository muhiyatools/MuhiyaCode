package workspace

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Path        string
}

// LoadSkillInstructions reads a discovered skill with a strict size bound so a
// selected skill cannot unexpectedly consume the entire model context.
func LoadSkillInstructions(skill Skill, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 32 * 1024
	}
	// Resolve symlinks and refuse a SKILL.md that escapes into a protected
	// credential root (I-5). read_skill is name-keyed and catalog-bounded, but a
	// SKILL.md symlinked to ~/.ssh/id_rsa would otherwise be read and returned.
	real := skill.Path
	if resolved, err := filepath.EvalSymlinks(skill.Path); err == nil {
		real = resolved
	}
	for _, root := range defaultSensitiveRoots() {
		if canonical, err := CanonicalPath(root); err == nil && IsInside(canonical, real) {
			return "", fmt.Errorf("skill %s resolves into a protected credential location", skill.Name)
		}
	}
	file, err := os.Open(real)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("skill %s exceeds %d bytes", skill.Name, maxBytes)
	}
	return strings.TrimSpace(string(data)), nil
}

// SkillRoots returns every skill location in precedence order (013 contract
// skills-autouse §1.1): workspace roots first, then user-configured roots, then
// the two home roots. Precedence matters because discovery is FIRST-WINS on the
// lowercased name — a project-local skill deliberately shadows a same-named
// global one, so a workspace can override house defaults.
//
// This ordering is the single source of truth for both the manual /skills flow
// and the session skill catalog the model sees, so the two can never disagree
// about which file a name resolves to.
func SkillRoots(workspace string, extraRoots []string) []string {
	home, _ := os.UserHomeDir()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" && home != "" {
		codexHome = filepath.Join(home, ".codex")
	}
	roots := []string{
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(workspace, ".codex", "skills"),
	}
	roots = append(roots, extraRoots...)
	if value := os.Getenv("MUHIYA_SKILLS_DIR"); value != "" {
		roots = append(roots, filepath.SplitList(value)...)
	}
	if codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"))
	}
	if home != "" {
		roots = append(roots, filepath.Join(home, ".agents", "skills"))
	}
	return roots
}

// DiscoverSkills walks every root from SkillRoots and returns the deduplicated
// catalog. Unreadable roots, malformed files, and files that are not SKILL.md
// are skipped silently (013 FR-009): one broken skill must never take down the
// catalog, the session, or a task.
func DiscoverSkills(workspace string, extraRoots []string, limit int) ([]Skill, error) {
	if limit <= 0 {
		limit = 40
	}
	roots := SkillRoots(workspace, extraRoots)
	seen := make(map[string]bool)
	var result []Skill
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if path != root && strings.Count(filepath.Clean(path), string(filepath.Separator))-strings.Count(filepath.Clean(root), string(filepath.Separator)) > 4 {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(entry.Name(), "SKILL.md") || len(result) >= limit {
				return nil
			}
			skill, ok := readSkill(path)
			if !ok || seen[strings.ToLower(skill.Name)] {
				return nil
			}
			seen[strings.ToLower(skill.Name)] = true
			result = append(result, skill)
			return nil
		})
		if len(result) >= limit {
			break
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

func readSkill(path string) (Skill, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Skill{}, false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 16*1024), 256*1024)
	frontmatter := false
	values := make(map[string]string)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			if frontmatter {
				break
			}
			frontmatter = true
			continue
		}
		if !frontmatter {
			continue
		}
		if index := strings.IndexByte(line, ':'); index > 0 {
			key := strings.TrimSpace(line[:index])
			value := strings.Trim(strings.TrimSpace(line[index+1:]), `"'`)
			values[key] = value
		}
	}
	name := strings.TrimSpace(values["name"])
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	if name == "" {
		return Skill{}, false
	}
	return Skill{Name: name, Description: strings.TrimSpace(values["description"]), Path: path}, true
}
