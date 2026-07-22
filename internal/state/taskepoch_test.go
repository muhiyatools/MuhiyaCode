package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskEpochAtomicLegacyAndCorruptRecovery(t *testing.T) {
	paths := testPaths(t)
	db, err := Open(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := &Sessions{DB: db}
	session, err := sessions.New(context.Background(), t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	var absent map[string]any
	if ok, err := sessions.ReadTaskEpochLedger(session.ID, &absent); err != nil || ok {
		t.Fatalf("legacy ok=%v err=%v", ok, err)
	}
	ledger := map[string]any{"version": 1, "activeId": "e1", "records": []any{map[string]any{"id": "e1", "state": "open"}}}
	if err := sessions.WriteTaskEpochLedger(session.ID, ledger); err != nil {
		t.Fatal(err)
	}
	var loaded map[string]any
	if ok, err := sessions.ReadTaskEpochLedger(session.ID, &loaded); err != nil || !ok || loaded["activeId"] != "e1" {
		t.Fatalf("loaded=%v ok=%v err=%v", loaded, ok, err)
	}
	dir, _ := SessionDir(paths, session.ID)
	if err := os.WriteFile(filepath.Join(dir, "task_epochs.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := sessions.ReadTaskEpochLedger(session.ID, &loaded); err != nil || ok {
		t.Fatalf("corrupt ok=%v err=%v", ok, err)
	}
}

func TestCapsuleImmutableRoundTripAndIndexRebuild(t *testing.T) {
	paths := testPaths(t)
	db, err := Open(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := &Sessions{DB: db}
	session, err := sessions.New(context.Background(), t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	value := map[string]any{"id": id, "value": "one"}
	if err := sessions.WriteCapsule(session.ID, id, value); err != nil {
		t.Fatal(err)
	}
	if err := sessions.WriteCapsule(session.ID, id, value); err != nil {
		t.Fatal(err)
	}
	if err := sessions.WriteCapsule(session.ID, id, map[string]any{"id": id, "value": "two"}); err == nil {
		t.Fatal("immutable overwrite accepted")
	}
	rows, err := sessions.ReadCapsules(session.ID)
	if err != nil || len(rows) != 1 || !json.Valid(rows[0]) {
		t.Fatalf("rows=%q err=%v", rows, err)
	}
	dir, _ := SessionDir(paths, session.ID)
	_ = os.Remove(filepath.Join(dir, "capsules", "index.json"))
	if err := sessions.rebuildCapsuleIndex(session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestTaskEpochPrecommitKeepsOldActiveUntilFinalCommit(t *testing.T) {
	paths := testPaths(t)
	db, err := Open(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := &Sessions{DB: db}
	session, err := sessions.New(context.Background(), t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	precommit := map[string]any{"version": 1, "activeId": "old", "records": []any{
		map[string]any{"id": "old", "state": "settled"}, map[string]any{"id": "new", "state": "open"}}}
	if err := sessions.WriteTaskEpochLedger(session.ID, precommit); err != nil {
		t.Fatal(err)
	}
	var loaded struct {
		Version  int              `json:"version"`
		ActiveID string           `json:"activeId"`
		Records  []map[string]any `json:"records"`
	}
	if ok, err := sessions.ReadTaskEpochLedger(session.ID, &loaded); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if loaded.ActiveID != "old" || len(loaded.Records) != 2 {
		t.Fatalf("precommit=%+v", loaded)
	}
	precommit["activeId"] = "new"
	if err := sessions.WriteTaskEpochLedger(session.ID, precommit); err != nil {
		t.Fatal(err)
	}
	if ok, err := sessions.ReadTaskEpochLedger(session.ID, &loaded); err != nil || !ok || loaded.ActiveID != "new" {
		t.Fatalf("final=%+v ok=%v err=%v", loaded, ok, err)
	}
}
