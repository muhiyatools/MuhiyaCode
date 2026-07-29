package command

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/state"
)

func TestSetAPIKeyValidatesBeforeReplacingCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer valid-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"user":{"id":"user-1"}}`))
	}))
	defer server.Close()

	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	settings := state.DefaultSettings()
	settings.Provider.BaseURL = server.URL + "/v1"
	settings.Provider.Models = []contract.Model{{ID: "test", ContextLimit: 1000}}
	oldSecrets := contract.Secrets{ProviderAPIKey: "existing-key"}
	if err := state.SaveSecrets(oldSecrets, paths); err != nil {
		t.Fatal(err)
	}
	app := &Application{
		paths: paths, settings: &settings, secrets: oldSecrets,
		sessions: &state.Sessions{Secrets: oldSecrets},
		provider: gateway.NewOpenAICompatible(gateway.Config{Settings: settings, APIKey: oldSecrets.ProviderAPIKey}),
	}

	if err := app.setAPIKey(context.Background(), "vk-record-id"); err == nil {
		t.Fatal("gateway record ID was accepted as a bearer token")
	}
	assertStoredAPIKey(t, paths, "existing-key")

	if err := app.setAPIKey(context.Background(), "invalid-key"); err == nil {
		t.Fatal("invalid key was accepted")
	}
	assertStoredAPIKey(t, paths, "existing-key")
	if app.secrets.ProviderAPIKey != "existing-key" {
		t.Fatal("invalid key replaced the in-memory credential")
	}

	if err := app.setAPIKey(context.Background(), "valid-key"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	assertStoredAPIKey(t, paths, "valid-key")
}

func assertStoredAPIKey(t *testing.T, paths state.Paths, expected string) {
	t.Helper()
	secrets, err := state.LoadSecrets(paths)
	if err != nil {
		t.Fatal(err)
	}
	if secrets.ProviderAPIKey != expected {
		t.Fatalf("stored key = %q, want %q", secrets.ProviderAPIKey, expected)
	}
}
