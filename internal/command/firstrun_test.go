package command

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestConfigurationNoticeGuidesFirstRun (T050) pins the fresh-machine path: with
// no API key, the startup notice is actionable guidance (run the login flow), not
// silence and not a raw error. This is what a first-run user sees before typing.
func TestConfigurationNoticeGuidesFirstRun(t *testing.T) {
	// No key at all → point the user at the browser sign-in / login command.
	if notice := configurationNotice(contract.Settings{}, contract.Secrets{}); !strings.Contains(notice, "login") {
		t.Fatalf("no-key startup must guide to login, got: %q", notice)
	}

	// Key present but no usable model → point at model setup, still actionable.
	settings := contract.Settings{}
	settings.Provider.ActiveModelID = "missing"
	notice := configurationNotice(settings, contract.Secrets{ProviderAPIKey: "sk-present"})
	if notice == "" || !strings.Contains(strings.ToLower(notice), "model") {
		t.Fatalf("missing-model startup must guide to model setup, got: %q", notice)
	}

	// Fully configured → no notice (a clean first run stays quiet).
	ready := contract.Settings{}
	ready.Provider.ActiveModelID = "m"
	ready.Provider.Models = []contract.Model{{ID: "m", Name: "M", ContextLimit: 128000}}
	if notice := configurationNotice(ready, contract.Secrets{ProviderAPIKey: "sk-present"}); notice != "" {
		t.Fatalf("a configured setup must produce no setup notice, got: %q", notice)
	}
}
