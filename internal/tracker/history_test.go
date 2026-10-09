//go:build integration

package tracker

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// What a profile marks watched or unwatched is pushed to each tracker it linked, in one request
// each way, unwatched first; a play its scrobble told watched is not told twice, and a push a
// tracker fails is pushed again once its claim lapses.
func TestWhatAProfileWatchesIsPushedToItsTrackers(t *testing.T) {
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
	for _, f := range []store.Film{
		{Title: "Heat", Folder: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}, Copies: hour("Heat/h.mkv")},
		{Title: "Alien", Folder: "Alien", IDs: map[domain.Provider]string{domain.ProviderTMDB: "348"}, Copies: hour("Alien/a.mkv")},
	} {
		if _, err := st.SaveFolder(ctx, films.ID, f.Folder, []byte("v"), []store.Film{f}, nil); err != nil {
			t.Fatal(err)
		}
	}
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	var eps []store.Episode
	for _, n := range []int{1, 2} {
		rel := "Wire/S01E0" + string(rune('0'+n)) + ".mkv"
		eps = append(eps, store.Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: "Wire", ByNumber: true, Copies: hour(rel)})
	}
	wire := store.Show{Title: "The Wire", Folder: "Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), wire, eps, nil); err != nil {
		t.Fatal(err)
	}
	item := func(where string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := db.QueryRow(ctx, `SELECT id FROM items WHERE `+where, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	heat, alien := item(`title = 'Heat'`), item(`title = 'Alien'`)
	first, second := item(`episode_number = 1`), item(`episode_number = 2`)

	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	trakt := newFake(t, domain.TrackerTrakt, "trakt-app")
	simkl := newFake(t, domain.TrackerSimkl, "simkl-app")
	links := New(st, nil, func(context.Context, domain.Event) {}, "1.2.3", log)
	links.services[domain.TrackerTrakt] = trakt.at(trakt.serve())
	links.services[domain.TrackerSimkl] = simkl.at(simkl.serve())
	mdblist := newFake(t, domain.TrackerMDBList, "mdblist-app")
	links.services[domain.TrackerMDBList] = mdblist.at(mdblist.serve())
	for _, tr := range []*fakeTracker{trakt, simkl, mdblist} {
		if err := st.SetTrackerClient(ctx, tr.tracker, tr.clientID); err != nil {
			t.Fatal(err)
		}
		tok := store.TrackerTokens{Access: "access", Refresh: "refresh", Expires: time.Now().Add(7 * 24 * time.Hour)}
		if err := st.LinkTracker(ctx, ada.ID, tr.tracker, "ada", tok); err != nil {
			t.Fatal(err)
		}
	}

	watched := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	if err := st.MarkWatched(ctx, ada.ID, heat, &watched); err != nil {
		t.Fatal(err)
	}
	for _, ep := range []uuid.UUID{first, second} {
		if err := st.MarkWatched(ctx, ada.ID, ep, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MarkUnwatched(ctx, ada.ID, second); err != nil {
		t.Fatal(err)
	}
	// Alien is played to its end, and its scrobble tells it watched.
	if _, err := st.SaveProgress(ctx, ada.ID, alien, 59*time.Minute, time.Hour, domain.ReachResumable, nil); err != nil {
		t.Fatal(err)
	}
	err = links.scrobble(ctx, domain.Event{Kind: domain.EventPlaybackStopped, Profile: ada.ID, Item: alien, Details: domain.PlaybackDetails{
		Playback: domain.NowPlaying{PositionMS: (59 * time.Minute).Milliseconds(), PlaybackCard: domain.PlaybackCard{
			Title: domain.PlaybackTitle{ID: alien, Kind: domain.ItemMovie, Title: "Alien"},
		}},
		Reach: domain.ReachEnd,
	}})
	if err != nil {
		t.Fatal(err)
	}

	push := func() {
		t.Helper()
		if err := links.push(ctx); err != nil {
			t.Fatal(err)
		}
	}
	push()
	if len(trakt.synced)+len(simkl.synced)+len(mdblist.synced) != 0 {
		t.Fatalf("pushed before the changes settled: %v %v %v", trakt.synced, simkl.synced, mdblist.synced)
	}
	if _, err := db.Exec(ctx, `UPDATE tracker_outbox SET queued_at = queued_at - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	simkl.set(func() { simkl.failing = true })
	push()
	asJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	wantRemoved := `{"shows":[{"ids":{"tvdb":79126},"seasons":[{"episodes":[{"number":2}],"number":1}]}]}`
	if len(trakt.synced) != 2 || trakt.synced[0].action != "/sync/history/remove" || asJSON(trakt.synced[0].body) != wantRemoved {
		t.Fatalf("Trakt was pushed %v, want the second episode removed first", trakt.synced)
	}
	at := asJSON(watched)
	wantAdded := `{"movies":[{"ids":{"tmdb":949},"watched_at":` + at + `}],"shows":[{"ids":{"tvdb":79126},"seasons":[{"episodes":[{"number":1,"watched_at":`
	if got := asJSON(trakt.synced[1].body); trakt.synced[1].action != "/sync/history" || len(got) < len(wantAdded) || got[:len(wantAdded)] != wantAdded {
		t.Errorf("Trakt was pushed %s, want Heat and the first episode added, and Alien left to its scrobble", got)
	}
	// MDBList takes the same, at its own paths.
	if len(mdblist.synced) != 2 || mdblist.synced[0].action != "/sync/watched/remove" || asJSON(mdblist.synced[0].body) != wantRemoved ||
		mdblist.synced[1].action != "/sync/watched" || asJSON(mdblist.synced[1].body) != asJSON(trakt.synced[1].body) {
		t.Errorf("MDBList was pushed %v, want what Trakt was", mdblist.synced)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM tracker_outbox`).Scan(&left); err != nil || left != 4 {
		t.Errorf("%d changes left, %v; want Simkl's four, which it failed", left, err)
	}

	// Simkl answers again once the claim on its changes lapses.
	simkl.set(func() { simkl.failing = false })
	push()
	if len(simkl.synced) != 0 {
		t.Errorf("Simkl's changes pushed again while still claimed: %v", simkl.synced)
	}
	if _, err := db.Exec(ctx, `UPDATE tracker_outbox SET claimed_until = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	push()
	if len(simkl.synced) != 2 || asJSON(simkl.synced[0].body) != wantRemoved || len(trakt.synced) != 2 {
		t.Errorf("Simkl was pushed %v, Trakt %d times; want Simkl the same, and Trakt nothing again", simkl.synced, len(trakt.synced))
	}
}
