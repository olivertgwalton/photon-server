package playback

import (
	"context"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
)

type told []domain.Node

func (t told) Nodes(context.Context) ([]domain.Node, error) { return t, nil }

// A transcode is offered first to the node with the most of its slots free, a node with no limit
// before any, and only to nodes that encode what it needs and that the others can reach.
func TestTheNodeWithTheMostSlotsFreeIsAskedFirst(t *testing.T) {
	both := domain.Encoder{Acceleration: domain.AccelNVENC, HEVC: domain.HEVCAllow, Libass: true}
	h264 := domain.Encoder{Acceleration: domain.AccelSoftware, HEVC: domain.HEVCDeny}
	self := domain.Node{ID: uuid.NewV7(), Name: "self", Encoder: h264, Transcodes: 1, Limit: 4}
	nodes := told{
		{ID: uuid.NewV7(), Name: "half", Address: "http://half", Encoder: both, Transcodes: 4, Limit: 8},
		{ID: uuid.NewV7(), Name: "idle", Address: "http://idle", Encoder: both, Transcodes: 0, Limit: 2},
		{ID: uuid.NewV7(), Name: "unlimited", Address: "http://unlimited", Encoder: h264, Transcodes: 9, Limit: hls.Unlimited},
		{ID: uuid.NewV7(), Name: "full", Address: "http://full", Encoder: both, Transcodes: 2, Limit: 2},
		{ID: uuid.NewV7(), Name: "unreachable", Encoder: both, Limit: 8},
		// This node's own advert, older than what it knows of itself now.
		{ID: self.ID, Name: "self, as it said", Address: "http://self", Encoder: h264, Limit: 4},
	}
	p := NewPlacer(nodes, func() domain.Node { return self }, nil, nodecall.Key{})
	names := func(need Need) []string {
		t.Helper()
		got, err := p.Candidates(t.Context(), need)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, n := range got {
			out = append(out, n.Name)
		}
		return out
	}
	if got := names(Need{}); !slices.Equal(got[:2], []string{"unlimited", "idle"}) || !slices.Equal(got[2:], []string{"self", "half", "full"}) {
		t.Errorf("for H.264: %v, want the unlimited and idle first, then by the share free, the full last, none unreachable", got)
	}
	if got := names(Need{HEVC: true}); !slices.Equal(got, []string{"idle", "half", "full"}) {
		t.Errorf("for HEVC: %v, want only the nodes that encode it", got)
	}
	if got := names(Need{Libass: true}); !slices.Equal(got, []string{"idle", "half", "full"}) {
		t.Errorf("for styled subtitles drawn in: %v, want only the nodes with libass", got)
	}
}
