package workspace

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellDoesNotInheritHostSecretCanary(t *testing.T) {
	t.Setenv("MUHIYA_HOST_SECRET_CANARY", "must-not-reach-child")
	command := `printf '%s' "$MUHIYA_HOST_SECRET_CANARY"`
	if runtime.GOOS == "windows" {
		command = `Write-Output $env:MUHIYA_HOST_SECRET_CANARY`
	}
	runner := &ShellRunner{Timeout: 10 * time.Second, OutputLimit: 1024}
	result, err := runner.Run(context.Background(), t.TempDir(), command, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Output, "must-not-reach-child") {
		t.Fatalf("host secret reached shell child: %q", result.Output)
	}
}
