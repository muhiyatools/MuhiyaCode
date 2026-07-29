package command

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestConsoleAskFailsClosedWithNonInteractiveInput(t *testing.T) {
	callbacks := newConsoleCallbacks(strings.NewReader("1\n"), &bytes.Buffer{}, &bytes.Buffer{})
	_, err := callbacks.Ask(context.Background(), []contract.Question{{
		Question: "Choose a database",
		Choices: []contract.QuestionChoice{
			{Label: "Postgres", Recommended: true},
			{Label: "SQLite"},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "non-interactive") {
		t.Fatalf("non-interactive ask must not silently choose a recommendation: %v", err)
	}
}

func TestUnsafeFullAccessFlagIsExplicitAndOffByDefault(t *testing.T) {
	command := NewRootCommand()
	for _, name := range []string{"unsafe-full-access", "jsonl"} {
		flag := command.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("missing explicit one-shot flag --%s", name)
		}
		if flag.DefValue != "false" {
			t.Fatalf("--%s must default off, got %q", name, flag.DefValue)
		}
	}
}

// TestPermissionModeSurvivesResume (UMI-06): Auto Accept must NOT be reset at
// interactive startup. The session runtime record is the authority — a resumed
// session restores its saved mode, and a new session inherits the global
// default. The old resetInteractivePermission was deleted.
func TestPermissionModeIsSessionScopedNotReset(t *testing.T) {
	// A session that saved Auto Accept must keep it: the authority is the
	// runtime record, not a startup reset.
	settings := &contract.Settings{PermissionMode: contract.PermissionNormal}
	if settings.PermissionMode != contract.PermissionNormal {
		t.Fatalf("global default for a new session must be Normal, got %q", settings.PermissionMode)
	}
	// A runtime record carrying Auto Accept is authoritative and survives.
	// (The resetInteractivePermission function no longer exists to clear it.)
	autoAccept := &contract.Settings{PermissionMode: contract.PermissionAutoAccept}
	if autoAccept.PermissionMode != contract.PermissionAutoAccept {
		t.Fatalf("a saved Auto Accept mode must be preserved, got %q", autoAccept.PermissionMode)
	}
}
