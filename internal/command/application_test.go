package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

func TestApplicationDiscoversModelRunsAndResumes(t *testing.T) {
	var mu sync.Mutex
	var wireModels []string
	var wireBodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"go-agent-model","name":"Go Agent","context_window":64000}]}`)
		case "/v1/capabilities", "/capabilities":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"features":{"web_search":false}}`)
		case "/v1/chat/completions":
			raw, _ := io.ReadAll(request.Body)
			var body struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(raw, &body)
			mu.Lock()
			wireModels = append(wireModels, body.Model)
			wireBodies = append(wireBodies, append([]byte(nil), raw...))
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello from Go.\"}}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":4,\"prompt_cache_hit_tokens\":9,\"prompt_cache_miss_tokens\":2}}\n\ndata: [DONE]\n\n")
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv(state.HomeEnvironment, home)
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	settings := state.DefaultSettings()
	settings.Provider.BaseURL = server.URL + "/v1"
	if err := state.SaveSettings(settings, paths); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSecrets(contract.Secrets{ProviderAPIKey: "sk-test-application-key"}, paths); err != nil {
		t.Fatal(err)
	}

	app, err := OpenApplication(ApplicationOptions{Context: context.Background(), Workspace: workspace, NewSession: true, Title: "integration", DisableMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := app.Runtime().Session.ID
	answer, stats, err := app.Runtime().Engine.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Hello from Go." || stats.Usage.TotalTokens != 15 {
		t.Fatalf("answer=%q stats=%+v", answer, stats)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, err := state.LoadSettings(paths)
	if err != nil {
		t.Fatal(err)
	}
	model, ok := state.ActiveModel(loaded)
	if !ok || model.ID != "go-agent-model" || model.ContextLimit != 64000 {
		t.Fatalf("model discovery was not persisted: %+v", loaded.Provider)
	}
	resumed, err := OpenApplication(ApplicationOptions{Context: context.Background(), SessionID: sessionID, DisableMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if got := resumed.Runtime().Session.ID; got != sessionID {
		t.Fatalf("resumed session %q, want %q", got, sessionID)
	}
	if aggregate := resumed.Runtime().Engine.UsageAggregate(); aggregate.Requests != 1 || aggregate.SumPrompt != 11 || aggregate.SumCompletion != 4 || aggregate.SumCacheRead != 9 || aggregate.SumCacheMiss != 2 || aggregate.UnavailableRequests != 0 {
		t.Fatalf("resumed usage aggregate = %+v", aggregate)
	}
	if usage := resumed.Runtime().Engine.Usage(); usage.TotalTokens != 15 {
		t.Fatalf("resumed legacy usage = %+v", usage)
	}
	if events := resumed.Recent(); len(events) < 2 || events[len(events)-1].Content != "Hello from Go." {
		t.Fatalf("resumed events = %#v", events)
	}
	var history map[string]any
	if err := resumed.sessions.ReadJSON(sessionID, "history.json", nil, &history); err != nil {
		t.Fatal(err)
	}
	if messages, _ := history["messages"].([]any); len(messages) < 2 {
		t.Fatalf("history was not persisted: %#v", history)
	}
	if _, _, err := resumed.Runtime().Engine.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if aggregate := resumed.Runtime().Engine.UsageAggregate(); aggregate.Requests != 2 || aggregate.SumPrompt != 22 || aggregate.SumCompletion != 8 || aggregate.SumCacheRead != 18 || aggregate.SumCacheMiss != 4 {
		t.Fatalf("post-resume cumulative usage = %+v", aggregate)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(wireModels) != 2 || wireModels[0] != "go-agent-model" || wireModels[1] != "go-agent-model" {
		t.Fatalf("wire models = %v", wireModels)
	}
	assertRestartPrefixStable(t, wireBodies)
}

func assertRestartPrefixStable(t *testing.T, bodies [][]byte) {
	t.Helper()
	if len(bodies) != 2 {
		t.Fatalf("wire body count = %d", len(bodies))
	}
	var decoded [2]struct {
		Messages []json.RawMessage `json:"messages"`
		Tools    json.RawMessage   `json:"tools"`
	}
	for index := range bodies {
		if err := json.Unmarshal(bodies[index], &decoded[index]); err != nil {
			t.Fatal(err)
		}
	}
	if string(decoded[0].Tools) != string(decoded[1].Tools) {
		t.Fatal("tool bytes changed across application restart")
	}
	if len(decoded[1].Messages) <= len(decoded[0].Messages) {
		t.Fatalf("resumed request did not append history: %d <= %d", len(decoded[1].Messages), len(decoded[0].Messages))
	}
	for index := range decoded[0].Messages {
		if string(decoded[0].Messages[index]) != string(decoded[1].Messages[index]) {
			t.Fatalf("message %d changed across restart\nfirst: %s\nnext:  %s", index, decoded[0].Messages[index], decoded[1].Messages[index])
		}
	}
}

func TestCLIConfigAndMCPRoundTrip(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	var output, errorsOut bytes.Buffer
	run := func(args ...string) error {
		command := NewRootCommand()
		command.SetArgs(args)
		command.SetIn(strings.NewReader(""))
		command.SetOut(&output)
		command.SetErr(&errorsOut)
		return command.ExecuteContext(context.Background())
	}
	if err := run("config", "set", "model", "test-model"); err != nil {
		t.Fatal(err)
	}
	if err := run("config", "set", "contextLimit", "32000"); err != nil {
		t.Fatal(err)
	}
	if err := run("mcp", "add-http", "local tools", "http://127.0.0.1:7890/mcp", "--timeout-ms", "2500"); err != nil {
		t.Fatal(err)
	}
	if err := run("mcp", "list"); err != nil {
		t.Fatal(err)
	}
	if err := run("config"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "local_tools") || !strings.Contains(output.String(), "test-model") {
		t.Fatalf("output = %s; errors = %s", output.String(), errorsOut.String())
	}
	paths, settings, _, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	model, ok := state.ActiveModel(settings)
	if !ok || model.ContextLimit != 32000 {
		t.Fatalf("settings = %+v", settings.Provider)
	}
	config, err := state.LoadMCPConfig(paths)
	if err != nil || len(config.Servers) != 1 || config.Servers[0].TimeoutMS != 2500 {
		t.Fatalf("MCP config=%+v err=%v", config, err)
	}
	if _, err := os.Stat(filepath.Join(paths.Home, "mcp.json")); err != nil {
		t.Fatal(err)
	}
}

func TestUsageIntegrityThroughApplicationLoop(t *testing.T) {
	responses := []string{
		`{"prompt_tokens":10,"completion_tokens":1,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":10}`,
		`{"prompt_tokens":10,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":8}}`,
		`{"prompt_tokens":10,"completion_tokens":1}`,
		`{"prompt_tokens":"bad","completion_tokens":1,"prompt_cache_hit_tokens":"bad"}`,
	}
	requestIndex := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/capabilities", "/capabilities":
			fmt.Fprint(w, `{"features":{"web_search":false}}`)
		case "/v1/chat/completions":
			usage := responses[requestIndex]
			requestIndex++
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}],\"usage\":%s}\n\ndata: [DONE]\n\n", usage)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	settings := state.DefaultSettings()
	settings.Provider.BaseURL = server.URL + "/v1"
	settings.Provider.ActiveModelID = "model"
	settings.Provider.Models = []contract.Model{{ID: "model", Name: "Model", ContextLimit: 128000}}
	if err := state.SaveSettings(settings, paths); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSecrets(contract.Secrets{ProviderAPIKey: "test-key"}, paths); err != nil {
		t.Fatal(err)
	}
	app, err := OpenApplication(ApplicationOptions{Context: context.Background(), Workspace: t.TempDir(), NewSession: true, DisableMCP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, prompt := range []string{"hi", "again", "third", "fourth"} {
		if _, _, err := app.Runtime().Engine.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	records := app.Runtime().Engine.UsageRecords()
	if len(records) != 4 {
		t.Fatalf("records=%+v", records)
	}
	if records[0].CacheReadTokens == nil || *records[0].CacheReadTokens != 0 || records[0].CacheMissTokens == nil || *records[0].CacheMissTokens != 10 {
		t.Fatalf("DeepSeek record=%+v", records[0])
	}
	if records[1].CacheReadTokens == nil || *records[1].CacheReadTokens != 8 || records[1].CacheMissTokens == nil || *records[1].CacheMissTokens != 2 || !records[1].MissDerived {
		t.Fatalf("OpenAI record=%+v", records[1])
	}
	if records[2].CacheReadTokens != nil || records[2].Attribution != contract.CacheAttributionNA {
		t.Fatalf("absent record=%+v", records[2])
	}
	if records[3].CacheReadTokens != nil || records[3].Diagnostic == "" {
		t.Fatalf("malformed record=%+v", records[3])
	}
	aggregate := app.Runtime().Engine.UsageAggregate()
	if aggregate.SumCacheRead != 8 || aggregate.SumCacheMiss != 12 || aggregate.UnavailableRequests != 2 {
		t.Fatalf("aggregate=%+v", aggregate)
	}
}

func TestParseKeyValues(t *testing.T) {
	t.Parallel()
	values, err := parseKeyValues([]string{"TOKEN=a=b", "EMPTY="})
	if err != nil || values["TOKEN"] != "a=b" || values["EMPTY"] != "" {
		t.Fatalf("values=%v err=%v", values, err)
	}
	if _, err := parseKeyValues([]string{"INVALID"}); err == nil {
		t.Fatal("expected invalid environment entry to fail")
	}
}
