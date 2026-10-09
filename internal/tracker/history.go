package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// pushAfter is how long a change waits to be pushed: long enough for the scrobble of a play
	// that ends watched, which tells the same watch, to be told first.
	pushAfter = 2 * time.Minute
	// pushEvery is how often a node looks for changes to push.
	pushEvery = time.Minute
	// pushLease is how long a node has to push the changes it claimed before another may.
	pushLease = 5 * time.Minute
	// pushBatch is the most changes claimed at once, each account's told in a request each way.
	pushBatch = 500
)

// historyWrite is how a tracker's history is changed.
type historyWrite string

const (
	historyAdd    historyWrite = "add"
	historyRemove historyWrite = "remove"
)

// syncHistory is the path of each historyWrite on Trakt and Simkl, the same on both.
var syncHistory = map[historyWrite]string{historyAdd: "/sync/history", historyRemove: "/sync/history/remove"}

// history is films and episodes as every tracker takes them into a history and out of one: an
// episode under its show and season, and when each was watched, which a removal leaves out.
type history struct {
	Movies []watchedTitle `json:"movies,omitzero"`
	Shows  []watchedShow  `json:"shows,omitzero"`
}

type watchedTitle struct {
	IDs       map[string]any `json:"ids"`
	WatchedAt *time.Time     `json:"watched_at,omitzero"`
}

type watchedShow struct {
	IDs     map[string]any  `json:"ids"`
	Seasons []watchedSeason `json:"seasons"`
}

type watchedSeason struct {
	Number   int              `json:"number"`
	Episodes []watchedEpisode `json:"episodes"`
}

type watchedEpisode struct {
	Number    int        `json:"number"`
	WatchedAt *time.Time `json:"watched_at,omitzero"`
}

// push tells each tracker what its accounts' profiles have marked watched or unwatched, and
// forgets what it told.
func (l *Links) push(ctx context.Context) error {
	claimed, err := l.st.ClaimTrackerChanges(ctx, time.Now().Add(-pushAfter), pushLease, pushBatch)
	if err != nil || len(claimed) == 0 {
		return err
	}
	clients, err := l.st.TrackerClients(ctx)
	if err != nil {
		return err
	}
	for _, c := range claimed {
		err := l.pushAccount(ctx, c, clients[c.Tracker])
		if err == nil {
			err = l.st.ForgetTrackerChanges(ctx, c.Rows)
		}
		if err != nil {
			// Claimed by any node again once the lease ends.
			l.log.WarnContext(ctx, "could not tell a tracker what was watched", slog.String("tracker", string(c.Tracker)), slog.Any("err", err))
		}
	}
	return nil
}

func (l *Links) pushAccount(ctx context.Context, c store.TrackerChanges, clientID string) error {
	grants, err := l.st.TrackerGrants(ctx, c.Profile)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(grants, func(g store.TrackerGrant) bool { return g.Tracker == c.Tracker })
	// A tracker an admin took away, or an account unlinked since, is told nothing.
	if clientID == "" || i < 0 {
		return nil
	}
	watched, unwatched := histories(c.Changes)
	svc := l.services[c.Tracker]
	return l.authorised(ctx, c.Profile, grants[i], clientID, func(access string) error {
		// Removed first: a push that fails between the two is pushed again whole, and a removal
		// twice is one, where an addition twice is two watches.
		for _, w := range []struct {
			write historyWrite
			h     history
		}{{historyRemove, unwatched}, {historyAdd, watched}} {
			if len(w.h.Movies)+len(w.h.Shows) == 0 {
				continue
			}
			if err := svc.history(ctx, clientID, access, w.write, w.h); err != nil {
				return err
			}
		}
		return nil
	})
}

// histories are the films and episodes changes left watched and unwatched, each as its last
// change left it: one title in two libraries is one to a tracker. A title trackers know by no id,
// or an episode with no number, is left out.
func histories(changes []store.TrackerChange) (watched, unwatched history) {
	type title struct {
		ids             string
		season, episode int
	}
	last := map[title]store.TrackerChange{}
	var order []title
	for _, c := range changes {
		ids := trackerIDs(c.IDs)
		if len(ids) == 0 || c.Kind == domain.ItemEpisode && c.Episode == 0 {
			continue
		}
		// fmt prints a map's keys sorted, so the same ids are the same title.
		t := title{fmt.Sprint(ids), c.Season, c.Episode}
		if _, ok := last[t]; !ok {
			order = append(order, t)
		}
		last[t] = c
	}
	for _, t := range order {
		c := last[t]
		h := &unwatched
		var at *time.Time
		if c.WatchedAt != nil {
			h = &watched
			utc := c.WatchedAt.UTC()
			at = &utc
		}
		ids := trackerIDs(c.IDs)
		if c.Kind != domain.ItemEpisode {
			h.Movies = append(h.Movies, watchedTitle{IDs: ids, WatchedAt: at})
			continue
		}
		show := slices.IndexFunc(h.Shows, func(s watchedShow) bool { return fmt.Sprint(s.IDs) == t.ids })
		if show < 0 {
			h.Shows, show = append(h.Shows, watchedShow{IDs: ids}), len(h.Shows)
		}
		seasons := &h.Shows[show].Seasons
		season := slices.IndexFunc(*seasons, func(s watchedSeason) bool { return s.Number == c.Season })
		if season < 0 {
			*seasons, season = append(*seasons, watchedSeason{Number: c.Season}), len(*seasons)
		}
		(*seasons)[season].Episodes = append((*seasons)[season].Episodes, watchedEpisode{Number: c.Episode, WatchedAt: at})
	}
	return watched, unwatched
}
