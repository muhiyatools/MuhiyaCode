package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestProviderPreservesGatewayErrorBeforeReceiptValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid or revoked virtual key."}}`))
	}))
	defer server.Close()

	provider := providerWithReceiptModel(server.URL)
	_, err := provider.Chat(context.Background(), contract.ChatRequest{
		Messages: []contract.Message{{Role: contract.RoleUser, Content: "hello"}},
	})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v, want *HTTPError", err, err)
	}
	if httpErr.Status != http.StatusUnauthorized || !strings.Contains(httpErr.Body, "Invalid or revoked") {
		t.Fatalf("HTTP error = %+v", httpErr)
	}
}

func TestProviderStillRequiresReceiptOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := providerWithReceiptModel(server.URL)
	_, err := provider.Chat(context.Background(), contract.ChatRequest{
		Messages: []contract.Message{{Role: contract.RoleUser, Content: "hello"}},
	})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusConflict {
		t.Fatalf("error = %T %v, want receipt conflict", err, err)
	}
}

func providerWithReceiptModel(baseURL string) *OpenAICompatible {
	settings := testSettings(baseURL)
	settings.Provider.Models[0].RecordID = "model:receipt"
	settings.Provider.Models[0].TargetModel = "provider/model"
	return NewOpenAICompatible(Config{
		Settings: settings, APIKey: "sk-test", MaxRetries: 1, IdleTimeout: time.Second,
	})
}
