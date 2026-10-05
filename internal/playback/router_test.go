package playback

import (
	"context"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type cluster struct {
	playbacks map[uuid.UUID]domain.Playback
	addresses map[uuid.UUID]string
}

func (c cluster) Playback(_ context.Context, id uuid.UUID) (domain.Playback, bool, error) {
	p, ok := c.playbacks[id]
	return p, ok, nil
}

func (c cluster) NodeAddress(_ context.Context, id uuid.UUID) (string, bool, error) {
	a, ok := c.addresses[id]
	return a, ok, nil
}

func TestAPlaybackIsServedByTheNodeThatOpenedIt(t *testing.T) {
	self, other, gone := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	mine, theirs, orphan := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	c := cluster{
		playbacks: map[uuid.UUID]domain.Playback{mine: {Node: self}, theirs: {Node: other}, orphan: {Node: gone}},
		addresses: map[uuid.UUID]string{other: "http://10.0.0.6:8640"},
	}
	r := NewRouter(c, self)
	for name, tc := range map[string]struct {
		playback  uuid.UUID
		address   string
		elsewhere bool
	}{
		"this node's":             {mine, "", false},
		"another node's":          {theirs, "http://10.0.0.6:8640", true},
		"a node gone quiet's":     {orphan, "", false},
		"a playback there is not": {uuid.NewV7(), "", false},
	} {
		address, elsewhere, err := r.Owner(t.Context(), tc.playback)
		if err != nil || address != tc.address || elsewhere != tc.elsewhere {
			t.Errorf("%s: %q, %v, %v; want %q, %v", name, address, elsewhere, err, tc.address, tc.elsewhere)
		}
	}
}
