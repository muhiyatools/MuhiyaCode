package secrecy

import (
	"regexp"
	"strings"
)

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]{12,}`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)\s*[:=]\s*[^\s,}]+`),
	regexp.MustCompile(`(?i)(AKIA|ASIA)[A-Z0-9]{16}`),
	regexp.MustCompile(`(?i)github_pat_[a-zA-Z0-9_]{22,}`),
	regexp.MustCompile(`(?i)gh[po]_[a-zA-Z0-9_]{36}`),
	regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
}

// Redact removes configured exact secrets and common credential formats.
// Values shorter than eight characters are ignored to avoid destroying normal
// prose when an empty or placeholder setting is supplied.
func Redact(input string, values ...string) string {
	output := input
	for _, value := range values {
		if len(value) >= 8 {
			output = strings.ReplaceAll(output, value, "[REDACTED_SECRET]")
		}
	}
	for _, pattern := range patterns {
		output = pattern.ReplaceAllString(output, "[REDACTED]")
	}
	return output
}
