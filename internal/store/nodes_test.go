//go:build integration

package store

import (
	"errors"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A node joining is kept as a node of all, its limit worked out, until an admin sets otherwise;
// what is set outlasts its starting again, under a new name.
func TestANodeKeepsWhatAnAdminSetsAcrossRestarts(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	id := uuid.NewV7()
	n, err := s.JoinNode(ctx, id, "mini")
	if err != nil || n.Role != domain.NodeAll || n.LimitSource != domain.LimitAutomatic || n.Name != "mini" || n.Availability != domain.NodeActive {
		t.Fatalf("a new node: %+v, %v; want all, its limit worked out, taking work", n, err)
	}
	set := domain.NodeSettings{Role: domain.NodeTranscode, LimitSource: domain.LimitSet, Limit: 12, Availability: domain.NodeDraining, Note: "driver update", Address: "http://10.0.0.5:8640"}
	if err := s.SetNodeSettings(ctx, id, set); err != nil {
		t.Fatal(err)
	}
	if n, err := s.JoinNode(ctx, id, "mini-2"); err != nil || n.NodeSettings != set || n.Name != "mini-2" {
		t.Errorf("starting again: %+v, %v; want %+v kept, under its new name", n, err, set)
	}
	none := domain.NodeSettings{Role: domain.NodeTranscode, LimitSource: domain.LimitSet, Availability: domain.NodeActive}
	if err := s.SetNodeSettings(ctx, id, none); err != nil {
		t.Fatal(err)
	}
	n, err = s.Node(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.LimitSource != domain.LimitSet || n.Limit != 0 {
		t.Errorf("set to no limit: %+v", n)
	}
	if err := s.SetNodeSettings(ctx, uuid.NewV7(), set); !errors.Is(err, ErrNotFound) {
		t.Errorf("setting a node there has never been: %v, want ErrNotFound", err)
	}
	if err := s.SetNodeSettings(ctx, id, domain.NodeSettings{Role: "gpu", LimitSource: domain.LimitAutomatic, Availability: domain.NodeActive}); err == nil {
		t.Error("a role there is not was kept")
	}
	// Seen up, its time is kept; forgotten, it is gone, and seeing it again finds none.
	before, err := s.Node(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if seen, err := s.SeeNode(ctx, id); err != nil || !seen.LastSeen.After(before.LastSeen) {
		t.Errorf("seen: %+v, %v; want its last seen later than %v", seen, err, before.LastSeen)
	}
	gone, err := s.JoinNode(ctx, uuid.NewV7(), "old")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetNode(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SeeNode(ctx, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("seeing a node forgotten: %v, want ErrNotFound", err)
	}
	other, err := s.JoinNode(ctx, uuid.NewV7(), "gpu-1")
	if err != nil {
		t.Fatal(err)
	}
	if nodes, err := s.KnownNodes(ctx); err != nil || len(nodes) != 2 || nodes[0].ID != id || nodes[1].ID != other.ID {
		t.Errorf("known nodes: %+v, %v; want both, the first to start first", nodes, err)
	}
}
