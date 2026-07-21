package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TM01: offline request-byte accounting for the token-efficiency work.
//
// Bytes, never "tokens": the scripted provider has no tokenizer, so these are
// wire bytes — the honest offline proxy (Constitution VI). A change that cuts
// bytes cuts tokens monotonically for a fixed tokenizer, which is all the
// before/after comparison needs.

// requestAccount is one scenario's wire cost.
type requestAccount struct {
	Requests   int
	TotalBytes int
	PerRequest []int
	// MainBytes / SubBytes split the total by routing pin so a change can be
	// attributed to the expensive main stream or the executor stream.
	MainBytes int
	SubBytes  int
}

// requestBytes sums the marshaled wire payload (tools + messages + model) of
// every request the scripted provider recorded.
func requestBytes(provider *scriptedProvider) requestAccount {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	account := requestAccount{Requests: len(provider.requests)}
	for _, request := range provider.requests {
		payload, _ := json.Marshal(struct {
			Model    string                    `json:"model"`
			Tools    []contract.ToolDefinition `json:"tools"`
			Messages []contract.Message        `json:"messages"`
		}{request.ModelID, request.Tools, request.Messages})
		size := len(payload)
		account.TotalBytes += size
		account.PerRequest = append(account.PerRequest, size)
		if strings.Contains(request.SessionID, ":sub:") {
			account.SubBytes += size
		} else {
			account.MainBytes += size
		}
	}
	return account
}

// logAccount records a scenario's cost in the test log. Numbers are reported,
// never asserted, until a task explicitly pins a ceiling — the plan's before/
// after table is assembled from these lines.
func logAccount(t *testing.T, label string, account requestAccount) {
	t.Helper()
	t.Logf("BYTES %s: requests=%d total=%d main=%d sub=%d per-request=%v",
		label, account.Requests, account.TotalBytes, account.MainBytes, account.SubBytes, account.PerRequest)
}

// TestRequestBytesAccountsRecordedRequests is the helper's own guard: it must
// count every recorded request and split main from executor traffic.
func TestRequestBytesAccountsRecordedRequests(t *testing.T) {
	provider := &scriptedProvider{}
	provider.requests = []contract.ChatRequest{
		{SessionID: "s:main", ModelID: "m", Messages: []contract.Message{{Role: contract.RoleUser, Content: "hello"}}},
		{SessionID: "s:sub:general", ModelID: "m", Messages: []contract.Message{{Role: contract.RoleUser, Content: "a much longer executor assignment"}}},
	}
	account := requestBytes(provider)
	if account.Requests != 2 || len(account.PerRequest) != 2 {
		t.Fatalf("expected 2 accounted requests, got %+v", account)
	}
	if account.MainBytes <= 0 || account.SubBytes <= 0 {
		t.Fatalf("main/sub split not populated: %+v", account)
	}
	if account.MainBytes+account.SubBytes != account.TotalBytes {
		t.Fatalf("split does not sum to total: %+v", account)
	}
	if account.SubBytes <= account.MainBytes {
		t.Fatalf("the longer executor request should weigh more: %+v", account)
	}
}
