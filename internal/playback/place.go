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
	Open(ctx context.Context, node domain.Node, playback uuid.UUID, c store.PlayVersion, o Opening) error
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
	Copy    store.PlayVersion
	Tracks  domain.ChosenTracks
	Opening Opening
}

// Place starts s on the first of nodes to take it, asking each in turn how decide plays it with
// what that node encodes; one that encodes nothing is delivered by the first of deliverers to take
// it, this node where they are none. A node with every slot held, short of space, or delivering as
// many streams as it may, refuses it, and the next is asked; one that would be encoded where nodes
// are none is hls.ErrTranscodeLimit, one every deliverer refused ErrStreamLimit, and one every
// node refused each refusal joined. A playback started already under s.ID is ErrStarted.
func Place(ctx context.Context, p placing, sessions starting, nodes, deliverers []domain.Node, s Start, decide func(Encoding) (Decision, error)) (Decision, domain.Playback, error) {
	asked := nodes
	if len(asked) == 0 {
		asked = []domain.Node{p.Self()}
	}
	var d Decision
	var started domain.Playback
	var err error
	var refusals []error
	for _, node := range asked {
		if d, err = decide(EncodingOf(node)); err != nil {
			return d, started, err
		}
		if d.Video == nil || d.Video.Encode == nil {
			if len(deliverers) == 0 {
				deliverers = []domain.Node{p.Self()}
			}
			for _, node := range deliverers {
				if started, err = start(ctx, p, sessions, node, s, d); !errors.Is(err, ErrStreamLimit) {
					break
				}
			}
			return d, started, err
		}
		if len(nodes) == 0 {
			return d, started, hls.ErrTranscodeLimit
		}
		started, err = start(ctx, p, sessions, node, s, d)
		if !refused(err) {
			return d, started, err
		}
		refusals = append(refusals, err)
	}
	return d, started, errors.Join(refusals...)
}

// refused is a node's refusal of a playback for want of room, for the next to be asked.
func refused(err error) bool {
	return errors.Is(err, hls.ErrTranscodeLimit) || errors.Is(err, ErrStreamLimit)
}

// start starts s on node, and opens it there; one the node refuses is abandoned.
func start(ctx context.Context, p placing, sessions starting, node domain.Node, s Start, d Decision) (domain.Playback, error) {
	card := Card(s.Session, s.Address, s.Title, s.Copy, d, s.Tracks)
	if d.Video != nil && d.Video.Encode != nil {
		card.Acceleration = node.Encoder.Acceleration
	}
	started, err := sessions.Start(ctx, s.ID, d.Method, card, time.Duration(s.Opening.StartMS)*time.Millisecond, node.ID)
	if err != nil {
		return started, err
	}
	o := s.Opening
	o.Method = d.Method
	if d.Method != domain.PlayDirect {
		o.Video, o.Audio = *d.Video, d.Audio
	}
	if err := p.Open(ctx, node, s.ID, s.Copy, o); err != nil {
		// One not abandoned holds its id, so no other node is asked.
		if aerr := sessions.Abandon(context.WithoutCancel(ctx), s.ID); aerr != nil {
			return started, fmt.Errorf("abandoning a playback node %s did not open: %w", node.Name, aerr)
		}
		return started, err
	}
	sessions.Opened(ctx, started)
	return started, nil
}
