package playback

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type placing interface {
	Self() domain.Node
	Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayCopy, o Opening) error
}

type starting interface {
	Start(ctx context.Context, id uuid.UUID, method domain.PlayMethod, card domain.PlaybackCard, start time.Duration, node uuid.UUID) (domain.Playback, error)
	Abandon(ctx context.Context, id uuid.UUID) error
	Opened(ctx context.Context, p domain.Playback)
}

// Start is a playback a session asks for: its id, the copy it plays, the tracks asked for, and
// the remux a node is asked to open, whose video and audio are as the playback is decided.
type Start struct {
	ID      uuid.UUID
	Session domain.Session
	Address string
	Title   domain.PlaybackTitle
	Copy    store.PlayCopy
	Tracks  domain.ChosenTracks
	Opening Opening
}

// Place starts s on the first of nodes to take it, asking each in turn how decide plays it with
// what that node encodes; one that encodes nothing is played on this node. A node with every slot
// held refuses it, and the next is asked; one that would be encoded where nodes are none, or that
// every node refused, is hls.ErrTranscodeLimit. A playback started already under s.ID is
// ErrStarted.
func Place(ctx context.Context, p placing, sessions starting, nodes []domain.Node, s Start, decide func(Encoding) (Decision, error)) (Decision, domain.Playback, error) {
	asked := nodes
	if len(asked) == 0 {
		asked = []domain.Node{p.Self()}
	}
	var d Decision
	var started domain.Playback
	var err error
	for _, node := range asked {
		if d, err = decide(EncodingOf(node)); err != nil {
			return d, started, err
		}
		if d.Video == nil || d.Video.Encode == nil {
			node = p.Self()
		} else if len(nodes) == 0 {
			return d, started, hls.ErrTranscodeLimit
		}
		started, err = start(ctx, p, sessions, node, s, d)
		if !errors.Is(err, hls.ErrTranscodeLimit) {
			break
		}
	}
	return d, started, err
}

// start starts s on node, and its remux there where it is not played directly; a remux the node
// refuses is abandoned, and the playback with it.
func start(ctx context.Context, p placing, sessions starting, node domain.Node, s Start, d Decision) (domain.Playback, error) {
	card := Card(s.Session, s.Address, s.Title, s.Copy, d, s.Tracks)
	if d.Video != nil {
		card.Acceleration = hls.EncodedOn(node.Encoder.Acceleration, *d.Video)
	}
	started, err := sessions.Start(ctx, s.ID, d.Method, card, time.Duration(s.Opening.StartMS)*time.Millisecond, node.ID)
	if err != nil {
		return started, err
	}
	if d.Method != domain.PlayDirect {
		o := s.Opening
		o.Video, o.Audio = *d.Video, d.Audio
		if err := p.Open(ctx, node, s.ID, s.Copy, o); err != nil {
			// One not abandoned holds its id, so no other node is asked.
			if aerr := sessions.Abandon(context.WithoutCancel(ctx), s.ID); aerr != nil {
				return started, fmt.Errorf("abandoning a playback node %s did not open: %w", node.Name, aerr)
			}
			return started, err
		}
	}
	sessions.Opened(ctx, started)
	return started, nil
}
