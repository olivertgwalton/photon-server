// Package remote plays a remote library's films and episodes from the copies its provider streams,
// as Remux does: what the provider offers of one is asked as it is played, and kept a minute, so
// a link that expires is asked for again rather than kept; a copy is known by what its bytes are,
// so it is the same version however often it is offered.
package remote

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// offersFor is how long what a provider offers of a title is kept, as Remux keeps a title's
// streams: long enough that a play's requests, and a seek's, ask once.
const offersFor = time.Minute

// ErrGone is a copy its provider no longer offers.
var ErrGone = errors.New("remote: the provider no longer offers this copy")

type streamer interface {
	Streams(ctx context.Context, source domain.FieldSource, title domain.Streamed) ([]domain.Offer, error)
}

// Offers are what providers offer of titles, kept on this node: a link may be good only from the
// address that asked for it, so each node asks for its own.
type Offers struct {
	providers streamer
	group     singleflight.Group
	mu        sync.Mutex
	kept      map[string]kept
}

type kept struct {
	offers []domain.Offer
	at     time.Time
}

func New(providers streamer) *Offers {
	return &Offers{providers: providers, kept: map[string]kept{}}
}

// Of answers what a provider offers of a title, best first, as it offered it within offersFor.
func (o *Offers) Of(ctx context.Context, source domain.FieldSource, title domain.Streamed) ([]domain.Offer, error) {
	key := keyOf(source, title)
	o.mu.Lock()
	k, ok := o.kept[key]
	o.mu.Unlock()
	if ok && time.Since(k.at) < offersFor {
		return k.offers, nil
	}
	got, err, _ := o.group.Do(key, func() (any, error) {
		offers, err := o.providers.Streams(context.WithoutCancel(ctx), source, title)
		if err != nil {
			return nil, err
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		for k, v := range o.kept {
			if time.Since(v.at) >= offersFor {
				delete(o.kept, k)
			}
		}
		o.kept[key] = kept{offers: offers, at: time.Now()}
		return offers, nil
	})
	if err != nil {
		return nil, err
	}
	offers, _ := got.([]domain.Offer)
	return offers, nil
}

// keyOf names a title of a provider's among those kept.
func keyOf(source domain.FieldSource, title domain.Streamed) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%d|%d", source, title.Kind, title.Season, title.Episode)
	for _, p := range slices.Sorted(maps.Keys(title.IDs)) {
		fmt.Fprintf(&b, "|%s=%s", p, title.IDs[p])
	}
	return b.String()
}

// Open opens the copy a remote part is, where its provider offers it now: through the relay, so
// whatever reads it never sees its address.
func (o *Offers) Open(ctx context.Context, p domain.Place) (media.Input, error) {
	offers, err := o.Of(ctx, p.Source, p.Title)
	if err != nil {
		return media.Input{}, err
	}
	known, name, _ := strings.Cut(p.Rel, "/")
	for _, offer := range offers {
		if hex.EncodeToString(offer.Fingerprint(p.Source)) == known {
			return media.Relayed(offer.URL, offer.From, name)
		}
	}
	return media.Input{}, ErrGone
}

// Rel is where a copy a provider offers is filed among a remote library's parts: by its
// fingerprint, which finds it among what is offered again, and its name, as a player is told it.
func Rel(source domain.FieldSource, offer domain.Offer) string {
	name := strings.NewReplacer("/", "-", "\\", "-").Replace(offer.Name)
	if name == "" || name == "." || name == ".." {
		name = "stream"
	}
	return hex.EncodeToString(offer.Fingerprint(source)) + "/" + name
}
