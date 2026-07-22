package evidence

import (
	"regexp"
	"strings"
)

type ProcessReductionInput struct {
	Adapter, Status, Artifact, Text string
	ExitCode                        int
	Complete, TimedOut, Cancelled   bool
	Facts                           []string
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func ReduceProcess(input ProcessReductionInput) ObservationCard {
	text := strings.ToValidUTF8(ansiRE.ReplaceAllString(input.Text, ""), "�")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	protected, ordinary := make([]string, 0, 20), make([]string, 0, 80)
	errorRE := regexp.MustCompile(`(?i)(FAIL|ERROR|ERR!|panic:|fatal:|warning:|\bexpected\b|\bactual\b|:[0-9]+:[0-9]+)`)
	for _, line := range lines {
		if errorRE.MatchString(line) {
			protected = append(protected, line)
		} else if strings.TrimSpace(line) != "" && len(ordinary) < 40 {
			ordinary = append(ordinary, line)
		}
	}
	card := ObservationCard{Status: input.Status, Complete: input.Complete, Artifact: input.Artifact, Summary: input.Adapter + " exit=" + itoa(input.ExitCode), MaxTokens: 1_500, Facts: append([]string(nil), input.Facts...)}
	if input.ExitCode != 0 || input.TimedOut || input.Cancelled {
		card.MaxTokens = 3_000
	}
	if input.TimedOut {
		card.Facts = append(card.Facts, "timed_out=true (timed out)")
	}
	if input.Cancelled {
		card.Facts = append(card.Facts, "cancelled=true")
	}
	if len(protected) > 0 {
		card.Excerpts = append(card.Excerpts, Excerpt{Source: input.Adapter, StartLine: 1, EndLine: len(protected), Text: strings.Join(protected, "\n"), Protected: true})
	}
	if len(ordinary) > 0 {
		card.Excerpts = append(card.Excerpts, Excerpt{Source: input.Adapter, StartLine: 1, EndLine: len(ordinary), Text: strings.Join(ordinary, "\n")})
	}
	card.OmittedLines = max(0, len(lines)-len(protected)-len(ordinary))
	return card
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [24]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
