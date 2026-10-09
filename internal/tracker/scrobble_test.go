//go:build integration

package tracker

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// A profile's plays are told to each tracker it linked as they start, pause and stop: a film by
// its ids, an episode by its show's and its number, how far in, and a play the server counts
// watched as watched. A token near its expiry, or refused before it, is refreshed first; a grant
// the tracker no longer has unlinks its account.
func TestAProfilesPlaysAreToldToItsTrackers(t *testing.T) {
	ctx := t.Context()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(ctx, url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	db, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })

	hour := func(rel string) []store.Copy {
		return []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{
			RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour},
		}}}}
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	heat := store.Film{
		Title: "Heat", Folder: "Heat", Copies: hour("Heat/h.mkv"),
		IDs: map[domain.Provider]string{domain.ProviderTMDB: "949", domain.ProviderIMDb: "tt0113277"},
	}
	if _, err := st.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []store.Film{heat}, nil); err != nil {
		t.Fatal(err)
	}
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	wire := store.Show{Title: "The Wire", Folder: "Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	pilot := store.Episode{Season: 1, Episodes: []int{1}, Title: "The Target", Folder: "Wire", ByNumber: true, Copies: hour("Wire/S01E01.mkv")}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), wire, []store.Episode{pilot}, nil); err != nil {
		t.Fatal(err)
	}
	id := func(kind domain.ItemKind) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.QueryRow(ctx, `SELECT id FROM items WHERE kind = $1`, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	one := 1
	film := domain.PlaybackTitle{ID: id(domain.ItemMovie), Kind: domain.ItemMovie, Title: "Heat", Year: 1995}
	episode := domain.PlaybackTitle{
		ID: id(domain.ItemEpisode), Kind: domain.ItemEpisode, Title: "The Target",
		ShowID: id(domain.ItemShow), Show: "The Wire", SeasonNumber: &one, EpisodeNumber: &one,
	}

	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	trakt := newFake(t, domain.TrackerTrakt, "trakt-app")
	simkl := newFake(t, domain.TrackerSimkl, "simkl-app")
	var told []domain.Event
	links := New(st, nil, func(_ context.Context, e domain.Event) { told = append(told, e) }, "1.2.3", log)
	links.services[domain.TrackerTrakt] = trakt.at(trakt.serve())
	links.services[domain.TrackerSimkl] = simkl.at(simkl.serve())
	mdblist := newFake(t, domain.TrackerMDBList, "mdblist-app")
	links.services[domain.TrackerMDBList] = mdblist.at(mdblist.serve())
	for tr, expires := range map[*fakeTracker]time.Duration{trakt: time.Hour, simkl: 7 * 24 * time.Hour, mdblist: 30 * 24 * time.Hour} {
		if err := st.SetTrackerClient(ctx, tr.tracker, tr.clientID); err != nil {
			t.Fatal(err)
		}
		tok := store.TrackerTokens{Access: "access", Refresh: "refresh", Expires: time.Now().Add(expires)}
		if err := st.LinkTracker(ctx, ada.ID, tr.tracker, "ada", tok); err != nil {
			t.Fatal(err)
		}
	}

	play := func(kind domain.EventKind, title domain.PlaybackTitle, at time.Duration, reach domain.Reach) {
		t.Helper()
		e := domain.Event{Kind: kind, Profile: ada.ID, Item: title.ID, Details: domain.PlaybackDetails{
			Playback: domain.NowPlaying{PlaybackCard: domain.PlaybackCard{Title: title}, PositionMS: at.Milliseconds()},
			Reach:    reach,
		}}
		if err := links.scrobble(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	play(domain.EventPlaybackStarted, film, 0, "")
	play(domain.EventPlaybackPaused, film, 20*time.Minute, "")
	play(domain.EventPlaybackResumed, film, 20*time.Minute, "")
	// Its credits begin at 50 minutes: the server counts it watched.
	play(domain.EventPlaybackStopped, film, 50*time.Minute, domain.ReachEnd)
	play(domain.EventPlaybackStarted, episode, 6*time.Minute, "")

	for _, tr := range []*fakeTracker{trakt, simkl, mdblist} {
		var got []string
		for _, s := range tr.scrobbled {
			got = append(got, s.action)
		}
		if want := []string{"start", "pause", "start", "stop", "start"}; !slices.Equal(got, want) {
			t.Fatalf("%s was told %v, want %v", tr.tracker, got, want)
		}
		for i, want := range []float64{0, 33.33, 33.33, 100, 10} {
			if p := tr.scrobbled[i].body["progress"]; p != want {
				t.Errorf("%s's %s: progress %v, want %v", tr.tracker, tr.scrobbled[i].action, p, want)
			}
		}
		movie, _ := tr.scrobbled[0].body["movie"].(map[string]any)
		ids, _ := movie["ids"].(map[string]any)
		if movie["title"] != "Heat" || movie["year"] != 1995.0 || ids["tmdb"] != 949.0 || ids["imdb"] != "tt0113277" {
			t.Errorf("%s was told the film as %v", tr.tracker, movie)
		}
		show, _ := tr.scrobbled[4].body["show"].(map[string]any)
		showIDs, _ := show["ids"].(map[string]any)
		ep, _ := tr.scrobbled[4].body["episode"].(map[string]any)
		// MDBList takes the episode within its season, within its show.
		if tr == mdblist {
			season, _ := show["season"].(map[string]any)
			ep, _ = season["episode"].(map[string]any)
			ep = map[string]any{"season": season["number"], "number": ep["number"]}
		}
		if showIDs["tvdb"] != 79126.0 || ep["season"] != 1.0 || ep["number"] != 1.0 || tr.scrobbled[4].body["movie"] != nil {
			t.Errorf("%s was told the episode as %v", tr.tracker, tr.scrobbled[4].body)
		}
	}
	if trakt.refreshes != 1 || simkl.refreshes != 0 || mdblist.refreshes != 0 {
		t.Errorf("refreshed Trakt %d times, Simkl %d and MDBList %d, want Trakt's token, a day from expiry, once",
			trakt.refreshes, simkl.refreshes, mdblist.refreshes)
	}

	// Trakt ends the access token early, so it is refreshed and the play told again; Simkl's user
	// revokes the app, so its account is unlinked and the profile told.
	trakt.set(func() { trakt.access = "ended early" })
	simkl.set(func() { simkl.access, simkl.refresh = "revoked", "revoked" })
	play(domain.EventPlaybackStopped, episode, 30*time.Minute, "")
	if trakt.refreshes != 2 || len(trakt.scrobbled) != 6 || trakt.scrobbled[5].action != "stop" {
		t.Errorf("Trakt, its token ended early: refreshed %d times, told %d plays; want the play told after a refresh", trakt.refreshes, len(trakt.scrobbled))
	}
	accounts, err := st.TrackerGrants(ctx, ada.ID)
	if err != nil || len(accounts) != 2 || slices.ContainsFunc(accounts, func(g store.TrackerGrant) bool { return g.Tracker == domain.TrackerSimkl }) {
		t.Errorf("accounts once Simkl's grant is gone: %+v, %v; want Trakt's and MDBList's", accounts, err)
	}
	if len(told) != 1 || told[0].Details != (domain.TrackerDetails{Tracker: domain.TrackerSimkl}) || told[0].Profile != ada.ID {
		t.Errorf("told %+v, want Ada told of Simkl", told)
	}
}
