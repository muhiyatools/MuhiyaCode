package orchestrator

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type RelatednessDecision string

const (
	RelatedContinue  RelatednessDecision = "continue"
	RelatedNewEpoch  RelatednessDecision = "new_epoch"
	RelatedUncertain RelatednessDecision = "uncertain"
)

type RelatednessInput struct {
	Prompt, CurrentGoal string
	CurrentSettled      bool
	ExplicitReplace     bool
	KnownPaths          []string
	KnownSymbols        []string
	ChecklistIDs        []string
}

type RelatednessResult struct {
	Decision RelatednessDecision `json:"decision"`
	Score    float64             `json:"score"`
	Reasons  []string            `json:"reasons"`
}

type RelatednessHysteresis struct {
	pendingCount int
}

var (
	epochContinuationRE = regexp.MustCompile("(?i)\\b(also|continue|instead|again|same|that|those|it|fix|change|make|now|then)\\b")
	correctionRE        = regexp.MustCompile("(?i)\\b(no[,]?|actually|rather|correction)\\b")
	switchRE            = regexp.MustCompile("(?i)\\b(new task|unrelated|switch(?:ing)? to|different task|forget that|separately)\\b")
	pathTokenRE         = regexp.MustCompile("(?i)(?:[a-z]:)?[\\w.-]+(?:[/\\\\][\\w.@ -]+)+|[\\w.-]+\\.(?:go|ts|tsx|js|jsx|py|rs|java|css|html|md|json|ya?ml)")
)

func ClassifyRelatedness(input RelatednessInput) RelatednessResult {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return RelatednessResult{Decision: RelatedUncertain, Reasons: []string{"empty_prompt"}}
	}
	if switchRE.MatchString(prompt) {
		if input.CurrentSettled || input.ExplicitReplace {
			return RelatednessResult{Decision: RelatedNewEpoch, Reasons: []string{"explicit_switch"}}
		}
		return RelatednessResult{Decision: RelatedUncertain, Reasons: []string{"explicit_switch_unsettled"}}
	}
	reasons := []string{}
	score := lexicalOverlap(prompt, input.CurrentGoal) * 0.35
	if score > 0 {
		reasons = append(reasons, "lexical_overlap")
	}
	if epochContinuationRE.MatchString(prompt) {
		score += 0.30
		reasons = append(reasons, "continuation_language")
	}
	if correctionRE.MatchString(prompt) {
		score += 0.35
		reasons = append(reasons, "correction_language")
	}
	words := tokenizeTerms(prompt)
	if len(words) <= 4 {
		score += 0.30
		reasons = append(reasons, "short_continuation")
	}
	if overlap(normalizePaths(extractPaths(prompt)), normalizePaths(input.KnownPaths)) || promptContainsPath(prompt, input.KnownPaths) {
		score += 0.35
		reasons = append(reasons, "path_overlap")
	}
	if containsFoldAny(prompt, input.KnownSymbols) {
		score += 0.25
		reasons = append(reasons, "symbol_overlap")
	}
	if containsFoldAny(prompt, input.ChecklistIDs) {
		score += 0.30
		reasons = append(reasons, "checklist_overlap")
	}
	if score > 1 {
		score = 1
	}
	decision := RelatedUncertain
	if score >= 0.35 {
		decision = RelatedContinue
	} else if score <= 0.14 && input.CurrentSettled {
		decision = RelatedNewEpoch
	} else if !input.CurrentSettled {
		reasons = append(reasons, "unsettled_retain")
	}
	sort.Strings(reasons)
	return RelatednessResult{Decision: decision, Score: score, Reasons: reasons}
}

func (h *RelatednessHysteresis) Decide(input RelatednessInput) RelatednessResult {
	result := ClassifyRelatedness(input)
	if result.Decision != RelatedNewEpoch || switchRE.MatchString(input.Prompt) {
		h.pendingCount = 0
		return result
	}
	h.pendingCount++
	if h.pendingCount < 2 {
		result.Decision = RelatedUncertain
		result.Reasons = append(result.Reasons, "hysteresis_pending")
	}
	return result
}

func extractPaths(value string) []string { return pathTokenRE.FindAllString(value, -1) }

func normalizePaths(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(filepath.ToSlash(strings.TrimSpace(value)))
		if value != "" {
			out = append(out, value)
		}
	}
	return sortedUnique(out, 128)
}

func promptContainsPath(prompt string, paths []string) bool {
	prompt = strings.ToLower(filepath.ToSlash(prompt))
	for _, path := range normalizePaths(paths) {
		if strings.Contains(prompt, path) {
			return true
		}
	}
	return false
}

func tokenizeTerms(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r > 127)
	})
	return sortedUnique(parts, 256)
}

func lexicalOverlap(a, b string) float64 {
	aa, bb := tokenizeTerms(a), tokenizeTerms(b)
	if len(aa) == 0 || len(bb) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, term := range aa {
		set[term] = true
	}
	common := 0
	for _, term := range bb {
		if set[term] {
			common++
		}
	}
	return float64(common) / float64(max(len(aa), len(bb)))
}

func overlap(a, b []string) bool {
	set := map[string]bool{}
	for _, value := range a {
		set[value] = true
	}
	for _, value := range b {
		if set[value] {
			return true
		}
	}
	return false
}

func containsFoldAny(value string, needles []string) bool {
	value = strings.ToLower(value)
	for _, needle := range needles {
		if needle = strings.ToLower(strings.TrimSpace(needle)); needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func sortedUnique(values []string, limit int) []string {
	set := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortStrings(values []string) { sort.Strings(values) }
