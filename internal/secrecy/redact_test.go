package secrecy

import (
	"strings"
	"testing"
)

func TestRedactRemovesExactAndPatternSecrets(t *testing.T) {
	exact := "opaque-secret-canary"
	input := "token=" + exact + " Bearer abcdefghijklmnop sk-abcdefghijklmnop github_pat_abcdefghijklmnopqrstuvwxyz"
	output := Redact(input, exact)
	for _, secret := range []string{exact, "abcdefghijklmnop", "github_pat_"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q survived redaction: %s", secret, output)
		}
	}
}
