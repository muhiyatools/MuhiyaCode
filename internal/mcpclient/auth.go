package mcpclient

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/muhiya/muhiyacode/internal/state"
)

type AuthorizeOptions struct {
	Timeout     time.Duration
	OnURL       func(string)
	OpenBrowser func(string) bool
}

type oauthCapture struct {
	mu                     sync.Mutex
	clientInformation      map[string]any
	discoveryState         map[string]any
	tokenEndpoint          string
	authorizationServerURL string
}

type oauthCaptureTransport struct {
	base    http.RoundTripper
	capture *oauthCapture
}

func (m *Manager) Authorize(ctx context.Context, name string, options AuthorizeOptions) error {
	config, err := state.LoadMCPConfig(m.paths)
	if err != nil {
		return err
	}
	var server *state.MCPServer
	for i := range config.Servers {
		if config.Servers[i].Name == name {
			server = &config.Servers[i]
			break
		}
	}
	if server == nil {
		return fmt.Errorf("MCP server not found: %s", name)
	}
	if server.Transport != "http" || server.OAuth == nil || !server.OAuth.Enabled {
		return fmt.Errorf("OAuth is only available for OAuth-enabled HTTP MCP servers")
	}
	if options.Timeout <= 0 {
		options.Timeout = 3 * time.Minute
	}
	capture := &oauthCapture{}
	oauthClient := *m.httpClient
	oauthClient.Transport = oauthCaptureTransport{base: transportOf(m.httpClient), capture: capture}
	redirect := "http://127.0.0.1:" + strconv.Itoa(server.OAuth.RedirectPort) + "/oauth/callback"
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		RedirectURL: redirect,
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			RedirectURIs:            []string{redirect},
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			ClientName:              "MuhiyaCode",
			Scope:                   server.OAuth.Scope,
			ApplicationType:         "native",
		}},
		Client: &oauthClient,
		AuthorizationCodeFetcher: func(flow context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			if options.OnURL != nil {
				options.OnURL(args.URL)
			}
			opener := options.OpenBrowser
			if opener == nil {
				opener = openBrowser
			}
			_ = opener(args.URL)
			return waitForCallback(flow, server.OAuth.RedirectPort, args.URL, options.Timeout)
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		return err
	}
	resp := &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: http.NoBody, Request: req}
	if err := handler.Authorize(ctx, req, resp); err != nil {
		return fmt.Errorf("authorize MCP %s: %w", name, err)
	}
	tokenSource, err := handler.TokenSource(ctx)
	if err != nil || tokenSource == nil {
		return fmt.Errorf("OAuth completed without a token source: %w", err)
	}
	token, err := tokenSource.Token()
	if err != nil {
		return err
	}
	m.secretsMu.Lock()
	defer m.secretsMu.Unlock()
	secrets, err := state.LoadMCPSecrets(m.paths)
	if err != nil {
		return err
	}
	entry := secrets.OAuth[name]
	if entry == nil {
		entry = make(map[string]any)
	}
	entry["tokens"] = encodeOAuthToken(token)
	clientInformation, discoveryState, tokenEndpoint, authorizationServerURL := capture.snapshot()
	if clientInformation != nil {
		entry["clientInformation"] = clientInformation
	}
	if discoveryState != nil {
		entry["discoveryState"] = discoveryState
	}
	if tokenEndpoint != "" {
		entry["tokenEndpoint"] = tokenEndpoint
	}
	if authorizationServerURL != "" {
		entry["authorizationServerUrl"] = authorizationServerURL
	}
	entry["resourceUrl"] = server.URL
	secrets.OAuth[name] = entry
	if err := state.SaveMCPSecrets(secrets, m.paths); err != nil {
		return err
	}
	m.setStatus(name, "authorized", "OAuth tokens saved.", 0)
	return nil
}

func (t oauthCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if readErr != nil {
		return response, nil
	}
	if len(data) > 2*1024*1024 {
		response.Body = io.NopCloser(io.MultiReader(bytes.NewReader(data), response.Body))
		return response, nil
	}
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(data))
	var payload map[string]any
	if json.Unmarshal(data, &payload) != nil {
		return response, nil
	}
	t.capture.observe(request.URL.String(), payload)
	return response, nil
}

func (c *oauthCapture) observe(requestURL string, payload map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if stringField(payload, "client_id", "clientId") != "" {
		c.clientInformation = cloneMap(payload)
	}
	if endpoint := stringField(payload, "token_endpoint", "tokenEndpoint"); endpoint != "" {
		c.tokenEndpoint = endpoint
		c.discoveryState = cloneMap(payload)
	}
	if stringField(payload, "access_token", "accessToken") != "" {
		c.tokenEndpoint = requestURL
	}
	if issuer := stringField(payload, "issuer"); issuer != "" {
		c.authorizationServerURL = issuer
	}
}

func (c *oauthCapture) snapshot() (map[string]any, map[string]any, string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneMap(c.clientInformation), cloneMap(c.discoveryState), c.tokenEndpoint, c.authorizationServerURL
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

func waitForCallback(ctx context.Context, port int, authorizationURL string, timeout time.Duration) (*auth.AuthorizationResult, error) {
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		return nil, err
	}
	expected := parsed.Query().Get("state")
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return nil, fmt.Errorf("start OAuth callback listener: %w", err)
	}
	defer listener.Close()
	result := make(chan *auth.AuthorizationResult, 1)
	errors := make(chan error, 1)
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if oauthError := query.Get("error"); oauthError != "" {
			http.Error(w, "MuhiyaCode OAuth failed. You can close this tab.", http.StatusBadRequest)
			select {
			case errors <- fmt.Errorf("OAuth failed: %s", oauthError):
			default:
			}
			return
		}
		// Fail closed: a missing expected state (e.g. an authorization URL
		// built without one) must not be treated as "no check needed" since
		// state is the CSRF defense for this loopback callback listener.
		stateValue := query.Get("state")
		if expected == "" || subtle.ConstantTimeCompare([]byte(stateValue), []byte(expected)) != 1 {
			http.Error(w, "OAuth state mismatch. Restart authorization from MuhiyaCode.", http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(w, "Missing OAuth code.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>MuhiyaCode MCP Authorized</title><style>body{font-family:system-ui;background:#111;color:#f4f4f5;display:grid;place-items:center;min-height:100vh;margin:0}main{max-width:520px;padding:32px}p{color:#a1a1aa}</style></head><body><main><h1>MuhiyaCode MCP authorized</h1><p>You can close this tab and return to the terminal.</p></main></body></html>`))
		select {
		case result <- &auth.AuthorizationResult{Code: code, State: stateValue}:
		default:
		}
	})
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case errors <- err:
			default:
			}
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		shutdown, shutdownCancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer shutdownCancel()
		_ = server.Shutdown(shutdown)
	}()
	select {
	case value := <-result:
		return value, nil
	case err := <-errors:
		return nil, err
	case <-waitCtx.Done():
		return nil, fmt.Errorf("timed out waiting for MCP OAuth callback: %w", waitCtx.Err())
	}
}
