package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/muhiya/muhiyacode/internal/state"
)

const mcpTestServerEnvironment = "MUHIYA_RUN_MCP_TEST_SERVER"

type greetArguments struct {
	Name string `json:"name"`
}

func greet(_ context.Context, _ *mcp.CallToolRequest, args greetArguments) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Hello " + args.Name}}}, nil, nil
}

func newTestMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "Muhiya test MCP", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "Greet a person"}, greet)
	return server
}

func TestMain(tests *testing.M) {
	if os.Getenv(mcpTestServerEnvironment) == "1" {
		_ = os.Unsetenv(mcpTestServerEnvironment)
		if err := newTestMCPServer().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(tests.Run())
}

func TestManagerStdioDiscoveryExecutionAndRefresh(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.UpsertMCPServer(state.MCPServer{
		Name: "stdio", Enabled: true, TimeoutMS: 10_000, Transport: "stdio",
		Command: os.Args[0], Env: map[string]string{mcpTestServerEnvironment: "1"},
	}, paths); err != nil {
		t.Fatal(err)
	}
	confirmations := 0
	manager := New(paths, nil, func(context.Context, string) (bool, error) {
		confirmations++
		return true, nil
	}, nil)
	defer manager.Close()
	manager.Refresh(context.Background(), 10*time.Second)
	assertMCPGreeting(t, manager, "mcp__stdio__greet")
	if confirmations != 1 {
		t.Fatalf("confirmations = %d", confirmations)
	}
	manager.Refresh(context.Background(), 10*time.Second)
	if tools := manager.Tools(); len(tools) != 1 {
		t.Fatalf("refresh accumulated tools: %d", len(tools))
	}
	if statuses := manager.Statuses(); len(statuses) != 1 || statuses[0].State != "connected" {
		t.Fatalf("statuses = %+v", statuses)
	}
}

func TestPinnedSurfaceAppearsAtBoundaryThenLoadsBeforeHandshake(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.UpsertMCPServer(state.MCPServer{
		Name: "pinned", Enabled: true, TimeoutMS: 10_000, Transport: "stdio",
		Command: os.Args[0], Env: map[string]string{mcpTestServerEnvironment: "1"},
	}, paths); err != nil {
		t.Fatal(err)
	}
	first := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	if tools, err := first.PinnedTools(); err != nil || len(tools) != 0 {
		t.Fatalf("first pinned tools=%d err=%v", len(tools), err)
	}
	first.Refresh(context.Background(), 10*time.Second)
	change, changed, err := first.TakeBoundaryChange()
	if err != nil || !changed || len(change.Tools) != 1 {
		t.Fatalf("change=%+v changed=%v err=%v", change, changed, err)
	}
	if _, err := change.Tools[0].Execute(context.Background(), json.RawMessage(`{"name":"first"}`)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer second.Close()
	pinned, err := second.PinnedTools()
	if err != nil || len(pinned) != 1 || pinned[0].Definition().Function.Name != "mcp__pinned__greet" {
		t.Fatalf("cached pinned tools=%v err=%v", pinned, err)
	}
	if _, err := pinned[0].Execute(context.Background(), json.RawMessage(`{"name":"early"}`)); err == nil {
		t.Fatal("forwarder executed before live handshake")
	}
	second.Refresh(context.Background(), 10*time.Second)
	output, err := pinned[0].Execute(context.Background(), json.RawMessage(`{"name":"second"}`))
	if err != nil || !strings.Contains(output, "Hello second") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	if _, changed, err := second.TakeBoundaryChange(); err != nil || changed {
		t.Fatalf("known cached server changed in-session: changed=%v err=%v", changed, err)
	}
}

func TestManagerHTTPBearerAndBrokenServerIsolation(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	server := newTestMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer access-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, request)
	}))
	defer httpServer.Close()
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	config := state.MCPConfig{Version: 1, Servers: []state.MCPServer{
		{Name: "broken", Enabled: true, TimeoutMS: 1000, Transport: "stdio", Command: "muhiya-command-that-does-not-exist"},
		{Name: "remote", Enabled: true, TimeoutMS: 10_000, Transport: "http", URL: httpServer.URL, OAuth: &state.MCPOAuth{Enabled: true, RedirectPort: 35698}},
	}}
	if err := state.SaveMCPConfig(config, paths); err != nil {
		t.Fatal(err)
	}
	secrets := state.DefaultMCPSecrets()
	secrets.OAuth["remote"] = map[string]any{"tokens": map[string]any{"access_token": "access-token"}}
	if err := state.SaveMCPSecrets(secrets, paths); err != nil {
		t.Fatal(err)
	}
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()
	manager.Refresh(context.Background(), 10*time.Second)
	assertMCPGreeting(t, manager, "mcp__remote__greet")
	statuses := manager.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("statuses = %+v", statuses)
	}
	foundBroken, foundRemote := false, false
	for _, status := range statuses {
		foundBroken = foundBroken || status.Name == "broken" && status.State == "error"
		foundRemote = foundRemote || status.Name == "remote" && status.State == "connected"
	}
	if !foundBroken || !foundRemote {
		t.Fatalf("server isolation failed: %+v", statuses)
	}
}

func TestOAuthTokenRefreshPersistsRotatedCredentials(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != "refresh-old" || request.Form.Get("client_id") != "client-id" {
			t.Errorf("refresh form = %v", request.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"access-new","refresh_token":"refresh-new","token_type":"Bearer","expires_in":3600}`)
	}))
	defer tokenServer.Close()
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	secrets := state.DefaultMCPSecrets()
	secrets.OAuth["refresh"] = map[string]any{
		"clientInformation": map[string]any{"client_id": "client-id", "token_endpoint_auth_method": "none"},
		"tokenEndpoint":     tokenServer.URL,
		"tokens": map[string]any{
			"access_token": "access-old", "refresh_token": "refresh-old", "token_type": "Bearer", "expiry": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		},
	}
	if err := state.SaveMCPSecrets(secrets, paths); err != nil {
		t.Fatal(err)
	}
	manager := New(paths, tokenServer.Client(), nil, nil)
	token, err := manager.oauthAccessToken(context.Background(), "refresh", secrets.OAuth["refresh"])
	if err != nil || token != "access-new" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	loaded, err := state.LoadMCPSecrets(paths)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := loaded.OAuth["refresh"]["tokens"].(map[string]any)
	if stored["access_token"] != "access-new" || stored["refresh_token"] != "refresh-new" {
		t.Fatalf("rotated token was not persisted: %#v", stored)
	}
}

func assertMCPGreeting(t *testing.T, manager *Manager, expectedName string) {
	t.Helper()
	tools := manager.Tools()
	if len(tools) != 1 || tools[0].Definition().Function.Name != expectedName {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Definition().Function.Name)
		}
		t.Fatalf("tools = %#v", names)
	}
	parameters := tools[0].Definition().Function.Parameters
	properties, _ := parameters["properties"].(map[string]any)
	if _, ok := properties["name"]; !ok {
		t.Fatalf("MCP input schema was not preserved: %#v", parameters)
	}
	output, err := tools[0].Execute(context.Background(), json.RawMessage(`{"name":"Muhiya"}`))
	if err != nil || !strings.Contains(output, "Hello Muhiya") {
		t.Fatalf("output=%q err=%v", output, err)
	}
}
