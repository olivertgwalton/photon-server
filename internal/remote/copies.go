package remote

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"
	"uuid"

	"golang.org/x/sync/singleflight"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// maxProbes is how many of what a provider offers of a title are read, best first, before it
	// is found to have nothing to play: a link may be dead, as Remux falls over to the next.
	maxProbes = 3
	// minCopy is the shortest copy kept: an addon answers a stream it could not get with a clip
	// saying so in its place.
	minCopy = time.Minute
)

// ErrNoCopy is a remote film or episode none of whose offers could be read.
var ErrNoCopy = errors.New("remote: nothing the provider offers of the title could be read")

type copyStore interface {
	RemoteTitleOf(ctx context.Context, item uuid.UUID) (store.RemoteTitle, bool, error)
	Offered(ctx context.Context, item uuid.UUID, offered [][]byte) ([][]byte, error)
	SaveCopy(ctx context.Context, lib, item uuid.UUID, c store.Copy) error
}

type prober interface {
	Probe(ctx context.Context, in media.Input) (domain.Facts, error)
}

// Copies give a remote library's films and episodes the copies their provider offers, as they are
// played: Remux's model, where a title is kept and its streams are not.
type Copies struct {
	store  copyStore
	offers *Offers
	prober prober
	group  singleflight.Group
}

func NewCopies(st copyStore, offers *Offers, p prober) *Copies {
	return &Copies{store: st, offers: offers, prober: p}
}

// Ensure has a remote film or episode hold the copies its provider offers now: a copy no longer
// offered is missing, and, where none it holds is offered, the best offer that can be read is
// read for what it is and kept as a version. Anything else is left as it is.
func (c *Copies) Ensure(ctx context.Context, item uuid.UUID) error {
	_, err, _ := c.group.Do(item.String(), func() (any, error) { return nil, c.ensure(context.WithoutCancel(ctx), item) })
	return err
}

func (c *Copies) ensure(ctx context.Context, item uuid.UUID) error {
	t, ok, err := c.store.RemoteTitleOf(ctx, item)
	if err != nil || !ok {
		return err
	}
	offers, err := c.offers.Of(ctx, t.Source, t.Title)
	if err != nil {
		return err
	}
	fingerprints := make([][]byte, len(offers))
	for i, o := range offers {
		fingerprints[i] = o.Fingerprint(t.Source)
	}
	live, err := c.store.Offered(ctx, item, fingerprints)
	if err != nil || len(live) > 0 {
		return err
	}
	for i, o := range offers[:min(len(offers), maxProbes)] {
		facts, err := c.read(ctx, o)
		if err != nil {
			continue
		}
		facts.Size = cmp.Or(facts.Size, o.Size)
		return c.store.SaveCopy(ctx, t.Library, item, store.Copy{
			ContentKey: fingerprints[i],
			Parts:      []store.Part{{RelPath: Rel(t.Source, o), Size: facts.Size, ModTime: time.Unix(0, 0), Facts: &facts}},
		})
	}
	return ErrNoCopy
}

// read probes what an offer is, refusing a clip too short to be the title.
func (c *Copies) read(ctx context.Context, o domain.Offer) (domain.Facts, error) {
	in, err := media.Relayed(o.URL, o.From, o.Name)
	if err != nil {
		return domain.Facts{}, err
	}
	defer in.Close()
	facts, err := c.prober.Probe(ctx, in)
	if err == nil && facts.Duration < minCopy || err == nil && !slices.ContainsFunc(facts.Streams, func(s domain.Stream) bool { return s.Kind == domain.StreamVideo }) {
		return facts, ErrNoCopy
	}
	return facts, err
}
