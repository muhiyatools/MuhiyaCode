package state

import (
	"context"
	"testing"
	"time"
)

func TestSessionRuntimeConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := &Sessions{DB: db}
	session, err := sessions.New(ctx, t.TempDir(), "runtime")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok, err := sessions.ReadRuntimeConfig(session.ID); err != nil || ok {
		t.Fatalf("new runtime config: ok=%v err=%v", ok, err)
	}
	want := SessionRuntimeConfig{ModelID: "minimax/minimax-m3", CacheEpoch: 7}
	if err := sessions.WriteRuntimeConfig(session.ID, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := sessions.ReadRuntimeConfig(session.ID)
	if err != nil || !ok {
		t.Fatalf("read runtime config: ok=%v err=%v", ok, err)
	}
	if got.Version != SessionRuntimeVersion || got.ModelID != want.ModelID || got.CacheEpoch != want.CacheEpoch {
		t.Fatalf("runtime config=%+v want=%+v", got, want)
	}

	forked, err := sessions.ForkAt(ctx, session.ID, "fork", 0, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	forkRuntime, ok, err := sessions.ReadRuntimeConfig(forked.ID)
	if err != nil || !ok {
		t.Fatalf("fork runtime: ok=%v err=%v", ok, err)
	}
	if forkRuntime.ModelID != want.ModelID || forkRuntime.CacheEpoch != 0 {
		t.Fatalf("fork runtime=%+v", forkRuntime)
	}
}
