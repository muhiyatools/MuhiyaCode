package evidence

import (
	"regexp"
	"sort"
	"strings"
)

type DiffReductionInput struct {
	Status, Artifact, Text, BeforeFingerprint, AfterFingerprint string
	Complete                                                    bool
	BeforeMode, AfterMode                                       string
	Applied, Partial, Mismatch, Idempotent                      bool
	Facts                                                       []string
}

func ReduceDiff(input DiffReductionInput) ObservationCard {
	paths := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\+\+\+\s+(?:b/)?(.+)$`).FindAllStringSubmatch(input.Text, -1) {
		if len(match) > 1 && match[1] != "/dev/null" {
			paths[match[1]] = true
		}
	}
	changed := make([]string, 0, len(paths))
	for path := range paths {
		changed = append(changed, path)
	}
	sort.Strings(changed)
	facts := append([]string{"changed_paths=" + strings.Join(changed, ",")}, input.Facts...)
	facts = append(facts, "applied="+boolString(input.Applied), "partial="+boolString(input.Partial), "mismatch="+boolString(input.Mismatch), "idempotent="+boolString(input.Idempotent))
	if input.BeforeFingerprint != "" || input.AfterFingerprint != "" {
		facts = append(facts, "fingerprint="+input.BeforeFingerprint+"->"+input.AfterFingerprint)
	}
	if input.BeforeMode != "" || input.AfterMode != "" {
		facts = append(facts, "mode="+input.BeforeMode+"->"+input.AfterMode)
	}
	card := ObservationCard{Status: input.Status, Complete: input.Complete, Artifact: input.Artifact, Summary: "diff result", Facts: facts}
	card.Excerpts = []Excerpt{{Source: "diff", StartLine: 1, EndLine: min(80, strings.Count(input.Text, "\n")+1), Text: strings.Join(strings.Split(input.Text, "\n")[:min(80, strings.Count(input.Text, "\n")+1)], "\n")}}
	card.OmittedLines = max(0, strings.Count(input.Text, "\n")+1-80)
	return card
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
