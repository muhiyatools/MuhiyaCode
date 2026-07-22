package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Upstream affinity. A model slug on a routing layer (OpenRouter) is served by
// several upstream providers, each holding its OWN prompt cache. Once a session
// has warmed one, every later request must ask to go back to it — otherwise a
// silent re-route re-bills the whole conversation as uncached, with
// byte-identical input and no trace anywhere on our side.

func captureBody(t *testing.T, request contract.ChatRequest) map[string]any {
	t.Helper()
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	provider := NewOpenAICompatible(Config{Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 1, IdleTimeout: time.Second})
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("outbound body is not JSON: %v\n%s", err, captured)
	}
	return body
}

func TestUpstreamPinIsSentAsAPreferenceNotARestriction(t *testing.T) {
	body := captureBody(t, contract.ChatRequest{
		Messages:    []contract.Message{{Role: contract.RoleUser, Content: "hi"}},
		PinUpstream: "minimax",
	})

	routing, ok := body["provider"].(map[string]any)
	if !ok {
		t.Fatalf("no provider routing field on the outbound body: %v", body)
	}
	order, ok := routing["order"].([]any)
	if !ok || len(order) == 0 || order[0] != "minimax" {
		t.Fatalf("provider.order = %v, want the pinned upstream slug first", routing["order"])
	}
	// The whole reliability argument rests on this one value. "only", or
	// allow_fallbacks=false, converts an upstream outage into a failed task —
	// paying a total loss to avoid one cold prefix.
	if allow, ok := routing["allow_fallbacks"].(bool); !ok || !allow {
		t.Fatalf("allow_fallbacks = %v, want true: a pin must never be able to fail a task", routing["allow_fallbacks"])
	}
	if _, restricted := routing["only"]; restricted {
		t.Fatal("provider.only is a hard restriction and must never be sent")
	}
}

// OpenRouter's order array accepts lowercase SLUGS, its response field reports
// display names, and an unmatched entry is skipped SILENTLY — so a verbatim
// display name pins nothing and reports no error. Every plausible slug
// spelling must therefore be sent; wrong guesses cost nothing.
func TestUpstreamOrderCoversTheRealSlugSchemes(t *testing.T) {
	for _, row := range []struct {
		reported string
		want     []string // must all be present, first entry must be want[0]
	}{
		// The nine upstreams actually serving minimax-m3 today collapse to
		// three spelling schemes: plain lowercase (Novita→novita,
		// DeepInfra→deepinfra), camel-hyphen (AtlasCloud→atlas-cloud), and
		// acronym-run (GMICloud→gmicloud, where hyphenation must NOT fire).
		{"Novita", []string{"novita", "Novita"}},
		{"DeepInfra", []string{"deepinfra", "deep-infra", "DeepInfra"}},
		{"AtlasCloud", []string{"atlascloud", "atlas-cloud", "AtlasCloud"}},
		{"GMICloud", []string{"gmicloud", "GMICloud"}},
		// A response that is already a variant slug keeps the exact variant as
		// a candidate while leading with the base provider.
		{"novita/fp8", []string{"novita", "novita/fp8"}},
	} {
		got := upstreamOrderCandidates(row.reported)
		if got[0] != row.want[0] {
			t.Fatalf("%q: first candidate = %q, want the base slug %q (order is tried first-to-last)", row.reported, got[0], row.want[0])
		}
		present := make(map[string]bool, len(got))
		for _, candidate := range got {
			present[candidate] = true
		}
		for _, want := range row.want {
			if !present[want] {
				t.Fatalf("%q: candidates %v missing %q", row.reported, got, want)
			}
		}
		if len(got) != len(present) {
			t.Fatalf("%q: candidates contain duplicates: %v", row.reported, got)
		}
	}
}

// No pin is the correct state for a session's first request and for every
// direct provider connection. Sending an empty routing object would be a
// meaningless field at best and a rejected request at worst.
func TestNoUpstreamPinSendsNoRoutingField(t *testing.T) {
	body := captureBody(t, contract.ChatRequest{
		Messages: []contract.Message{{Role: contract.RoleUser, Content: "hi"}},
	})
	if _, present := body["provider"]; present {
		t.Fatalf("an unpinned request must not carry a provider field: %v", body["provider"])
	}
}

// The pin travels in the body, never in the prompt. If it reached the messages
// or the tool block it would change the cached prefix on the very turn it
// exists to protect.
func TestUpstreamPinDoesNotTouchThePrompt(t *testing.T) {
	unpinned := captureBody(t, contract.ChatRequest{
		Messages: []contract.Message{{Role: contract.RoleSystem, Content: "system"}, {Role: contract.RoleUser, Content: "hi"}},
	})
	pinned := captureBody(t, contract.ChatRequest{
		Messages:    []contract.Message{{Role: contract.RoleSystem, Content: "system"}, {Role: contract.RoleUser, Content: "hi"}},
		PinUpstream: "minimax",
	})
	before, err := json.Marshal(unpinned["messages"])
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(pinned["messages"])
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("pinning changed the messages:\n%s\n%s", before, after)
	}
}

// The pin is a pure optimization, so it must be incapable of failing a task.
// A route that refuses the routing field outright (a 400 is TERMINAL in the
// retry loop) would otherwise turn every request of every session into a hard
// failure — a total loss traded for a caching nicety.
func TestARefusedPinIsDroppedAndNeverFailsTheTask(t *testing.T) {
	var pinned []bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		_, hasProvider := decoded["provider"]
		pinned = append(pinned, hasProvider)
		if hasProvider {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unrecognized field: provider"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompatible(Config{Settings: testSettings(server.URL), APIKey: "sk-test", MaxRetries: 3, IdleTimeout: time.Second})
	request := contract.ChatRequest{
		Messages:    []contract.Message{{Role: contract.RoleUser, Content: "hi"}},
		PinUpstream: "minimax",
	}
	response, err := provider.Chat(context.Background(), request)
	if err != nil {
		t.Fatalf("a refused pin killed the task: %v", err)
	}
	if response.Content != "ok" {
		t.Fatalf("content = %q, want the retried answer", response.Content)
	}
	if len(pinned) != 2 || !pinned[0] || pinned[1] {
		t.Fatalf("expected one pinned attempt then one unpinned retry, got %v", pinned)
	}

	// And it must stay off: re-learning the lesson on every request would spend
	// a wasted round trip per turn for the life of the process.
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 3 || pinned[2] {
		t.Fatalf("the rejection did not latch — a later request pinned again: %v", pinned)
	}
}

func TestPinFallbackRequiresExplicitAffinityFieldRejection(t *testing.T) {
	for _, test := range []struct {
		name string
		err  *HTTPError
		want bool
	}{
		{"field rejected", &HTTPError{Status: 400, Body: `{"error":"unknown field provider"}`}, true},
		{"authentication", &HTTPError{Status: 401, Body: `{"error":"provider credentials invalid"}`}, false},
		{"unknown model", &HTTPError{Status: 400, Body: `{"error":"unknown model"}`}, false},
		{"quota", &HTTPError{Status: 422, Body: `{"error":"provider quota exhausted"}`}, false},
		{"server failure", &HTTPError{Status: 500, Body: `{"error":"unsupported provider"}`}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := rejectsUpstreamAffinity(test.err); got != test.want {
				t.Fatalf("rejectsUpstreamAffinity(%+v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

// A config reload (the user changes model or re-authenticates mid-session) must
// not re-enable a pin the route has already refused. UpdateConfig replaces
// settings and credentials only — this test exists so a future refactor that
// swaps the whole Config, or rebuilds the provider struct, cannot silently
// resurrect a known-bad request shape and start failing tasks again.
func TestARejectedPinSurvivesAConfigReload(t *testing.T) {
	var attempts []bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		_, hasProvider := decoded["provider"]
		attempts = append(attempts, hasProvider)
		if hasProvider {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unrecognized field: provider"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	provider := NewOpenAICompatible(Config{Settings: settings, APIKey: "sk-test", MaxRetries: 3, IdleTimeout: time.Second})
	request := contract.ChatRequest{
		Messages:    []contract.Message{{Role: contract.RoleUser, Content: "hi"}},
		PinUpstream: "minimax",
	}
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	provider.UpdateConfig(settings, "sk-rotated")

	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatalf("a reload re-armed the refused pin and failed the task: %v", err)
	}
	for index, wasPinned := range attempts[1:] {
		if wasPinned {
			t.Fatalf("attempt %d pinned again after the rejection latched: %v", index+1, attempts)
		}
	}
}

// The upstream that served a request must come back to the caller, or the
// session can never learn what to pin to.
func TestUpstreamIsReadFromTheStream(t *testing.T) {
	accumulator := NewStreamAccumulatorForProfile(ResolveModelProfile("minimax-m3"), nil, nil)
	for _, line := range []string{
		`data: {"provider":"minimax","choices":[{"delta":{"content":"a"}}]}`,
		`data: {"provider":"minimax","choices":[{"delta":{"content":"b"}}]}`,
		"data: [DONE]",
	} {
		if err := accumulator.ConsumeLine(line); err != nil {
			t.Fatal(err)
		}
	}
	if got := accumulator.Result().Upstream; got != "minimax" {
		t.Fatalf("upstream = %q, want minimax", got)
	}
}

// A direct provider connection sends no such field, and inventing one would
// pin a session to a name that means nothing.
func TestAbsentUpstreamStaysEmpty(t *testing.T) {
	accumulator := NewStreamAccumulatorForProfile(ResolveModelProfile("deepseek-v4-pro"), nil, nil)
	if err := accumulator.ConsumeLine(`data: {"choices":[{"delta":{"content":"a"}}]}`); err != nil {
		t.Fatal(err)
	}
	if got := accumulator.Result().Upstream; got != "" {
		t.Fatalf("upstream = %q, want empty for a direct connection", got)
	}
}
