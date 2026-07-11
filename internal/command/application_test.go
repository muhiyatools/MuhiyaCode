package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"go-agent-model","name":"Go Agent","context_window":64000}]}`)
		case "/v1/capabilities", "/capabilities":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"features":{"web_search":false}}`)
		case "/v1/chat/completions":
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			mu.Lock()
			wireModels = append(wireModels, body.Model)
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello from Go.\"}}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n")
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
	mu.Lock()
	defer mu.Unlock()
	if len(wireModels) != 1 || wireModels[0] != "go-agent-model" {
		t.Fatalf("wire models = %v", wireModels)
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

func TestParsePlanAndKeyValues(t *testing.T) {
	t.Parallel()
	plan := parsePlan("- [x] Inspect (completed)\n- [ ] Build (in_progress)\n\nKeep scope tight.\n")
	if len(plan.Steps) != 2 || plan.Steps[1].Status != contract.PlanInProgress || plan.Note != "Keep scope tight." {
		t.Fatalf("plan = %+v", plan)
	}
	values, err := parseKeyValues([]string{"TOKEN=a=b", "EMPTY="})
	if err != nil || values["TOKEN"] != "a=b" || values["EMPTY"] != "" {
		t.Fatalf("values=%v err=%v", values, err)
	}
	if _, err := parseKeyValues([]string{"INVALID"}); err == nil {
		t.Fatal("expected invalid environment entry to fail")
	}
}
