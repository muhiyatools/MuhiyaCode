package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// runChecks greps the post-run workspace for every workload check. A missing
// target path fails a contains check and passes an absent check.
func runChecks(workspace string, checks []workloadCheck) (map[string]bool, int) {
	results := map[string]bool{}
	failures := 0
	for _, check := range checks {
		found := treeContains(filepath.Join(workspace, filepath.FromSlash(check.Path)), check.Needle)
		passed := found
		if check.Kind == "absent" {
			passed = !found
		}
		results[fmt.Sprintf("%s %s %q", check.Path, check.Kind, check.Needle)] = passed
		if !passed {
			failures++
		}
	}
	return results, failures
}

// treeContains reports whether any regular file at/under target contains
// needle, case-insensitively.
func treeContains(target, needle string) bool {
	lowered := strings.ToLower(needle)
	info, err := os.Stat(target)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return fileContains(target, lowered)
	}
	found := false
	_ = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || found {
			return walkErr
		}
		if fileContains(path, lowered) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func fileContains(path, loweredNeedle string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), loweredNeedle)
}

var (
	workloadLine   = regexp.MustCompile(`^\s*\d+\.\s+(.+?)\s*$`)
	workloadExpect = regexp.MustCompile(`^\s*expect:\s*(.+?)\s*$`)
	workloadCheckL = regexp.MustCompile(`^\s*check:\s*(.+?)\s*::\s*(contains|absent)\s*::\s*(.+?)\s*$`)
)

// loadWorkload parses a workload markdown file: exactly ONE numbered prompt
// line, an optional expect: line, and any number of check: lines. Only the
// prompt is ever sent to the model.
func loadWorkload(path string) (workloadSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return workloadSpec{}, err
	}
	defer file.Close()
	var spec workloadSpec
	prompts := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if match := workloadCheckL.FindStringSubmatch(line); len(match) == 4 {
			spec.Checks = append(spec.Checks, workloadCheck{Path: match[1], Kind: match[2], Needle: match[3]})
			continue
		}
		if match := workloadExpect.FindStringSubmatch(line); len(match) == 2 {
			spec.Expect = match[1]
			continue
		}
		if match := workloadLine.FindStringSubmatch(line); len(match) == 2 {
			prompts++
			spec.Prompt = match[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return workloadSpec{}, err
	}
	if prompts != 1 {
		return workloadSpec{}, fmt.Errorf("workload %s must contain exactly one numbered prompt, got %d", path, prompts)
	}
	if len(spec.Checks) == 0 {
		return workloadSpec{}, fmt.Errorf("workload %s has no check: lines", path)
	}
	return spec, nil
}

func printSummary(result runResult, outPath string) {
	finalPrompt := "unavailable"
	if result.FinalMainPromptTokens != nil {
		finalPrompt = fmt.Sprintf("%d", *result.FinalMainPromptTokens)
	}
	steady := "unavailable"
	if result.SteadyStateHitRate != nil {
		steady = fmt.Sprintf("%.2f%%", *result.SteadyStateHitRate*100)
	}
	billed := fmt.Sprintf("%d", result.BilledTokens)
	if result.BilledTokensEstimated {
		billed = "~" + billed
	}
	fmt.Printf("%s %s: agentRuns=%d(+%d reused) turns=%d toolCalls=%d (main=%d sub=%d) finalMainPrompt=%s billed=%s steady=%s filesChanged=%d overwrites=%d/%d dupReads=%d checksFailed=%d/%d -> %s\n",
		result.Build, result.Workload, result.AgentRuns, result.AgentRunsReused, result.Turns,
		result.ToolCalls, result.MainToolCalls, result.SubagentToolCalls, finalPrompt, billed, steady,
		len(result.FilesChanged), result.WriteFileAudit.OverwritesOfExistingFiles, result.WriteFileAudit.TotalCalls,
		result.DuplicateReads.Total, result.ChecklistFailures, len(result.ChecklistResults), outPath)
}

func validatePinnedModel(settings contract.Settings) error {
	id := strings.TrimSpace(settings.Provider.ActiveModelID)
	if id == "" {
		return errors.New("active model is empty")
	}
	lower := strings.ToLower(id)
	if strings.Contains(lower, "router") || strings.Contains(lower, "auto") {
		return fmt.Errorf("model %q is not a pinned concrete model", id)
	}
	for _, model := range settings.Provider.Models {
		if model.ID == id {
			if model.ContextLimit <= 0 {
				return fmt.Errorf("model %q has no context limit", id)
			}
			return nil
		}
	}
	return fmt.Errorf("model %q is not in configured models", id)
}

func currentGitCommit() string {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}
