//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A new server keeps artwork in each node's own cache folder until an admin names a bucket.
func TestArtworkIsKeptOnDiskUntilAnAdminNamesABucket(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	if st, err := s.Storage(ctx); err != nil || st.Kind != domain.StorageDisk {
		t.Fatalf("a new server: %+v, %v; want disk", st, err)
	}
	if err := s.SetStorage(ctx, domain.Storage{Kind: domain.StorageBucket, Bucket: domain.Bucket{Delivery: domain.DeliverProxy}}); err == nil {
		t.Error("a bucket with no name was kept")
	}
	st := domain.Storage{Kind: domain.StorageBucket, Bucket: domain.Bucket{
		Endpoint: "https://s3.example.com", Name: "photon", Folder: "media", AccessKey: "key", SecretKey: "secret",
		Delivery: domain.DeliverRedirect, PublicEndpoint: "https://media.example.com",
	}}
	if err := s.SetStorage(ctx, st); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Storage(ctx); err != nil || got != st {
		t.Errorf("kept %+v, %v; want %+v", got, err, st)
	}
}

// A move's copies are each taken by one node, taken over only once one has gone quiet, and the
// move is finished, keeping things where it took them, only once every copy asked for is done.
func TestAMoveFinishesOnlyOnceEveryCopyIsDone(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	a, b := uuid.NewV7(), uuid.NewV7()
	if ok, err := s.ClaimMoveSource(ctx, a, time.Now()); ok || err != nil {
		t.Errorf("a copy claimed with no move under way: %v, %v", ok, err)
	}
	to := domain.Storage{Kind: domain.StorageBucket, Bucket: domain.Bucket{Name: "photon", Delivery: domain.DeliverProxy}}
	if err := s.StartStorageMove(ctx, to); err != nil {
		t.Fatal(err)
	}
	if err := s.StartStorageMove(ctx, to); !errors.Is(err, ErrMoving) {
		t.Errorf("a second move: %v, want ErrMoving", err)
	}
	ok, err := s.ClaimMoveSource(ctx, a, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("a copy not begun was not claimed")
	}
	ok, err = s.ClaimMoveSource(ctx, a, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a copy that said something within the minute was taken over")
	}
	ok, err = s.ClaimMoveSource(ctx, b, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("another copy was not claimed")
	}
	if err := s.ReportMoveSource(ctx, a, 3, 3, true); err != nil {
		t.Fatal(err)
	}
	ok, err = s.ClaimMoveSource(ctx, a, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("a done copy was taken again")
	}
	if done, err := s.FinishStorageMove(ctx, []uuid.UUID{a, b}); done || err != nil {
		t.Errorf("finished with one copy of two done: %v, %v", done, err)
	}
	m, moving, err := s.StorageMove(ctx)
	if !moving || err != nil || m.To != to || len(m.Sources) != 2 {
		t.Errorf("the move under way: %+v, %v, %v", m, moving, err)
	}
	if err := s.ReportMoveSource(ctx, b, 0, 0, true); err != nil {
		t.Fatal(err)
	}
	if done, err := s.FinishStorageMove(ctx, []uuid.UUID{a, b}); !done || err != nil {
		t.Errorf("not finished with every copy done: %v, %v", done, err)
	}
	got, err := s.Storage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != to {
		t.Errorf("things are kept %+v, want where the move took them", got)
	}
	_, moving, err = s.StorageMove(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if moving {
		t.Error("the move is still under way once finished")
	}
	if err := s.CancelStorageMove(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("cancelling no move: %v, want ErrNotFound", err)
	}
}
