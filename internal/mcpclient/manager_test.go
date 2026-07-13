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
	"github.com/muhiya/muhiyacode/internal/contract"
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

// TestMarkServerErrorClearsToolsAndAllowsReconnect (T032 / REV D1) verifies the
// dead-reconnect fix: a simulated transport failure drops the connection AND the
// stale tool entries (so the forwarding tool's next call takes the lazy
// reconnect path instead of calling a closed session forever), flips status to
// error, and a subsequent EnsureLive recovers the server and its tools.
func TestMarkServerErrorClearsToolsAndAllowsReconnect(t *testing.T) {
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
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()
	manager.Refresh(context.Background(), 10*time.Second)
	if !manager.HasLiveServer("stdio") {
		t.Fatal("setup: stdio should be live")
	}
	manager.mu.RLock()
	_, hadTool := manager.tools["mcp__stdio__greet"]
	manager.mu.RUnlock()
	if !hadTool {
		t.Fatal("setup: greet tool should be registered")
	}

	// Simulate a transport-level failure (what mcpTool.Execute does on EOF/close).
	manager.markServerError("stdio", "stdio: connection closed")

	if manager.HasLiveServer("stdio") {
		t.Fatal("connection should be dropped after markServerError")
	}
	manager.mu.RLock()
	_, stillTool := manager.tools["mcp__stdio__greet"]
	manager.mu.RUnlock()
	if stillTool {
		t.Fatal("D1: the stale tool entry must be deleted so the next call takes the reconnect path")
	}
	if statuses := manager.Statuses(); len(statuses) != 1 || statuses[0].State != "error" {
		t.Fatalf("status after markServerError = %+v, want error", statuses)
	}

	// The lazy reconnect path recovers on the next attempt.
	if err := manager.EnsureLive(context.Background(), "stdio"); err != nil {
		t.Fatalf("EnsureLive after error did not reconnect: %v", err)
	}
	if !manager.HasLiveServer("stdio") {
		t.Fatal("EnsureLive should have reconnected the server")
	}
	manager.mu.RLock()
	_, backTool := manager.tools["mcp__stdio__greet"]
	manager.mu.RUnlock()
	if !backTool {
		t.Fatal("greet tool should be back in the registry after reconnect")
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
	// M5: the forwarding tool now lazy-connects on first Execute. Calling
	// it WITHOUT a fresh session-level Refresh still succeeds because the
	// cached surface triggers an on-demand connect within the lazy start
	// timeout.
	output, err := pinned[0].Execute(context.Background(), json.RawMessage(`{"name":"early"}`))
	if err != nil || !strings.Contains(output, "Hello early") {
		t.Fatalf("lazy-connect failed: output=%q err=%v", output, err)
	}
	// Surface stabilization: subsequent calls hit the live session, no
	// boundary change is needed.
	output, err = pinned[0].Execute(context.Background(), json.RawMessage(`{"name":"later"}`))
	if err != nil || !strings.Contains(output, "Hello later") {
		t.Fatalf("second execute failed: output=%q err=%v", output, err)
	}
	if _, changed, err := second.TakeBoundaryChange(); err != nil || changed {
		t.Fatalf("known cached server changed in-session: changed=%v err=%v", changed, err)
	}
}

func TestLazyConnectFailsLoudlyOnShortContext(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	// Server points at a non-existent command so no live handshake can
	// succeed inside the bound. EnsureLive must surface the H8-style
	// unavailable message instead of blocking past the lazy deadline.
	if err := state.UpsertMCPServer(state.MCPServer{
		Name: "wedged", Enabled: true, TimeoutMS: 200, Transport: "stdio",
		Command: "muhiya-command-that-does-not-exist",
	}, paths); err != nil {
		t.Fatal(err)
	}
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()
	// Seed the pinned-tools inline so the forwarding path resolves the
	// tool name without a prior Refresh round (the cached-surface path
	// that M5 ships from).
	manager.mu.Lock()
	manager.pinned = map[string]contract.ToolDefinition{"mcp__wedged__probe": canonicalDefinition(contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: "mcp__wedged__probe", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}})}
	manager.applied = cloneDefinitionsByName(manager.pinned)
	manager.mu.Unlock()
	// Bound the call so a wedged server cannot stall the goroutine: a
	// tight test context aborts the lazy connect within 2.5s.
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	err = manager.EnsureLive(ctx, "wedged")
	if err == nil {
		t.Fatal("EnsureLive did not surface an error for a wedged server")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected unavailable message, got %v", err)
	}
	found := false
	for _, status := range manager.Statuses() {
		if status.Name == "wedged" && status.State == "error" {
			found = true
		}
	}
	if !found {
		t.Fatalf("wedged server was not flipped to error: %+v", manager.Statuses())
	}
}

func TestLazyConnectUsesOnlyTargetServer(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := state.UpsertMCPServer(state.MCPServer{
			Name: name, Enabled: true, TimeoutMS: 10_000, Transport: "stdio",
			Command: os.Args[0], Env: map[string]string{mcpTestServerEnvironment: "1"},
		}, paths); err != nil {
			t.Fatal(err)
		}
	}
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()
	// Pre-established live "b" so we can verify EnsureLive("a") only
	// touches "a": "b" stays connected.
	manager.Refresh(context.Background(), 10*time.Second)
	if !manager.HasLiveServer("b") {
		t.Fatal("setup: b should be live")
	}
	beforeB := manager.connectionForTest("b")
	// Mutate manager.pinned so EnsureLive picks "a" through the cache.
	manager.mu.Lock()
	manager.pinned = map[string]contract.ToolDefinition{
		"mcp__a__greet": canonicalDefinition(contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: "mcp__a__greet", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}),
	}
	manager.applied = cloneDefinitionsByName(manager.pinned)
	manager.mu.Unlock()
	if err := manager.EnsureLive(context.Background(), "a"); err != nil {
		t.Fatalf("EnsureLive(a) failed: %v", err)
	}
	if !manager.HasLiveServer("a") {
		t.Fatal("EnsureLive(a) did not produce a live connection")
	}
	if !manager.HasLiveServer("b") {
		t.Fatal("EnsureLive(a) reaped the b connection unexpectedly")
	}
	afterB := manager.connectionForTest("b")
	if beforeB != afterB {
		t.Fatal("EnsureLive(a) replaced the b connection (must be untouched)")
	}
}

func TestRefreshBlockingWaitsForOAuthRoundTrip(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	server := newTestMCPServer()
	// Introduce a 750ms stall in the fake transport so a non-blocking
	// Refresh would return before the round trip completes.
	handlerDelay := make(chan struct{}, 1)
	defer close(handlerDelay)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		select {
		case <-handlerDelay:
		case <-time.After(750 * time.Millisecond):
		}
		handler.ServeHTTP(w, request)
	}))
	defer httpServer.Close()
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	config := state.MCPConfig{Version: 1, Servers: []state.MCPServer{{Name: "remote", Enabled: true, TimeoutMS: 5_000, Transport: "http", URL: httpServer.URL}}}
	if err := state.SaveMCPConfig(config, paths); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveMCPSecrets(state.DefaultMCPSecrets(), paths); err != nil {
		t.Fatal(err)
	}
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()
	manager.RefreshBlocking(context.Background())
	for _, status := range manager.Statuses() {
		if status.Name == "remote" && status.State != "connected" {
			t.Fatalf("RefreshBlocking did not wait for connect: state=%q", status.State)
		}
	}
}

// TestRefreshBlockingHonorsContextTimeout (T034 / REV D3) verifies a bounded
// context makes RefreshBlocking return well before a wedged server's stall,
// rather than blocking on the connect round trip indefinitely.
func TestRefreshBlockingHonorsContextTimeout(t *testing.T) {
	t.Setenv(state.HomeEnvironment, t.TempDir())
	server := newTestMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		time.Sleep(3 * time.Second) // stall well past the caller's timeout
		handler.ServeHTTP(w, request)
	}))
	defer httpServer.Close()
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	config := state.MCPConfig{Version: 1, Servers: []state.MCPServer{{Name: "remote", Enabled: true, TimeoutMS: 10_000, Transport: "http", URL: httpServer.URL}}}
	if err := state.SaveMCPConfig(config, paths); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveMCPSecrets(state.DefaultMCPSecrets(), paths); err != nil {
		t.Fatal(err)
	}
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	manager.RefreshBlocking(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("RefreshBlocking did not honor the context timeout: waited %s (server stalls 3s)", elapsed)
	}
}

func TestRefreshDedupJoinsInFlightRound(t *testing.T) {
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
	manager := New(paths, nil, func(context.Context, string) (bool, error) { return true, nil }, nil)
	defer manager.Close()
	// Fire two Refreshes back-to-back; the second one must NOT cancel
	// the first (would re-create the stdio process). We watch the
	// connection pointer — if dedup works it stays identical.
	firstDone := make(chan struct{})
	var firstConn, secondConn *connection
	go func() {
		defer close(firstDone)
		manager.Refresh(context.Background(), 0)
		firstConn = manager.connectionForTest("stdio")
	}()
	// Tiny sleep so the first round had time to register itself.
	time.Sleep(50 * time.Millisecond)
	manager.Refresh(context.Background(), 0)
	secondConn = manager.connectionForTest("stdio")
	<-firstDone
	if firstConn == nil || secondConn == nil {
		t.Fatal("both Refresh calls must produce a live connection")
	}
	if firstConn != secondConn {
		t.Fatal("concurrent Refresh replaced the connection (dedup should have joined it)")
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
