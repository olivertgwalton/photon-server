package remote

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
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
	// readyWithin is how long a stream has to begin answering before it is taken to be fetching,
	// not dead: a debrid link already cached answers in a second or two, where a usenet release
	// being downloaded, or a torrent being cached, keeps its player waiting on the download.
	readyWithin = 15 * time.Second
)

var (
	// ErrNoCopy is a remote film or episode none of whose offers could be read.
	ErrNoCopy = errors.New("remote: nothing the provider offers of the title could be read")
	// ErrFetching is a remote film or episode whose provider is fetching the best of what it
	// offers: it plays once the provider has it.
	ErrFetching = errors.New("remote: the provider is fetching the title; it plays once it has it")
)

type copyStore interface {
	HoldDiscovered(ctx context.Context, id uuid.UUID) (bool, error)
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
	// identify matches a title, as its job does: a title a search found is matched as it is
	// opened, so its page has what its provider says of it, and its streams its other ids.
	identify func(ctx context.Context, id uuid.UUID) error
	group    singleflight.Group
	// readyWithin is the package's, a test's own shorter.
	readyWithin time.Duration
}

func NewCopies(st copyStore, offers *Offers, p prober, identify func(ctx context.Context, id uuid.UUID) error) *Copies {
	return &Copies{store: st, offers: offers, prober: p, identify: identify, readyWithin: readyWithin}
}

// Ensure has a remote film or episode hold the copies its provider offers now: a copy no longer
// offered is missing, and, where none it holds is offered, the best offer that can be read is
// read for what it is and kept as a version, or ErrFetching answered where the provider is
// fetching it. A title a search found is made one of its library's first, and matched. Anything
// else is left as it is.
func (c *Copies) Ensure(ctx context.Context, item uuid.UUID) error {
	_, err, _ := c.group.Do(item.String(), func() (any, error) { return nil, c.ensure(context.WithoutCancel(ctx), item) })
	return err
}

func (c *Copies) ensure(ctx context.Context, item uuid.UUID) error {
	held, err := c.store.HoldDiscovered(ctx, item)
	if err != nil {
		return err
	}
	if held {
		if err := c.identify(ctx, item); err != nil {
			return err
		}
	}
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
	// A stream that fails is dead, and the next is read in its place; one that does not answer in
	// time is being fetched, and the next is left alone, so opening a title fetches one at most.
	for i, o := range offers[:min(len(offers), maxProbes)] {
		facts, err := c.read(ctx, o)
		if errors.Is(err, ErrFetching) {
			return err
		}
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

// read probes what an offer is, once it answers, refusing a clip too short to be the title.
func (c *Copies) read(ctx context.Context, o domain.Offer) (domain.Facts, error) {
	in, err := media.Relayed(o.URL, o.From, o.Name)
	if err != nil {
		return domain.Facts{}, err
	}
	defer in.Close()
	if err := c.answers(ctx, in); err != nil {
		return domain.Facts{}, err
	}
	facts, err := c.prober.Probe(ctx, in)
	if err == nil && facts.Duration < minCopy || err == nil && !slices.ContainsFunc(facts.Streams, func(s domain.Stream) bool { return s.Kind == domain.StreamVideo }) {
		return facts, ErrNoCopy
	}
	return facts, err
}

// answers is whether an offer's media begins answering within readyWithin: ErrFetching where it
// does not, and why where it fails.
func (c *Copies) answers(ctx context.Context, in media.Input) error {
	ctx, cancel := context.WithTimeout(ctx, c.readyWithin)
	defer cancel()
	resp, err := media.Fetch(ctx, http.MethodGet, in.URL, http.Header{"Range": {"bytes=0-0"}})
	if ctx.Err() != nil {
		return ErrFetching
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("remote: the media answered %s", resp.Status)
	}
	return nil
}
