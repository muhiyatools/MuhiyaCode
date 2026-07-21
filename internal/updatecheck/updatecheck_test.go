package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewerComparesNumerically is the trap a string compare falls into:
// "1.10.0" sorts BELOW "1.9.0" lexically but is the newer release.
func TestNewerComparesNumerically(t *testing.T) {
	for _, row := range []struct {
		latest, current string
		want            bool
	}{
		{"1.1.0", "1.0.6", true},
		{"1.10.0", "1.9.0", true},
		{"2.0.0", "1.99.99", true},
		{"1.0.7", "1.0.6", true},
		{"1.0.6", "1.0.6", false},
		{"1.0.5", "1.0.6", false},
		{"v1.1.0", "1.0.6", true},
		{"1.1.0-beta.1", "1.0.6", true},
		// Anything unparseable must stay silent rather than nag about an
		// update that may not exist.
		{"", "1.0.6", false},
		{"latest", "1.0.6", false},
		{"1.0", "1.0.6", false},
		{"1.0.6", "", false},
		{"1.0.x", "1.0.6", false},
	} {
		if got := Newer(row.latest, row.current); got != row.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", row.latest, row.current, got, row.want)
		}
	}
}

func TestStateFreshness(t *testing.T) {
	now := time.Now()
	if (State{}).Fresh(now) {
		t.Error("a never-checked state must not be fresh")
	}
	if !(State{CheckedAt: now.Add(-time.Hour)}).Fresh(now) {
		t.Error("an hour-old check is still within the TTL")
	}
	if (State{CheckedAt: now.Add(-TTL - time.Minute)}).Fresh(now) {
		t.Error("a check older than the TTL must be stale")
	}
}

// TestFetchParsesRegistryPayload exercises the decode path against a fake
// registry, including the failure modes that must return an error rather than
// a bogus version.
func TestFetchParsesRegistryPayload(t *testing.T) {
	t.Setenv(DisableEnv, "")
	for _, row := range []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{name: "ok", status: 200, body: `{"version":"1.2.3","name":"muhiyacode"}`, want: "1.2.3"},
		{name: "not found", status: 404, body: `{}`, wantErr: true},
		{name: "malformed", status: 200, body: `not json`, wantErr: true},
		{name: "missing version", status: 200, body: `{"name":"muhiyacode"}`, wantErr: true},
	} {
		t.Run(row.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(row.status)
				_, _ = w.Write([]byte(row.body))
			}))
			defer server.Close()
			got, err := fetchFrom(context.Background(), server.URL)
			if row.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got version %q", got)
				}
				return
			}
			if err != nil || got != row.want {
				t.Fatalf("version=%q err=%v, want %q", got, err, row.want)
			}
		})
	}
}

// TestFetchRespectsOptOut: the env var must short-circuit before any network
// call, so an air-gapped or CI machine never reaches out.
func TestFetchRespectsOptOut(t *testing.T) {
	t.Setenv(DisableEnv, "1")
	if !Disabled() {
		t.Fatal("opt-out env var not honored")
	}
	if _, err := Fetch(context.Background()); err == nil {
		t.Fatal("Fetch made a call despite the opt-out")
	}
}
