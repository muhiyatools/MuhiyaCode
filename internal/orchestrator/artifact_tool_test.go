package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/evidence"
)

func TestArtifactFetcherUsesExactBoundedAuthorizedRanges(t *testing.T) {
	store, _ := evidence.NewStore(t.TempDir(), 1<<20)
	metadata, _ := store.Put(evidence.PutInput{SessionID: "s", WorkspaceID: "w", Complete: true, Content: []byte("0123456789")})
	fetcher, _ := NewArtifactFetcher(store, "s", "w")
	got, err := fetcher.Execute(context.Background(), []byte(`{"handle":"`+metadata.Handle+`","offset":2,"limit":4}`))
	if err != nil || !strings.HasSuffix(got, "2345") {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := fetcher.Execute(context.Background(), []byte(`{"handle":"`+metadata.Handle+`","offset":0,"limit":70000}`)); err == nil {
		t.Fatal("recursive-flood-sized fetch accepted")
	}
	other, _ := NewArtifactFetcher(store, "other", "w")
	if _, err := other.Execute(context.Background(), []byte(`{"handle":"`+metadata.Handle+`","offset":0,"limit":2}`)); err == nil {
		t.Fatal("cross-session broker fetch accepted")
	}
}

func TestArtifactFetcherRunsThroughNormalRegistryIdentity(t *testing.T) {
	store, _ := evidence.NewStore(t.TempDir(), 1<<20)
	metadata, _ := store.Put(evidence.PutInput{SessionID: "s", WorkspaceID: "w", Complete: true, Content: []byte("evidence")})
	fetcher, _ := NewArtifactFetcher(store, "s", "w")
	registry := NewRegistry(fetcher)
	got, err := registry.Execute(context.Background(), "fetch_artifact", []byte(`{"handle":"`+metadata.Handle+`","offset":0,"limit":8}`), map[string]bool{"fetch_artifact": true})
	if err != nil || !strings.Contains(got, "evidence") {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
