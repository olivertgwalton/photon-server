package playback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/nodecall"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type told []domain.Node

func (t told) Nodes(context.Context) ([]domain.Node, error) { return t, nil }

// A transcode is offered first to the node with the most of its slots free, a node with no limit
// before any, and only to nodes that encode what it needs and that the others can reach.
func TestTheNodeWithTheMostSlotsFreeIsAskedFirst(t *testing.T) {
	both := domain.Encoder{Acceleration: domain.AccelNVENC, HEVC: domain.HEVCAllow, Libass: true}
	h264 := domain.Encoder{Acceleration: domain.AccelSoftware, HEVC: domain.HEVCDeny}
	self := domain.Node{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "self", Encoder: h264, Transcodes: 1, Limit: 4}
	nodes := told{
		{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "half", Address: "http://half", Encoder: both, Transcodes: 4, Limit: 8},
		{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "idle", Address: "http://idle", Encoder: both, Transcodes: 0, Limit: 2},
		{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "unlimited", Address: "http://unlimited", Encoder: h264, Transcodes: 9, Limit: hls.Unlimited},
		{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "full", Address: "http://full", Encoder: both, Transcodes: 2, Limit: 2},
		{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "unreachable", Encoder: both, Limit: 8},
		{Role: domain.NodeTranscode, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "gpu", Address: "http://gpu", Encoder: both, Transcodes: 6, Limit: 8},
		{Role: domain.NodeTranscode, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "gpu, full", Address: "http://gpu-full", Encoder: both, Transcodes: 8, Limit: 8},
		{Role: domain.NodeAll, Availability: domain.NodeDraining, ID: uuid.NewV7(), Name: "drained", Address: "http://drained", Encoder: both, Limit: 8},
		{Role: domain.NodeServe, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "serves only", Address: "http://serve", Encoder: both, Limit: 8},
		// This node's own advert, older than what it knows of itself now.
		{Role: domain.NodeAll, Availability: domain.NodeActive, ID: self.ID, Name: "self, as it said", Address: "http://self", Encoder: h264, Limit: 4},
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
	want := []string{"gpu", "unlimited", "idle", "self", "half", "gpu, full", "full"}
	if got := names(Need{}); !slices.Equal(got, want) {
		t.Errorf("for H.264: %v, want %v: those with a slot free first, of them one set to transcode, then one with no limit, then by the share free; none that never encodes or is drained, none unreachable", got, want)
	}
	want = []string{"gpu", "idle", "half", "gpu, full", "full"}
	if got := names(Need{HEVC: true}); !slices.Equal(got, want) {
		t.Errorf("for HEVC: %v, want only the nodes that encode it, %v", got, want)
	}
	if got := names(Need{Libass: true}); !slices.Equal(got, want) {
		t.Errorf("for styled subtitles drawn in: %v, want only the nodes with libass, %v", got, want)
	}
}

// Of nodes otherwise equal, the one a remux was opened on is asked after the others next time,
// so they take turns.
func TestNodesOtherwiseEqualTakeTurns(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer remote.Close()
	enc := domain.Encoder{Acceleration: domain.AccelSoftware, HEVC: domain.HEVCDeny}
	self := domain.Node{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "self", Encoder: enc, Limit: hls.Unlimited}
	other := domain.Node{Role: domain.NodeAll, Availability: domain.NodeActive, ID: uuid.NewV7(), Name: "other", Address: remote.URL, Encoder: enc, Limit: hls.Unlimited}
	p := NewPlacer(told{other}, func() domain.Node { return self }, nil, nodecall.Key{})
	first := func() string {
		t.Helper()
		got, err := p.Candidates(t.Context(), Need{})
		if err != nil {
			t.Fatal(err)
		}
		return got[0].Name
	}
	n := first()
	if n != "other" {
		t.Fatalf("first asked %s, want other, before any were opened, in the order told", n)
	}
	if err := p.Open(t.Context(), other, uuid.NewV7(), store.PlayCopy{}, Opening{}); err != nil {
		t.Fatal(err)
	}
	if n := first(); n != "self" {
		t.Errorf("after opening on other, first asked %s, want self", n)
	}
}
