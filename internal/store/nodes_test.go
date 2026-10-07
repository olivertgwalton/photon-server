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
	if err != nil || n.Role != domain.NodeAll || n.LimitSource != domain.LimitAutomatic || n.Name != "mini" {
		t.Fatalf("a new node: %+v, %v; want all, its limit worked out", n, err)
	}
	set := domain.NodeSettings{Role: domain.NodeTranscode, LimitSource: domain.LimitSet, Limit: 12}
	if err := s.SetNodeSettings(ctx, id, set); err != nil {
		t.Fatal(err)
	}
	if n, err := s.JoinNode(ctx, id, "mini-2"); err != nil || n.NodeSettings != set || n.Name != "mini-2" {
		t.Errorf("starting again: %+v, %v; want %+v kept, under its new name", n, err, set)
	}
	none := domain.NodeSettings{Role: domain.NodeTranscode, LimitSource: domain.LimitSet}
	if err := s.SetNodeSettings(ctx, id, none); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Node(ctx, id); n.LimitSource != domain.LimitSet || n.Limit != 0 {
		t.Errorf("set to no limit: %+v", n)
	}
	if err := s.SetNodeSettings(ctx, uuid.NewV7(), set); !errors.Is(err, ErrNotFound) {
		t.Errorf("setting a node there has never been: %v, want ErrNotFound", err)
	}
	if err := s.SetNodeSettings(ctx, id, domain.NodeSettings{Role: "gpu", LimitSource: domain.LimitAutomatic}); err == nil {
		t.Error("a role there is not was kept")
	}
	other, _ := s.JoinNode(ctx, uuid.NewV7(), "gpu-1")
	if nodes, err := s.KnownNodes(ctx); err != nil || len(nodes) != 2 || nodes[0].ID != id || nodes[1].ID != other.ID {
		t.Errorf("known nodes: %+v, %v; want both, the first to start first", nodes, err)
	}
}
