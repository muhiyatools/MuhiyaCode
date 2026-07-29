package processenv

import (
	"slices"
	"testing"
)

func TestSanitizedExcludesSecretCanariesAndPreservesRuntime(t *testing.T) {
	environment := Sanitized([]string{
		"PATH=/bin",
		"HOME=/home/test",
		"OPENAI_API_KEY=host-canary",
		"AWS_SECRET_ACCESS_KEY=cloud-canary",
		"LC_ALL=C",
	})
	if slices.Contains(environment, "OPENAI_API_KEY=host-canary") ||
		slices.Contains(environment, "AWS_SECRET_ACCESS_KEY=cloud-canary") {
		t.Fatalf("host secret leaked into child environment: %v", environment)
	}
	for _, expected := range []string{"HOME=/home/test", "LC_ALL=C", "PATH=/bin"} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("runtime variable %q missing from %v", expected, environment)
		}
	}
}

func TestSanitizedPassesOnlyExplicitSecrets(t *testing.T) {
	environment := Sanitized(
		[]string{"PATH=/bin", "TOKEN=host-secret"},
		map[string]string{"TOKEN": "granted-secret", "SERVER_MODE": "stdio"},
	)
	if !slices.Contains(environment, "TOKEN=granted-secret") ||
		!slices.Contains(environment, "SERVER_MODE=stdio") {
		t.Fatalf("explicit grant missing from %v", environment)
	}
	if slices.Contains(environment, "TOKEN=host-secret") {
		t.Fatalf("host value overrode explicit grant: %v", environment)
	}
}
