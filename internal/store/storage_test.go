//go:build integration

package store

import (
	"testing"

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
