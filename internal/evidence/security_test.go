package evidence

import (
	"strings"
	"testing"
	"time"
)

func TestFetchEnforcesOwnershipRangeExpiryHashAndSecretBlock(t *testing.T) {
	store, _ := NewStore(t.TempDir(), 4096)
	metadata, _ := store.Put(PutInput{SessionID: "s1", WorkspaceID: "w1", Source: "read", Complete: true, Content: []byte("0123456789")})
	if _, _, err := store.Fetch(metadata.Handle, "s2", "w1", 0, 2); err == nil {
		t.Fatal("cross-session fetch succeeded")
	}
	if _, _, err := store.Fetch(metadata.Handle, "s1", "w2", 0, 2); err == nil {
		t.Fatal("cross-workspace fetch succeeded")
	}
	got, _, err := store.Fetch(metadata.Handle, "s1", "w1", 2, 4)
	if err != nil || string(got) != "2345" {
		t.Fatalf("range=%q err=%v", got, err)
	}
	if _, _, err := store.Fetch(metadata.Handle, "s1", "w1", -1, 2); err == nil {
		t.Fatal("invalid range accepted")
	}
	expired := time.Now().Add(-time.Minute)
	expiredMeta, _ := store.Put(PutInput{SessionID: "s1", WorkspaceID: "w1", ExpiresAt: &expired, Content: []byte("old")})
	if _, _, err := store.Fetch(expiredMeta.Handle, "s1", "w1", 0, 3); err == nil {
		t.Fatal("expired handle accepted")
	}
	secret, _ := store.Put(PutInput{SessionID: "s1", WorkspaceID: "w1", Content: []byte("API_KEY=sk-abcdefghijklmnop")})
	if _, _, err := store.Fetch(secret.Handle, "s1", "w1", 0, 100); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret fetch err=%v", err)
	}
}
