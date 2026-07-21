package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// A routing layer (OpenRouter) can serve two consecutive requests from two
// different upstream providers. Prefix caches are per-upstream, so the second
// one starts cold — while every byte we sent says it should have been warm.
// That is the one cause of a cache miss with no evidence anywhere on our side,
// so the harness has to say it out loud and stop pricing switches against a
// cache that no longer exists.

// upstreamProvider records the pin each request carried and answers with a
// fixed upstream, the way a routing layer names who actually served it.
type upstreamProvider struct {
	upstream  string
	responses []contract.ChatResponse
	pins      []string
}

func (p *upstreamProvider) Chat(_ context.Context, request contract.ChatRequest) (contract.ChatResponse, error) {
	p.pins = append(p.pins, request.PinUpstream)
	response := contract.ChatResponse{Content: "done"}
	if len(p.responses) > 0 {
		response = p.responses[0]
		p.responses = p.responses[1:]
	}
	response.Usage = contract.Usage{PromptTokens: 1_000, CompletionTokens: 10, PromptTokensAvailable: true, Upstream: p.upstream}
	return response, nil
}

func (p *upstreamProvider) StableRequestMessages(request contract.ChatRequest) ([]contract.Message, error) {
	return append([]contract.Message(nil), request.Messages...), nil
}

func (p *upstreamProvider) ListModels(context.Context) ([]contract.Model, error) { return nil, nil }

func recordUpstream(t *testing.T, engine *Engine, model, upstream string) {
	t.Helper()
	err := engine.recordUsageAndEmit(func() error {
		return engine.recordMainUsage(context.Background(), mainUsageObservation{
			model: model,
			usage: contract.Usage{PromptTokens: 5_000, CompletionTokens: 10, PromptTokensAvailable: true, Upstream: upstream},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamFlipIsReportedAndRetiresWarmth(t *testing.T) {
	settings := advisorSettings()
	var notices []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "upstream", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Callbacks: contract.Callbacks{Notice: func(line string) { notices = append(notices, line) }},
	})
	if err != nil {
		t.Fatal(err)
	}

	recordUpstream(t, engine, "minimax-m3", "minimax")
	if !engine.modelWarmThisSession("minimax-m3") {
		t.Fatal("a served request must mark the model warm")
	}
	engine.emitUpstreamNoticeIfPending(context.Background())
	if len(notices) != 0 {
		t.Fatalf("a first upstream is not a flip and must be silent: %v", notices)
	}

	// Same model, same conversation, different upstream — the cache we thought
	// we had belongs to a machine this request never reached.
	recordUpstream(t, engine, "minimax-m3", "novita")
	engine.emitUpstreamNoticeIfPending(context.Background())

	if len(notices) != 1 || !strings.Contains(notices[0], "minimax") || !strings.Contains(notices[0], "novita") {
		t.Fatalf("the flip must be named, both sides of it: %v", notices)
	}
	if engine.modelWarmThisSession("minimax-m3") {
		t.Fatal("warmth survived an upstream flip — the advisor would price a cold start as free")
	}
}

// The field is absent on a direct provider connection, and absence must be
// completely silent: this notice fires on every turn otherwise.
func TestDirectConnectionNeverReportsAnUpstream(t *testing.T) {
	settings := advisorSettings()
	var notices []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "direct", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Callbacks: contract.Callbacks{Notice: func(line string) { notices = append(notices, line) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		recordUpstream(t, engine, "minimax-m3", "")
		engine.emitUpstreamNoticeIfPending(context.Background())
	}
	if len(notices) != 0 {
		t.Fatalf("a direct connection reports no upstream and must stay silent: %v", notices)
	}
	if !engine.modelWarmThisSession("minimax-m3") {
		t.Fatal("an absent upstream field must not retire warmth")
	}
}

// The session must actually ASK to go back to the upstream holding its prefix.
// Request 1 cannot know who that is; every request after it must say so, or the
// routing layer is free to re-route and re-bill the whole conversation.
func TestSessionPinsToTheUpstreamThatServedItFirst(t *testing.T) {
	settings := engineSettings()
	provider := &upstreamProvider{upstream: "minimax"}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pin", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider.responses = []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{Content: "done"},
	}
	if _, _, err := engine.Run(context.Background(), "read a.go and tell me what it does"); err != nil {
		t.Fatal(err)
	}
	if len(provider.pins) < 2 {
		t.Fatalf("expected at least two requests, got %d", len(provider.pins))
	}
	if provider.pins[0] != "" {
		t.Fatalf("the first request cannot know an upstream yet, sent %q", provider.pins[0])
	}
	for index, pin := range provider.pins[1:] {
		if pin != "minimax" {
			t.Fatalf("request %d did not pin to the warmed upstream: %q", index+1, pin)
		}
	}
}

// When the pinned upstream is unavailable the routing layer falls back, and the
// session must adopt the new one rather than asking forever for a machine that
// is not answering. A pin that cannot heal is a pin that turns one outage into
// a permanently cold session.
func TestPinFollowsAFallbackInsteadOfFightingIt(t *testing.T) {
	settings := advisorSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "heal", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	recordUpstream(t, engine, "minimax-m3", "minimax")
	if got := engine.upstreamPin(); got != "minimax" {
		t.Fatalf("pin = %q, want minimax", got)
	}
	recordUpstream(t, engine, "minimax-m3", "novita") // primary was down; fell back
	if got := engine.upstreamPin(); got != "novita" {
		t.Fatalf("pin = %q — the session kept asking for an upstream that did not serve it", got)
	}
}

// A routing layer under load can bounce a session between peers repeatedly.
// Each bounce is a real cold start and must be reported, but the reporting has
// to stay one calm line per change — and above all the task must still finish.
func TestAFlipStormReportsEachChangeAndStillCompletes(t *testing.T) {
	settings := advisorSettings()
	var notices []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "storm", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Callbacks: contract.Callbacks{Notice: func(line string) { notices = append(notices, line) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A, A, B, B, A — three distinct changes across five requests. A repeat of
	// the same upstream is not a change and must stay silent, or a busy router
	// turns the transcript into noise.
	for _, upstream := range []string{"minimax", "minimax", "novita", "novita", "minimax"} {
		recordUpstream(t, engine, "minimax-m3", upstream)
		engine.emitUpstreamNoticeIfPending(context.Background())
		// The pin must always name whoever just served us, never a stale peer.
		if got := engine.upstreamPin(); got != upstream {
			t.Fatalf("pin = %q after being served by %q", got, upstream)
		}
	}
	if len(notices) != 2 {
		t.Fatalf("expected one notice per CHANGE (minimax→novita, novita→minimax), got %d: %v", len(notices), notices)
	}
	// Every flip retires warmth; the last one leaves it retired.
	if engine.modelWarmThisSession("minimax-m3") {
		t.Fatal("warmth survived a flip storm")
	}
}

// A resumed session must pin to the upstream that last served it. Resume is the
// single most expensive request in a session — the conversation is at its
// largest — so landing it on a machine that never saw the conversation is the
// worst-case placement miss available.
func TestResumeRestoresTheUpstreamPin(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "resumed", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		InitialPrefixShape: &contract.PrefixShapeSnapshot{
			Version: contract.PrefixShapeSnapshotVersion, SystemHash: "S", ToolsHash: "T", ModelID: "main",
			Upstream: "novita",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.upstreamPin(); got != "novita" {
		t.Fatalf("resumed pin = %q, want the persisted upstream", got)
	}
	// Warmth is NOT restored: we cannot know what a provider still holds, so
	// switch pricing stays conservative on resume (switchcost.go).
	if engine.modelWarmThisSession("main") {
		t.Fatal("resume restored warmth — switch costs would be priced against an unverifiable cache")
	}
}

// A fresh session has no upstream to restore and must send no preference.
func TestFreshSessionHasNoPin(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "fresh", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.upstreamPin(); got != "" {
		t.Fatalf("a fresh session must carry no pin, got %q", got)
	}
}

// The persisted shape must carry the upstream, and re-arm on a flip so a resume
// pins to where the session ENDED rather than where it started.
func TestPersistedShapeCarriesTheCurrentUpstream(t *testing.T) {
	settings := engineSettings()
	var written []contract.PrefixShapeSnapshot
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "persist", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
		Persistence: Persistence{WritePrefixShape: func(_ context.Context, snapshot contract.PrefixShapeSnapshot) error {
			written = append(written, snapshot)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	shape := PrefixShape{SystemHash: "S", ToolsHash: "T", ModelID: "main"}

	recordUpstream(t, engine, "main", "minimax")
	engine.persistPrefixShapeOnce(context.Background(), shape)
	if len(written) != 1 || written[0].Upstream != "minimax" {
		t.Fatalf("first write = %+v, want upstream minimax", written)
	}
	// Written once and latched: a second call while nothing changed is a no-op.
	engine.persistPrefixShapeOnce(context.Background(), shape)
	if len(written) != 1 {
		t.Fatalf("the sidecar was rewritten with no change: %+v", written)
	}
	// A flip re-arms it, so the sidecar tracks the CURRENT upstream.
	recordUpstream(t, engine, "main", "novita")
	engine.persistPrefixShapeOnce(context.Background(), shape)
	if len(written) != 2 || written[1].Upstream != "novita" {
		t.Fatalf("a flip must re-arm the sidecar with the new upstream, got %+v", written)
	}
}

// The upstream must reach the persisted ledger, or a miss cannot be correlated
// against it after the fact — which is the entire diagnostic purpose.
func TestUpstreamIsPersistedOnTheUsageRecord(t *testing.T) {
	settings := advisorSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "ledger", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	recordUpstream(t, engine, "minimax-m3", "deepinfra")
	records := engine.UsageRecords()
	if len(records) == 0 {
		t.Fatal("no usage record was written")
	}
	if got := records[len(records)-1].Upstream; got != "deepinfra" {
		t.Fatalf("upstream on the record = %q, want deepinfra", got)
	}
}
