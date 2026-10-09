// Package tracker links a profile's accounts on Trakt and Simkl, each through the app an admin
// registered on it, by the device flow both offer (RFC 8628): the profile is shown a code to enter
// on the tracker's site, and the server asks the tracker after it until it is entered.
package tracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// ErrRefused is a link that cannot start: a tracker an admin has not set up, or set up with a
// client id it does not know, or one the profile has linked already.
var ErrRefused = errors.New("link refused")

const (
	// followEvery is how often a node looks for codes due to be asked after; each is asked at
	// most once each interval its tracker set, whichever nodes look.
	followEvery = time.Second
	// slowDownBy is how much less often a tracker that asks to be asked less often is asked, as
	// RFC 8628 says.
	slowDownBy = 5 * time.Second
)

// What a tracker answers a code being asked after, short of tokens.
var (
	errPending  = errors.New("the code has not been entered")
	errSlowDown = errors.New("the tracker was asked too often")
	// errGone is a code that will never be entered now: expired, denied or used already.
	errGone = errors.New("the code is expired, denied or used")
)

// service is how a tracker is asked, through the app registered as clientID.
type service interface {
	code(ctx context.Context, clientID string) (kv.TrackerLink, error)
	// token answers what the tracker granted once the code is entered, and errPending,
	// errSlowDown or errGone until then.
	token(ctx context.Context, clientID, deviceCode string) (store.TrackerTokens, error)
	username(ctx context.Context, clientID, access string) (string, error)
	revoke(ctx context.Context, clientID string, tok store.TrackerTokens) error
	// refresh answers new tokens for old, or errGrantGone for a grant the tracker no longer has.
	refresh(ctx context.Context, clientID string, old store.TrackerTokens) (store.TrackerTokens, error)
	// scrobble tells the tracker what a player did. A play it has counted already, or that has
	// hardly begun, is no fault.
	scrobble(ctx context.Context, clientID, access string, a action, p play) error
	history(ctx context.Context, clientID, access string, w historyWrite, h history) error
}

type Links struct {
	st       *store.Store
	kv       *kv.KV
	raise    func(context.Context, domain.Event)
	log      *slog.Logger
	services map[domain.Tracker]service
	plays    chan domain.Event
}

// New names the server to each tracker as version of Photon.
func New(st *store.Store, k *kv.KV, raise func(context.Context, domain.Event), version string, log *slog.Logger) *Links {
	return &Links{
		st: st, kv: k, raise: raise, log: log, plays: make(chan domain.Event, playsHeld),
		services: map[domain.Tracker]service{domain.TrackerTrakt: newTrakt(version), domain.TrackerSimkl: newSimkl(version)},
	}
}

// State is how far a profile is from an account on a tracker.
type State string

const (
	// StateUnavailable is a tracker an admin has not set up.
	StateUnavailable State = "unavailable"
	StateUnlinked    State = "unlinked"
	// StateLinking is a code waiting to be entered.
	StateLinking State = "linking"
	StateLinked  State = "linked"
)

func States() []State {
	return []State{StateUnavailable, StateUnlinked, StateLinking, StateLinked}
}

// Status is a profile's account on a tracker, or the code it has to enter for one.
type Status struct {
	Tracker domain.Tracker
	State   State
	// Account is the account linked.
	Account domain.TrackerAccount
	// Link is the code to enter, without its device code.
	Link kv.TrackerLink
}

// Trackers answers how far a profile is from an account on each tracker.
func (l *Links) Trackers(ctx context.Context, profile uuid.UUID) ([]Status, error) {
	clients, err := l.st.TrackerClients(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := l.st.TrackerGrants(ctx, profile)
	if err != nil {
		return nil, err
	}
	linked := map[domain.Tracker]domain.TrackerAccount{}
	for _, g := range grants {
		linked[g.Tracker] = g.TrackerAccount
	}
	out := make([]Status, 0, len(domain.Trackers()))
	for _, t := range domain.Trackers() {
		s := Status{Tracker: t, State: StateUnlinked}
		if a, ok := linked[t]; ok {
			s.State, s.Account = StateLinked, a
		} else if clients[t] == "" {
			s.State = StateUnavailable
		} else if link, ok, err := l.kv.TrackerLink(ctx, profile, t); err != nil {
			return nil, err
		} else if ok {
			link.DeviceCode = ""
			s.State, s.Link = StateLinking, link
		}
		out = append(out, s)
	}
	return out, nil
}

// Link asks a tracker for a code for a profile to enter, in place of any it was given before.
func (l *Links) Link(ctx context.Context, profile uuid.UUID, t domain.Tracker) (kv.TrackerLink, error) {
	clientID, err := l.clientID(ctx, t)
	if err != nil {
		return kv.TrackerLink{}, err
	}
	grants, err := l.st.TrackerGrants(ctx, profile)
	if err != nil {
		return kv.TrackerLink{}, err
	}
	for _, a := range grants {
		if a.Tracker == t {
			return kv.TrackerLink{}, fmt.Errorf("%w: the profile has linked %s already, as %s: unlink it first", ErrRefused, t, a.Username)
		}
	}
	link, err := l.services[t].code(ctx, clientID)
	if refusal, ok := errors.AsType[*provider.Refusal](err); ok && (refusal.Code == http.StatusUnauthorized || refusal.Code == http.StatusForbidden) {
		return kv.TrackerLink{}, fmt.Errorf("%w: %s refused the client id: an admin checks it is the id of an app registered there", ErrRefused, t)
	}
	if err != nil {
		return kv.TrackerLink{}, err
	}
	link.Profile, link.Tracker = profile, t
	if err := l.kv.StartTrackerLink(ctx, link); err != nil {
		return kv.TrackerLink{}, err
	}
	link.DeviceCode = ""
	return link, nil
}

// Unlink forgets a profile's account on a tracker, or the code it was entering, and tells the
// tracker to forget what it granted. ErrNotFound for neither.
func (l *Links) Unlink(ctx context.Context, profile uuid.UUID, t domain.Tracker) error {
	linking, err := l.kv.EndTrackerLink(ctx, profile, t)
	if err != nil {
		return err
	}
	tok, err := l.st.UnlinkTracker(ctx, profile, t)
	if errors.Is(err, store.ErrNotFound) && linking {
		return nil
	}
	if err != nil {
		return err
	}
	if clientID, err := l.clientID(ctx, t); err == nil {
		l.revoke(ctx, t, clientID, tok)
	}
	return nil
}

// Run asks trackers after the codes profiles are entering, tells them of the plays this node took
// and pushes what profiles marked watched or unwatched, until ctx ends. Plays still held then are
// not told; a play that ended watched is pushed all the same.
func (l *Links) Run(ctx context.Context) {
	tick := time.NewTicker(followEvery)
	defer tick.Stop()
	pushes := time.NewTicker(pushEvery)
	defer pushes.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-pushes.C:
			if err := l.push(ctx); err != nil && ctx.Err() == nil {
				l.log.WarnContext(ctx, "what profiles watched not pushed to trackers", slog.Any("err", err))
			}
		case <-tick.C:
			if err := l.follow(ctx); err != nil && ctx.Err() == nil {
				l.log.WarnContext(ctx, "codes entered on trackers not asked after", slog.Any("err", err))
			}
		case e := <-l.plays:
			if err := l.scrobble(ctx, e); err != nil && ctx.Err() == nil {
				l.log.WarnContext(ctx, "a play not told to trackers", slog.Any("err", err))
			}
		}
	}
}

func (l *Links) follow(ctx context.Context) error {
	due, err := l.kv.DueTrackerLinks(ctx)
	if err != nil || len(due) == 0 {
		return err
	}
	clients, err := l.st.TrackerClients(ctx)
	if err != nil {
		return err
	}
	for _, link := range due {
		if err := l.ask(ctx, link, clients[link.Tracker]); err != nil {
			l.log.WarnContext(ctx, "could not ask a tracker after a code", slog.String("tracker", string(link.Tracker)), slog.Any("err", err))
		}
	}
	return nil
}

// ask asks a tracker whether a code was entered, and keeps the account once it is. A tracker that
// cannot be asked is asked again next interval, until the code expires.
func (l *Links) ask(ctx context.Context, link kv.TrackerLink, clientID string) error {
	if clientID == "" {
		// An admin took the tracker away while the code waited.
		return l.end(ctx, link)
	}
	svc := l.services[link.Tracker]
	tok, err := svc.token(ctx, clientID, link.DeviceCode)
	switch {
	case errors.Is(err, errPending):
		return nil
	case errors.Is(err, errSlowDown):
		return l.kv.SlowTrackerLink(ctx, link.Profile, link.Tracker, slowDownBy)
	case errors.Is(err, errGone):
		return l.end(ctx, link)
	case err != nil:
		return err
	}
	username, err := svc.username(ctx, clientID, tok.Access)
	if err == nil {
		err = l.st.LinkTracker(ctx, link.Profile, link.Tracker, username, tok)
	}
	if err != nil {
		// The code is spent, so the profile starts again.
		l.revoke(ctx, link.Tracker, clientID, tok)
		return errors.Join(err, l.end(ctx, link))
	}
	return l.end(ctx, link)
}

// end forgets a link and tells its profile what came of it.
func (l *Links) end(ctx context.Context, link kv.TrackerLink) error {
	if _, err := l.kv.EndTrackerLink(ctx, link.Profile, link.Tracker); err != nil {
		return err
	}
	l.raise(ctx, domain.Event{Kind: domain.EventTrackerChanged, Profile: link.Profile, Details: domain.TrackerDetails{Tracker: link.Tracker}})
	return nil
}

// revoke tells a tracker to forget what it granted. One that will not is left for the account's
// owner to remove on the tracker's site.
func (l *Links) revoke(ctx context.Context, t domain.Tracker, clientID string, tok store.TrackerTokens) {
	if err := l.services[t].revoke(ctx, clientID, tok); err != nil {
		l.log.WarnContext(ctx, "could not revoke a tracker's grant", slog.String("tracker", string(t)), slog.Any("err", err))
	}
}

func (l *Links) clientID(ctx context.Context, t domain.Tracker) (string, error) {
	clients, err := l.st.TrackerClients(ctx)
	if err != nil {
		return "", err
	}
	if clients[t] == "" {
		return "", fmt.Errorf("%w: %s is not set up: an admin sets its client id", ErrRefused, t)
	}
	return clients[t], nil
}
