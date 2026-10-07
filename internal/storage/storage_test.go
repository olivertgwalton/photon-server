//go:build integration

package storage

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type settingsAt struct {
	mu sync.Mutex
	at domain.Storage
}

func (s *settingsAt) Storage(context.Context) (domain.Storage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at, nil
}

func (s *settingsAt) set(st domain.Storage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at = st
}

// testBucket is a fresh folder of the bucket TEST_S3_ENDPOINT and TEST_S3_BUCKET name.
func testBucket(t *testing.T) domain.Bucket {
	t.Helper()
	b := domain.Bucket{
		Endpoint: os.Getenv("TEST_S3_ENDPOINT"), Name: os.Getenv("TEST_S3_BUCKET"), Folder: uuid.NewV7().String(),
		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"), SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
	}
	if b.Endpoint == "" || b.Name == "" {
		t.Fatal("TEST_S3_ENDPOINT and TEST_S3_BUCKET are unset")
	}
	return b
}

// A node keeps artwork where an admin last chose, moving there as it is told, while in use.
func TestANodeKeepsArtworkWhereItIsTold(t *testing.T) {
	settings := &settingsAt{at: domain.Storage{Kind: domain.StorageDisk}}
	stores, err := Open(t.Context(), settings, t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	ctx := t.Context()
	if empty, err := stores.Empty(ctx); !empty || err != nil {
		t.Fatalf("a new node's disk empty = %v, %v", empty, err)
	}
	if err := stores.Artwork.Put(ctx, "on-disk", strings.NewReader("poster")); err != nil {
		t.Fatal(err)
	}
	if empty, _ := stores.Empty(ctx); empty {
		t.Error("a disk holding a poster is said to be empty")
	}

	events := make(chan domain.Event)
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		stores.Run(run, func() (<-chan domain.Event, func()) { return events, func() {} })
	}()
	defer func() { stop(); <-done }()

	settings.set(domain.Storage{Kind: domain.StorageBucket, Bucket: testBucket(t)})
	events <- domain.Event{Kind: domain.EventStorageChanged}
	// The event is taken once the node is ready for another, by when it has moved.
	events <- domain.Event{Kind: domain.EventLibraryChanged}
	if empty, err := stores.Empty(ctx); !empty || err != nil {
		t.Errorf("a fresh bucket empty = %v, %v; want the node keeping artwork there", empty, err)
	}
	if err := stores.Artwork.Put(ctx, "in-bucket", strings.NewReader("poster")); err != nil {
		t.Fatal(err)
	}
	if ok, err := stores.Artwork.Exists(ctx, "in-bucket"); !ok || err != nil {
		t.Errorf("a poster put after the move is not in the bucket: %v, %v", ok, err)
	}
	if ok, _ := stores.Artwork.Exists(ctx, "on-disk"); ok {
		t.Error("the bucket holds what was kept on disk before")
	}
	_ = stores.Artwork.Delete(context.Background(), "in-bucket")
}

func TestABucketIsCheckedBeforeItIsChosen(t *testing.T) {
	stores := &Stores{}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := stores.Check(ctx, testBucket(t)); err != nil {
		t.Errorf("a bucket the server can use: %v", err)
	}
	wrong := testBucket(t)
	wrong.SecretKey = "wrong"
	if err := stores.Check(ctx, wrong); err == nil {
		t.Error("a bucket the server cannot sign for passed its check")
	}
}
