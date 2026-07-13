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
	file, err := os.Open(skill.Path)
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

// DiscoverWorkspaceSkills discovers only workspace-resident skills (under
// <workspace>/.agents/skills and <workspace>/.codex/skills), for the stable-
// prefix listing (003 D12). Restricting to workspace roots keeps the model's
// read_file calls inside the permission-guard containment. Env/home roots are
// intentionally excluded here — they remain reachable via the manual /skills
// flow only. Ordering is deterministic (stable sort by lowercased name).
func DiscoverWorkspaceSkills(workspace string, limit int) ([]Skill, error) {
	if limit <= 0 {
		limit = 40
	}
	seen := make(map[string]bool)
	var result []Skill
	for _, root := range []string{
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(workspace, ".codex", "skills"),
	} {
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

func DiscoverSkills(workspace string, extraRoots []string, limit int) ([]Skill, error) {
	if limit <= 0 {
		limit = 40
	}
	home, _ := os.UserHomeDir()
	codecHome := os.Getenv("CODEX_HOME")
	if codecHome == "" && home != "" {
		codecHome = filepath.Join(home, ".codex")
	}
	roots := append([]string{}, extraRoots...)
	if value := os.Getenv("MUHIYA_SKILLS_DIR"); value != "" {
		roots = append(roots, filepath.SplitList(value)...)
	}
	roots = append(roots,
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(workspace, ".codex", "skills"),
		filepath.Join(codecHome, "skills"),
		filepath.Join(home, ".agents", "skills"),
	)
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
