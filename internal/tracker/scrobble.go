package tracker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// refreshBefore is how long before an access token expires it is refreshed, as Simkl advises:
	// soon enough that a play is never told with a token that lapsed.
	refreshBefore = 24 * time.Hour
	// playsHeld is how many plays a node holds to tell trackers, which it tells one at a time. More
	// are dropped while a tracker is slow to answer, rather than held without end.
	playsHeld = 256
)

// errGrantGone is a refresh a tracker refused for good: its user revoked the app, or the grant
// lapsed unused.
var errGrantGone = errors.New("the tracker no longer grants the account")

// action is what a player did, as a tracker is told it.
type action string

const (
	// actionStart is a play starting or resuming.
	actionStart action = "start"
	actionPause action = "pause"
	// actionStop is a play ending: from 80% a tracker counts it watched, and short of that keeps
	// where it stopped, as a pause.
	actionStop action = "stop"
)

// actions are what a tracker is told of a play starting, pausing, resuming and stopping.
var actions = map[domain.EventKind]action{
	domain.EventPlaybackStarted: actionStart, domain.EventPlaybackResumed: actionStart,
	domain.EventPlaybackPaused: actionPause, domain.EventPlaybackStopped: actionStop,
}

// play is a film, or a show's episode, as Trakt and Simkl both take it, and how far through it the
// player is, in percent.
type play struct {
	Progress float64   `json:"progress"`
	Movie    *titled   `json:"movie,omitzero"`
	Show     *titled   `json:"show,omitzero"`
	Episode  *numbered `json:"episode,omitzero"`
}

type titled struct {
	Title string         `json:"title"`
	Year  int            `json:"year,omitzero"`
	IDs   map[string]any `json:"ids"`
}

type numbered struct {
	Season int `json:"season"`
	Number int `json:"number"`
}

// Played takes a play starting, pausing, resuming or stopping on this node, to tell the trackers
// its profile linked. It never waits: a play the node holds too many to take is dropped.
func (l *Links) Played(e domain.Event) {
	if _, ok := actions[e.Kind]; !ok {
		return
	}
	select {
	case l.plays <- e:
	default:
		l.log.Warn("a play was not told to trackers: too many are waiting", slog.String("profile", e.Profile.String()))
	}
}

// scrobble tells each tracker the play's profile linked what its player did.
func (l *Links) scrobble(ctx context.Context, e domain.Event) error {
	a := actions[e.Kind]
	grants, err := l.st.TrackerGrants(ctx, e.Profile)
	if err != nil || len(grants) == 0 {
		return err
	}
	d, _ := e.Details.(domain.PlaybackDetails)
	p, ok, err := l.play(ctx, d)
	if err != nil || !ok {
		return err
	}
	clients, err := l.st.TrackerClients(ctx)
	if err != nil {
		return err
	}
	for _, g := range grants {
		clientID := clients[g.Tracker]
		if clientID == "" {
			continue
		}
		err := l.authorised(ctx, e.Profile, g, clientID, func(access string) error {
			return l.services[g.Tracker].scrobble(ctx, clientID, access, a, p)
		})
		// A play told watched is no longer the history push's to tell, which would tell it twice.
		if err == nil && a == actionStop && p.Progress == 100 {
			err = l.st.ForgetTrackerWatched(ctx, e.Profile, g.Tracker, e.Item)
		}
		if err != nil {
			l.log.WarnContext(ctx, "could not tell a tracker of a play", slog.String("tracker", string(g.Tracker)),
				slog.String("action", string(a)), slog.Any("err", err))
		}
	}
	return nil
}

// play is the film or episode a playback plays as trackers know it, or not ok for a title they
// know nothing of, or one removed since.
func (l *Links) play(ctx context.Context, d domain.PlaybackDetails) (play, bool, error) {
	t := d.Playback.Title
	length, err := l.st.Length(ctx, t.ID)
	if errors.Is(err, store.ErrNotFound) {
		return play{}, false, nil
	}
	if err != nil {
		return play{}, false, err
	}
	p := play{Progress: 100}
	// A play the server counts watched is told as watched, whatever share of it that was: its
	// credits may start before a tracker's 80%.
	if d.Reach != domain.ReachEnd && length > 0 {
		p.Progress = math.Round(min(float64(d.Playback.PositionMS)/float64(length.Milliseconds()), 1)*10000) / 100
	}
	by := t.ID
	switch {
	case t.Kind == domain.ItemMovie:
		p.Movie = &titled{Title: t.Title, Year: t.Year}
	case t.Kind == domain.ItemEpisode && t.SeasonNumber != nil && t.EpisodeNumber != nil:
		// An episode is told by its show's ids and its number, which every tracker matches.
		by = t.ShowID
		p.Show = &titled{Title: t.Show}
		p.Episode = &numbered{Season: *t.SeasonNumber, Number: *t.EpisodeNumber}
	default:
		return play{}, false, nil
	}
	ids, err := l.st.ExternalIDs(ctx, []uuid.UUID{by})
	if err != nil {
		return play{}, false, err
	}
	known := trackerIDs(ids[by])
	if len(known) == 0 {
		return play{}, false, nil
	}
	if p.Movie != nil {
		p.Movie.IDs = known
	} else {
		p.Show.IDs = known
	}
	return p, true, nil
}

// trackerIDs are the ids of a title every tracker matches by.
func trackerIDs(ids map[domain.Provider]string) map[string]any {
	out := map[string]any{}
	for _, provider := range []domain.Provider{domain.ProviderTMDB, domain.ProviderTVDB, domain.ProviderIMDb} {
		v, ok := ids[provider]
		if !ok {
			continue
		}
		// TMDB's and TheTVDB's ids are numbers to every tracker; IMDb's are not.
		if n, err := strconv.Atoi(v); err == nil {
			out[string(provider)] = n
		} else {
			out[string(provider)] = v
		}
	}
	return out
}

// authorised calls do with an account's access token. One the tracker refuses before it was due
// to expire is refreshed, and do called once more.
func (l *Links) authorised(ctx context.Context, profile uuid.UUID, g store.TrackerGrant, clientID string, do func(access string) error) error {
	stale := time.Now().Add(refreshBefore)
	for range 2 {
		access, err := l.access(ctx, profile, g, clientID, stale)
		if err != nil {
			return err
		}
		err = do(access)
		refusal, refused := errors.AsType[*provider.Refusal](err)
		if !refused || refusal.Code != http.StatusUnauthorized {
			return err
		}
		// Refreshed only if the token refused is still the one kept: another node may have
		// refreshed it since.
		stale = g.Expires.Add(time.Nanosecond)
	}
	return fmt.Errorf("%s refused its own refreshed token", g.Tracker)
}

// access answers an account's access token, refreshed where it expires before stale. A refresh
// the tracker refuses for good unlinks the account, and tells its profile.
func (l *Links) access(ctx context.Context, profile uuid.UUID, g store.TrackerGrant, clientID string, stale time.Time) (string, error) {
	if !g.Expires.Before(stale) {
		return g.Access, nil
	}
	svc := l.services[g.Tracker]
	tok, err := l.st.RefreshTracker(ctx, profile, g.Tracker, stale, func(ctx context.Context, old store.TrackerTokens) (store.TrackerTokens, error) {
		fresh, err := svc.refresh(ctx, clientID, old)
		// Simkl's refresh token stays the same, and may be left out of its answer.
		fresh.Refresh = cmp.Or(fresh.Refresh, old.Refresh)
		return fresh, err
	})
	if errors.Is(err, errGrantGone) {
		if _, err := l.st.UnlinkTracker(ctx, profile, g.Tracker); err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		l.raise(ctx, domain.Event{Kind: domain.EventTrackerChanged, Profile: profile, Details: domain.TrackerDetails{Tracker: g.Tracker}})
		return "", fmt.Errorf("%w, so it is unlinked", err)
	}
	return tok.Access, err
}
