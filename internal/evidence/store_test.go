package evidence

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestStoreCommitsContentAddressedBlobBeforeMetadataAndCollects(t *testing.T) {
	store, err := NewStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Put(PutInput{SessionID: "s", WorkspaceID: "w", Source: "test", Status: "success", Complete: true, Content: []byte("complete raw output")})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ContentHash != Hash([]byte("complete raw output")) || metadata.SizeBytes != 19 {
		t.Fatalf("metadata=%+v", metadata)
	}
	for _, path := range []string{store.blobPath(metadata.ContentHash), store.metadataPath(metadata.Handle)} {
		info, err := os.Stat(path)
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
			t.Fatalf("unsafe or missing artifact path %s: mode=%v err=%v", path, info.Mode(), err)
		}
	}
	if removed, err := store.Collect(nil); err != nil || removed != 1 {
		t.Fatalf("collect removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(store.blobPath(metadata.ContentHash)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unreferenced blob survived mark-sweep")
	}
}

func TestStoreQuotaAndDeduplicatedBlobAccounting(t *testing.T) {
	store, _ := NewStore(t.TempDir(), 10)
	input := PutInput{SessionID: "s", WorkspaceID: "w", Source: "x", Complete: true, Content: []byte("123456")}
	if _, err := store.Put(input); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(input); err != nil {
		t.Fatalf("identical content consumed quota twice: %v", err)
	}
	input.Content = []byte("abcdef")
	if _, err := store.Put(input); err == nil {
		t.Fatal("quota overflow accepted")
	}
}
