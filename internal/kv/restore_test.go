//go:build integration

package kv

import (
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestARestoreUnderwayLapsesUnlessItsNodeKeepsIt(t *testing.T) {
	k, err := Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	defer k.Close()
	ctx := t.Context()
	defer func() {
		if _, err := k.Clear(ctx); err != nil {
			t.Error(err)
		}
	}()
	r := domain.Restore{Dump: "photon-20261008T120000Z.dump", Node: uuid.NewV7(), Started: time.Unix(1_800_000_000, 0).UTC(), Phase: domain.RestoreStopping}
	if ok, err := k.BeginRestore(ctx, r, 30*time.Second); err != nil || !ok {
		t.Fatalf("beginning: %v %v", ok, err)
	}
	if ok, err := k.BeginRestore(ctx, r, time.Minute); err != nil || ok {
		t.Errorf("beginning a second while one is under way: %v %v, want it refused", ok, err)
	}
	if got, ok, err := k.Restoring(ctx); err != nil || !ok || got != r {
		t.Errorf("restoring = %+v %v %v, want %+v", got, ok, err, r)
	}
	// Valkey lapses it on its own clock; what is asked of it is the life it was given.
	life := func() int64 {
		ms, err := k.client.Do(ctx, k.client.B().Pttl().Key(k.key(restoreKey)).Build()).AsInt64()
		if err != nil {
			t.Fatal(err)
		}
		return ms
	}
	if ms := life(); ms <= 0 || ms > 30_000 {
		t.Errorf("life given = %d ms, want up to 30 s", ms)
	}
	if err := k.KeepRestore(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if ms := life(); ms <= 30_000 {
		t.Errorf("life once kept = %d ms, want up to a minute", ms)
	}
	if err := k.EndRestore(ctx); err != nil {
		t.Fatal(err)
	}
	if err := k.KeepRestore(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := k.Restoring(ctx); err != nil || ok {
		t.Errorf("keeping a restore that ended: %v %v, want it to stay ended", ok, err)
	}

	o := domain.RestoreOutcome{Dump: r.Dump, At: r.Started, Result: domain.RestoreFailed, Reason: "psql: exit status 3"}
	if err := k.SaveRestoreOutcome(ctx, o); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := k.LastRestore(ctx); err != nil || !ok || got != o {
		t.Errorf("last restore = %+v %v %v, want %+v", got, ok, err, o)
	}
}
