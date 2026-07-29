package command

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestVerifyGatewayIdentity(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer minted-token" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"user":{"id":"user-1","name":"Muhiya"},
			"plan":{"name":"Dev","windows":[]},
			"credits":{"extra_total":0,"extra_remaining":0},
			"spend":{"today_usd":0}
		}`))
	}))
	defer server.Close()

	settings := contract.Settings{}
	settings.Provider.BaseURL = server.URL + "/v1"
	if err := verifyGatewayIdentity(context.Background(), settings, "minted-token", "user-1"); err != nil {
		t.Fatalf("matching identity rejected: %v", err)
	}
	err := verifyGatewayIdentity(context.Background(), settings, "minted-token", "different-user")
	if err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("mismatched identity error = %v", err)
	}
}
