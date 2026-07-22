package evidence

import "strings"

type FileReductionInput struct {
	Kind, Source, Status, Artifact string
	Complete                       bool
	Text                           string
	StartLine, MaxLines            int
	Skipped                        []string
	Facts                          []string
}

func ReduceFile(input FileReductionInput) ObservationCard {
	lines := strings.Split(strings.ReplaceAll(input.Text, "\r\n", "\n"), "\n")
	maxLines := input.MaxLines
	if maxLines <= 0 {
		maxLines = 120
	}
	shown := min(len(lines), maxLines)
	start := max(1, input.StartLine)
	card := ObservationCard{Status: input.Status, Complete: input.Complete, Artifact: input.Artifact, Summary: input.Kind + " result", Skipped: input.Skipped, Facts: append([]string(nil), input.Facts...)}
	if shown > 0 && !(len(lines) == 1 && lines[0] == "") {
		card.Excerpts = []Excerpt{{Source: input.Source, StartLine: start, EndLine: start + shown - 1, Text: strings.Join(lines[:shown], "\n")}}
	}
	card.OmittedLines = max(0, len(lines)-shown)
	if !input.Complete && strings.TrimSpace(input.Text) == "" {
		card.Facts = append(card.Facts, "negative result is incomplete; absence is not proven")
	}
	return card
}
