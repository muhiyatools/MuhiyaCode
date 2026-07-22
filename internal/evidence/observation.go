package evidence

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type Excerpt struct {
	Source    string `json:"source"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Text      string `json:"text"`
	Protected bool   `json:"protected,omitempty"`
}

type ObservationCard struct {
	Status       string    `json:"status"`
	Complete     bool      `json:"complete"`
	Artifact     string    `json:"artifact,omitempty"`
	Summary      string    `json:"summary"`
	Facts        []string  `json:"facts,omitempty"`
	Excerpts     []Excerpt `json:"excerpts,omitempty"`
	OmittedLines int       `json:"omitted_lines,omitempty"`
	Skipped      []string  `json:"skipped,omitempty"`
	Degraded     bool      `json:"degraded,omitempty"`
	MaxTokens    int       `json:"-"`
}

func (card ObservationCard) Render() string {
	maxTokens := card.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1_500
	}
	limit := maxTokens * 4
	var lines []string
	lines = append(lines, fmt.Sprintf("status=%s complete=%t", card.Status, card.Complete))
	if card.Artifact != "" {
		lines = append(lines, "artifact="+card.Artifact)
	} else if card.Degraded {
		lines = append(lines, "artifact=unavailable (degraded; raw result was not stored)")
	}
	if summary := strings.TrimSpace(card.Summary); summary != "" {
		lines = append(lines, "summary="+summary)
	}
	facts := append([]string(nil), card.Facts...)
	sort.Strings(facts)
	for _, fact := range facts {
		lines = append(lines, "fact="+fact)
	}
	excerpts := append([]Excerpt(nil), card.Excerpts...)
	sort.SliceStable(excerpts, func(i, j int) bool {
		if excerpts[i].Source == excerpts[j].Source {
			return excerpts[i].StartLine < excerpts[j].StartLine
		}
		return excerpts[i].Source < excerpts[j].Source
	})
	for _, excerpt := range excerpts {
		prefix := fmt.Sprintf("excerpt=%s:%d-%d ", excerpt.Source, excerpt.StartLine, excerpt.EndLine)
		candidate := prefix + excerpt.Text
		if excerpt.Protected || len(strings.Join(append(lines, candidate), "\n")) <= limit {
			lines = append(lines, candidate)
		} else {
			card.OmittedLines += max(1, excerpt.EndLine-excerpt.StartLine+1)
		}
	}
	if card.OmittedLines > 0 {
		lines = append(lines, fmt.Sprintf("omitted_lines=%d", card.OmittedLines))
	}
	if len(card.Skipped) > 0 {
		skipped := append([]string(nil), card.Skipped...)
		sort.Strings(skipped)
		lines = append(lines, "skipped="+strings.Join(skipped, ","))
	}
	result := strings.Join(lines, "\n")
	if len(result) <= limit {
		return result
	}
	return truncateUTF8(result, limit)
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimRight(value, "\r\n ") + "…"
}
