package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A signed-out first run must present the onboarding modal with EXACTLY one
// option, "Log in with Muhiya Account".
func TestOnboardingOpensWithSingleOptionWhenSignedOut(t *testing.T) {
	acts := Actions{
		IsLoggedIn:      func() bool { return false },
		LoginViaBrowser: func(context.Context) (string, error) { return "user@example.com", nil },
	}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})

	m.openOnboarding()
	if m.modal == nil {
		t.Fatal("onboarding modal did not open when signed out")
	}
	if m.modal.title != "Set up MuhiyaCode" {
		t.Fatalf("unexpected onboarding title %q", m.modal.title)
	}
	if len(m.modal.choices) != 1 || m.modal.choices[0].Label != "Log in with Muhiya Account" {
		t.Fatalf("onboarding must offer exactly one option, got %+v", m.modal.choices)
	}
}

func TestOnboardingSkippedWhenSignedIn(t *testing.T) {
	acts := Actions{IsLoggedIn: func() bool { return true }}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})
	m.openOnboarding()
	if m.modal != nil {
		t.Fatalf("a signed-in user must never see onboarding: %+v", m.modal)
	}
}

// Selecting the single option must dispatch the same browser-login action the
// `/login` command uses, yielding actionMsg{kind:"login"}.
func TestOnboardingSelectionStartsLogin(t *testing.T) {
	acts := Actions{
		IsLoggedIn:      func() bool { return false },
		LoginViaBrowser: func(context.Context) (string, error) { return "user@example.com", nil },
	}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})
	m.openOnboarding()

	cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting the login option produced no command")
	}
	msg := cmd()
	am, ok := msg.(actionMsg)
	if !ok || am.kind != "login" {
		t.Fatalf("expected actionMsg{kind:login}, got %#v", msg)
	}
	if am.err != nil || am.value != "user@example.com" {
		t.Fatalf("login action carried unexpected result: value=%v err=%v", am.value, am.err)
	}
}

// Esc dismisses the modal (the escape hatch that keeps `/login <key>` usable) and
// leaves the app fully usable.
func TestOnboardingEscDismisses(t *testing.T) {
	acts := Actions{IsLoggedIn: func() bool { return false }, LoginViaBrowser: func(context.Context) (string, error) { return "", nil }}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})
	m.openOnboarding()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.modal != nil {
		t.Fatalf("Esc must dismiss onboarding, still open: %+v", m.modal)
	}
}

// Onboarding must not clobber (or be dropped by) an in-flight reply modal such as
// the workspace-trust prompt: it defers and surfaces once that modal is answered.
func TestOnboardingDefersBehindReplyModal(t *testing.T) {
	acts := Actions{
		IsLoggedIn:      func() bool { return false },
		LoginViaBrowser: func(context.Context) (string, error) { return "", nil },
	}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})

	reply := make(chan int, 1)
	m.enqueueModal(modalRequest{title: "Trust workspace", message: "trust?", choices: twoChoices(), reply: reply})

	m.openOnboarding()
	if !m.onboardingPending {
		t.Fatal("onboarding should defer behind a reply modal")
	}
	if m.modal == nil || m.modal.reply == nil {
		t.Fatalf("the reply modal must remain active while onboarding defers: %+v", m.modal)
	}

	// Answer the trust prompt; onboarding must now surface and the flag clear.
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	<-reply
	if m.modal == nil || m.modal.title != "Set up MuhiyaCode" {
		t.Fatalf("onboarding did not surface after the reply modal closed: %+v", m.modal)
	}
	if m.onboardingPending {
		t.Fatal("onboardingPending must clear once the modal opens")
	}
}

// Signing in retires a pending onboarding modal; logging out re-presents it.
func TestLoginClearsPendingAndLogoutReopens(t *testing.T) {
	signedIn := false
	acts := Actions{IsLoggedIn: func() bool { return signedIn }}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})

	m.onboardingPending = true
	m.handleAction(actionMsg{kind: "login", value: "user@example.com"})
	if m.onboardingPending {
		t.Fatal("a successful login must clear the pending onboarding flag")
	}

	m.handleAction(actionMsg{kind: "logout"})
	if m.modal == nil || m.modal.title != "Set up MuhiyaCode" {
		t.Fatalf("logout must re-present onboarding: %+v", m.modal)
	}
}

// A prompt typed during loading while signed out must be preserved in the
// composer (not fired against a missing key) and the onboarding modal shown.
func TestHydrationHoldsPendingPromptWhenSignedOut(t *testing.T) {
	acts := Actions{IsLoggedIn: func() bool { return false }, LoginViaBrowser: func(context.Context) (string, error) { return "", nil }}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})
	m.loading = true
	m.pendingSubmit = "draft prompt"

	m.applyHydration(hydratedMsg{result: HydratedRuntime{Runtime: testRuntime(t)}})

	if m.pendingSubmit != "" {
		t.Fatal("pendingSubmit should be consumed")
	}
	if got := m.input.Value(); got != "draft prompt" {
		t.Fatalf("pending prompt not preserved in composer, got %q", got)
	}
	if m.modal == nil || m.modal.title != "Set up MuhiyaCode" {
		t.Fatalf("onboarding modal should be open after signed-out hydration: %+v", m.modal)
	}
}
