package mcpclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/state"
	"golang.org/x/oauth2"
)

func (m *Manager) oauthAccessToken(ctx context.Context, name string, secret map[string]any) (string, error) {
	if secret == nil {
		return "", nil
	}
	token := decodeOAuthToken(secret)
	if token == nil {
		return "", nil
	}
	if token.Valid() {
		return token.AccessToken, nil
	}
	if token.RefreshToken == "" {
		return "", fmt.Errorf("MCP OAuth token for %s expired; run `muhiyacode mcp auth %s`", name, name)
	}
	clientID, clientSecret, authStyle := oauthClient(secret)
	tokenEndpoint := stringField(secret, "tokenEndpoint", "token_endpoint")
	if tokenEndpoint == "" {
		tokenEndpoint = recursiveStringField(secret["discoveryState"], "token_endpoint", "tokenEndpoint")
	}
	if clientID == "" || tokenEndpoint == "" {
		return "", fmt.Errorf("MCP OAuth token for %s needs refresh metadata; run `muhiyacode mcp auth %s`", name, name)
	}
	clientCtx := context.WithValue(ctx, oauth2.HTTPClient, m.httpClient)
	config := oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: oauth2.Endpoint{TokenURL: tokenEndpoint, AuthStyle: authStyle}}
	refreshed, err := config.TokenSource(clientCtx, token).Token()
	if err != nil {
		return "", fmt.Errorf("refresh MCP OAuth token for %s: %w", name, err)
	}
	if err := m.saveOAuthToken(name, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func decodeOAuthToken(secret map[string]any) *oauth2.Token {
	tokens, _ := secret["tokens"].(map[string]any)
	if tokens == nil {
		tokens = secret
	}
	access := stringField(tokens, "access_token", "accessToken")
	refresh := stringField(tokens, "refresh_token", "refreshToken")
	if access == "" && refresh == "" {
		return nil
	}
	token := &oauth2.Token{AccessToken: access, RefreshToken: refresh, TokenType: stringField(tokens, "token_type", "tokenType")}
	for _, key := range []string{"expiry", "expires_at", "expiresAt"} {
		value := tokens[key]
		switch typed := value.(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				token.Expiry = parsed
			}
		case float64:
			token.Expiry = time.Unix(int64(typed), 0)
		case int64:
			token.Expiry = time.Unix(typed, 0)
		}
	}
	return token
}

func oauthClient(secret map[string]any) (id, clientSecret string, style oauth2.AuthStyle) {
	client, _ := secret["clientInformation"].(map[string]any)
	if client == nil {
		client, _ = secret["client_information"].(map[string]any)
	}
	id = stringField(client, "client_id", "clientId")
	clientSecret = stringField(client, "client_secret", "clientSecret")
	method := stringField(client, "token_endpoint_auth_method", "tokenEndpointAuthMethod")
	switch method {
	case "client_secret_post", "none":
		style = oauth2.AuthStyleInParams
	case "client_secret_basic":
		style = oauth2.AuthStyleInHeader
	default:
		style = oauth2.AuthStyleAutoDetect
	}
	return id, clientSecret, style
}

func (m *Manager) saveOAuthToken(name string, token *oauth2.Token) error {
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
	secrets.OAuth[name] = entry
	return state.SaveMCPSecrets(secrets, m.paths)
}

func encodeOAuthToken(token *oauth2.Token) map[string]any {
	value := map[string]any{"access_token": token.AccessToken, "token_type": token.TokenType}
	if token.RefreshToken != "" {
		value["refresh_token"] = token.RefreshToken
	}
	if !token.Expiry.IsZero() {
		value["expiry"] = token.Expiry.UTC().Format(time.RFC3339Nano)
	}
	return value
}

func stringField(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func recursiveStringField(value any, keys ...string) string {
	switch typed := value.(type) {
	case map[string]any:
		if found := stringField(typed, keys...); found != "" {
			return found
		}
		for _, nested := range typed {
			if found := recursiveStringField(nested, keys...); found != "" {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := recursiveStringField(nested, keys...); found != "" {
				return found
			}
		}
	}
	return ""
}
