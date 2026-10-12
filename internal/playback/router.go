package playback

import (
	"context"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type nodes interface {
	Playback(ctx context.Context, id uuid.UUID) (domain.Playback, bool, error)
	Node(ctx context.Context, id uuid.UUID) (domain.Node, bool, error)
}

// Router says which node of the cluster serves a playback's HLS: the one that opened it, as only
// it holds what its ffmpeg makes.
type Router struct {
	nodes nodes
	self  uuid.UUID
}

func NewRouter(n nodes, self uuid.UUID) *Router { return &Router{nodes: n, self: self} }

// Owner answers the node serving a playback where that is another node that is still there;
// false where it is this one, or none.
func (r *Router) Owner(ctx context.Context, playback uuid.UUID) (domain.Node, bool, error) {
	p, ok, err := r.nodes.Playback(ctx, playback)
	if err != nil || !ok || p.Node == r.self || p.Node == (uuid.UUID{}) {
		return domain.Node{}, false, err
	}
	return r.nodes.Node(ctx, p.Node)
}
