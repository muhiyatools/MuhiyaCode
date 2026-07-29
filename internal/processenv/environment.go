// Package processenv builds minimal child-process environments. Host secrets
// are excluded unless the caller passes them explicitly.
package processenv

import (
	"sort"
	"strings"
)

var inheritedKeys = map[string]bool{
	"COLORTERM":   true,
	"COMSPEC":     true,
	"HOME":        true,
	"HOMEDRIVE":   true,
	"HOMEPATH":    true,
	"LANG":        true,
	"NO_COLOR":    true,
	"PATH":        true,
	"PATHEXT":     true,
	"SHELL":       true,
	"SYSTEMROOT":  true,
	"TEMP":        true,
	"TERM":        true,
	"TMP":         true,
	"TMPDIR":      true,
	"USERPROFILE": true,
	"WINDIR":      true,
}

// Sanitized keeps only process-runtime variables plus explicit caller grants.
// Explicit values intentionally win: MCP configuration and its encrypted
// secret store are the authority for what that child may receive.
func Sanitized(inherited []string, explicit ...map[string]string) []string {
	environment := inheritedEnvironment(inherited)
	for _, grants := range explicit {
		applyGrants(environment, grants)
	}
	return sortedEnvironment(environment)
}

func inheritedEnvironment(inherited []string) map[string]string {
	environment := make(map[string]string)
	for _, assignment := range inherited {
		key, value, ok := strings.Cut(assignment, "=")
		if ok && allowedInheritedKey(key) {
			environment[key] = value
		}
	}
	return environment
}

func applyGrants(environment, grants map[string]string) {
	for key, value := range grants {
		if strings.TrimSpace(key) != "" && !strings.Contains(key, "=") {
			environment[key] = value
		}
	}
}

func sortedEnvironment(environment map[string]string) []string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	childEnvironment := make([]string, 0, len(keys))
	for _, key := range keys {
		childEnvironment = append(childEnvironment, key+"="+environment[key])
	}
	return childEnvironment
}

func allowedInheritedKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return inheritedKeys[upper] || strings.HasPrefix(upper, "LC_")
}
