package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	gatewayclient "github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/mcpclient"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/spf13/cobra"
)

const oauthClientID = "muhiyacode"

type tokenExchangeRequest struct {
	platformURL string
	code        string
	verifier    string
	redirectURI string
}

// newLoginCommand implements a browser-based sign-in (RFC 8252 native-app flow:
// loopback redirect + PKCE). It opens the Muhiya PLATFORM (not the gateway),
// where a logged-in user authorizes the CLI; the platform then hands back a
// gateway API key over an HTTPS back-channel. No API key is ever pasted by hand.
func newLoginCommand() *cobra.Command {
	var platformFlag string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to Muhiya through your browser (no API key needed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, platformFlag)
		},
	}
	cmd.Flags().StringVar(&platformFlag, "platform", "", "Muhiya platform base URL (overrides MUHIYACODE_PLATFORM_URL / derived)")
	return cmd
}

func newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored MuhiyaCode credential",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths, _, secrets, err := loadConfig()
			if err != nil {
				return err
			}
			if strings.TrimSpace(secrets.ProviderAPIKey) == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "Already signed out.")
				return nil
			}
			secrets.ProviderAPIKey = ""
			if err := state.SaveSecrets(secrets, paths); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Signed out. Your local credential was removed.")
			return nil
		},
	}
}

func runLogin(cmd *cobra.Command, platformOverride string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Opening your browser to sign in to Muhiya…")
	email, _, err := performBrowserLogin(cmd.Context(), platformOverride, func(u string) {
		fmt.Fprintln(out, "If it does not open automatically, visit:")
		fmt.Fprintln(out, "  "+u)
	})
	if err != nil {
		return err
	}
	if email != "" {
		fmt.Fprintf(out, "\n✓ Signed in as %s.\n", email)
	} else {
		fmt.Fprintln(out, "\n✓ Signed in.")
	}
	if _, settings, _, cerr := loadConfig(); cerr == nil {
		if _, ok := state.ActiveModel(settings); !ok {
			fmt.Fprintln(out, "Next: run 'muhiyacode config discover' to load available models.")
		}
	}
	return nil
}

// performBrowserLogin runs the loopback+PKCE browser sign-in and persists the
// resulting gateway key. onURL (optional) receives the authorization URL as a
// fallback for when the browser can't be opened automatically. It returns the
// signed-in email (may be empty) and the raw token so the TUI can also sync its
// in-memory credential via setAPIKey.
func performBrowserLogin(ctx context.Context, platformOverride string, onURL func(string)) (string, string, error) {
	paths, settings, secrets, err := loadConfig()
	if err != nil {
		return "", "", err
	}

	platformURL, err := resolvePlatformURL(platformOverride, settings.Provider.BaseURL)
	if err != nil {
		return "", "", err
	}

	// PKCE (S256) + CSRF state.
	verifier := randomURLToken(64)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	stateTok := randomURLToken(24)

	// Loopback listener on an ephemeral port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	authorizeURL := platformURL + "/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {oauthClientID},
		"redirect_uri":          {redirectURI},
		"state":                 {stateTok},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	type callback struct {
		code string
		err  error
	}
	resultCh := make(chan callback, 1)
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writeLoginHTML(w, false)
			select {
			case resultCh <- callback{err: fmt.Errorf("authorization denied: %s", e)}:
			default:
			}
			return
		}
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(stateTok)) != 1 {
			http.Error(w, "OAuth state mismatch. Restart 'muhiyacode login'.", http.StatusBadRequest)
			select {
			case resultCh <- callback{err: fmt.Errorf("state mismatch (possible CSRF)")}:
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			return
		}
		writeLoginHTML(w, true)
		select {
		case resultCh <- callback{code: code}:
		default:
		}
	})
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if onURL != nil {
		onURL(authorizeURL)
	}
	_ = mcpclient.OpenBrowser(authorizeURL)

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var code string
	select {
	case res := <-resultCh:
		if res.err != nil {
			return "", "", res.err
		}
		code = res.code
	case <-waitCtx.Done():
		return "", "", fmt.Errorf("timed out waiting for browser authorization")
	}

	token, baseURL, email, userID, err := exchangeToken(waitCtx, tokenExchangeRequest{
		platformURL: platformURL,
		code:        code,
		verifier:    verifier,
		redirectURI: redirectURI,
	})
	if err != nil {
		return "", "", err
	}

	if strings.TrimSpace(baseURL) != "" {
		settings.Provider.BaseURL = baseURL
	}
	if err := verifyGatewayIdentity(waitCtx, settings, token, userID); err != nil {
		return "", "", err
	}

	secrets.ProviderAPIKey = token
	if err := state.SaveSettings(settings, paths); err != nil {
		return "", "", err
	}
	if err := state.SaveSecrets(secrets, paths); err != nil {
		return "", "", err
	}
	return email, token, nil
}

func verifyGatewayIdentity(ctx context.Context, settings contract.Settings, token, oauthUserID string) error {
	gatewayUserID, err := authenticatedGatewayUserID(ctx, settings, token)
	if err != nil {
		return err
	}
	if oauthUserID == "" || gatewayUserID != oauthUserID {
		return fmt.Errorf(
			"gateway identity mismatch: OAuth user %q, token user %q",
			oauthUserID, gatewayUserID,
		)
	}
	return nil
}

func authenticatedGatewayUserID(ctx context.Context, settings contract.Settings, token string) (string, error) {
	if strings.HasPrefix(strings.TrimSpace(token), "vk-") {
		return "", fmt.Errorf("gateway key IDs cannot authenticate requests; paste the sk-virt bearer token")
	}
	usage, err := gatewayclient.FetchUsage(ctx, settings, token)
	if err != nil {
		return "", fmt.Errorf("verify gateway identity: %w", err)
	}
	if strings.TrimSpace(usage.User.ID) == "" {
		return "", fmt.Errorf("verify gateway identity: usage response has no user ID")
	}
	return usage.User.ID, nil
}

// resolvePlatformURL picks the platform base URL: explicit flag, then
// MUHIYACODE_PLATFORM_URL, then a derivation from the gateway base URL
// (api.host -> host). Local/dev setups should pass --platform or the env var.
func resolvePlatformURL(override, gatewayBase string) (string, error) {
	candidate := strings.TrimSpace(override)
	if candidate == "" {
		candidate = strings.TrimSpace(os.Getenv("MUHIYACODE_PLATFORM_URL"))
	}
	if candidate == "" {
		candidate = derivePlatformFromGateway(gatewayBase)
	}
	if candidate == "" {
		return "", fmt.Errorf("could not determine the Muhiya platform URL; pass --platform https://…")
	}
	if !strings.HasPrefix(candidate, "http://") && !strings.HasPrefix(candidate, "https://") {
		candidate = "https://" + candidate
	}
	return strings.TrimRight(candidate, "/"), nil
}

func derivePlatformFromGateway(gatewayBase string) string {
	u, err := url.Parse(strings.TrimSpace(gatewayBase))
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Port() == "8090" || strings.Contains(u.Host, "localhost:8090") || strings.Contains(u.Host, "127.0.0.1:8090") {
		return "http://localhost:3000"
	}
	host := strings.TrimPrefix(u.Host, "api.")
	return u.Scheme + "://" + host
}

func exchangeToken(ctx context.Context, exchange tokenExchangeRequest) (token, baseURL, email, userID string, err error) {
	payload, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"code":          exchange.code,
		"code_verifier": exchange.verifier,
		"redirect_uri":  exchange.redirectURI,
		"client_id":     oauthClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchange.platformURL+"/api/oauth/token", bytes.NewReader(payload))
	if err != nil {
		return "", "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", "", fmt.Errorf("contact platform: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error != "" {
			return "", "", "", "", fmt.Errorf("token exchange failed: %s", e.Error)
		}
		return "", "", "", "", fmt.Errorf("token exchange failed (HTTP %d)", resp.StatusCode)
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		BaseURL     string `json:"base_url"`
		Email       string `json:"email"`
		UserID      string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(tokenResponse.AccessToken) == "" {
		return "", "", "", "", fmt.Errorf("platform returned no access token")
	}
	if strings.TrimSpace(tokenResponse.UserID) == "" {
		return "", "", "", "", fmt.Errorf("platform returned no user identity")
	}
	return tokenResponse.AccessToken, tokenResponse.BaseURL, tokenResponse.Email, tokenResponse.UserID, nil
}

func randomURLToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeLoginHTML(w http.ResponseWriter, success bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title, body := "MuhiyaCode signed in", "You're signed in. Return to your terminal."
	if !success {
		title, body = "MuhiyaCode sign-in failed", "Sign-in was cancelled or failed. Return to your terminal and try again."
	}
	_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>`+title+
		`</title><style>body{font-family:system-ui;background:#0b0b0c;color:#f4f4f5;display:grid;place-items:center;min-height:100vh;margin:0}main{max-width:520px;padding:32px;text-align:center}h1{color:#22c55e}p{color:#a1a1aa}</style></head><body><main><h1>`+
		title+`</h1><p>`+body+`</p></main></body></html>`)
}
