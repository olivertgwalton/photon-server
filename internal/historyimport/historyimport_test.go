//go:build integration

package historyimport

import (
	"context"
	"errors"
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

type household struct {
	st      *store.Store
	db      *pgx.Conn
	profile uuid.UUID
}

// newHousehold has The Matrix, Alien and Heat, and The Wire's first two
// episodes, each an hour long.
func newHousehold(t *testing.T) household {
	t.Helper()
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
	copies := func(rel string) []store.Copy {
		return []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{
			RelPath: rel, Size: 1, ModTime: time.Unix(0, 0),
			Facts: &domain.Facts{Duration: time.Hour},
		}}}}
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Film{
		{Title: "The Matrix", Folder: "Matrix", IDs: map[domain.Provider]string{domain.ProviderTMDB: "603"}, Copies: copies("Matrix/m.mkv")},
		{Title: "Alien", Folder: "Alien", IDs: map[domain.Provider]string{domain.ProviderTMDB: "348"}, Copies: copies("Alien/a.mkv")},
		{Title: "Heat", Folder: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}, Copies: copies("Heat/h.mkv")},
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
		eps = append(eps, store.Episode{Season: 1, Episodes: []int{n}, Title: rel, Folder: "Wire", ByNumber: true, Copies: copies(rel)})
	}
	show := store.Show{Title: "The Wire", Folder: "Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), show, eps, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	return household{st: st, db: db, profile: ada.ID}
}

// item answers a film's id by its title, or an episode's by its number.
func (h household) item(t *testing.T, title string, episode int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.db.QueryRow(t.Context(), `
		SELECT id FROM items WHERE (kind = 'movie' AND title = $1) OR (kind = 'episode' AND episode_number = $2)`,
		title, episode).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type state struct {
	positionMS int64
	plays      int
	watchedAt  *time.Time
}

func (h household) state(t *testing.T, item uuid.UUID) state {
	t.Helper()
	var s state
	err := h.db.QueryRow(t.Context(), `
		SELECT position_ms, plays, watched_at FROM watch_state WHERE profile_id = $1 AND item_id = $2`,
		h.profile, item).Scan(&s.positionMS, &s.plays, &s.watchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s
	}
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// run starts an import and runs its job, answering how it ended.
func (h household) run(t *testing.T, kind domain.ImportSource, base string, c Credentials) domain.HistoryImport {
	t.Helper()
	imports := New(h.st, slog.New(slog.DiscardHandler))
	id, err := imports.Start(t.Context(), kind, base, h.profile, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := imports.Run(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	run, err := h.st.Import(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAPlexHistoryIsImportedWhereItIsNewer(t *testing.T) {
	h := newHousehold(t)
	ctx := t.Context()
	matrix, alien, first, second := h.item(t, "The Matrix", 0), h.item(t, "Alien", 0), h.item(t, "", 1), h.item(t, "", 2)
	// Ada has since started the second episode here, and Alien, which Plex says she watched but
	// not when.
	for _, started := range []uuid.UUID{second, alien} {
		if _, err := h.st.SaveProgress(ctx, h.profile, started, 10*time.Minute, time.Hour, domain.ReachStart, nil); err != nil {
			t.Fatal(err)
		}
	}
	f := plexLibrary()
	f.films = append(f.films,
		object{"ratingKey": "12", "title": "Solaris", "viewCount": 1, "lastViewedAt": watchedAt.Unix(), "Guid": []object{{"id": "tmdb://593"}}},
		object{"ratingKey": "13", "title": "Home Video", "viewCount": 1, "lastViewedAt": watchedAt.Unix()},
		object{"ratingKey": "14", "title": "Alien", "viewCount": 1, "Guid": []object{{"id": "tmdb://348"}}},
		object{"ratingKey": "15", "title": "Heat", "viewCount": 1, "Guid": []object{{"id": "tmdb://949"}}},
	)
	f.episodes = append(f.episodes, object{
		"ratingKey": "22", "grandparentTitle": "The Wire", "grandparentRatingKey": "20",
		"parentIndex": 1, "index": 2, "viewCount": 1, "lastViewedAt": watchedAt.Unix(),
	})
	base := f.serve(t)

	imports := New(h.st, slog.New(slog.DiscardHandler))
	if _, err := imports.Start(ctx, domain.ImportPlex, base, h.profile, Credentials{Token: "wrong"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("a wrong token: err = %v, want the import refused before it is queued", err)
	}
	if all, err := h.st.Imports(ctx); err != nil || len(all) != 0 {
		t.Fatalf("imports after a refusal = %+v, %v; want none", all, err)
	}

	run := h.run(t, domain.ImportPlex, base, Credentials{Token: f.token})
	if run.Status != domain.ImportDone || run.Matched != 5 || run.Imported != 3 || run.Skipped != 2 || run.Unmatched != 2 {
		t.Errorf("run = %+v; want done, 5 matched, 3 imported, 2 skipped (one newer here, one undated), 2 unmatched", run)
	}
	wantMisses := []domain.Missed{
		{Title: "Solaris", Reason: domain.MissNotFound},
		{Title: "Home Video", Reason: domain.MissNoIDs},
		{Title: "Alien", Reason: domain.MissUndated},
	}
	if !slices.Equal(run.Misses, wantMisses) {
		t.Errorf("misses = %+v, want %+v", run.Misses, wantMisses)
	}
	if s := h.state(t, matrix); s.plays != 2 || s.watchedAt == nil || !s.watchedAt.Equal(watchedAt) {
		t.Errorf("The Matrix = %+v, want watched twice, on the day Plex says", s)
	}
	if s := h.state(t, first); s.positionMS != 20*60_000 || s.watchedAt != nil {
		t.Errorf("S01E01 = %+v, want it to resume 20 minutes in", s)
	}
	if s := h.state(t, second); s.positionMS != 10*60_000 || s.watchedAt != nil {
		t.Errorf("S01E02 = %+v, want where Ada stopped here since, not Plex's older watch", s)
	}
	if s := h.state(t, alien); s.positionMS != 10*60_000 || s.watchedAt != nil {
		t.Errorf("Alien = %+v, want where Ada stopped here, not Plex's undated watch", s)
	}
	if s := h.state(t, h.item(t, "Heat", 0)); s.plays != 1 || s.watchedAt == nil {
		t.Errorf("Heat = %+v, want Plex's undated watch, with nothing of it here", s)
	}
	if _, _, err := h.st.StartImport(ctx, run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("starting a finished import again: err = %v, want its token forgotten", err)
	}
}

func TestAJellyfinHistoryIsImportedByShowAndNumber(t *testing.T) {
	h := newHousehold(t)
	f := jellyfinLibrary(false)
	run := h.run(t, domain.ImportJellyfin, f.serve(t), Credentials{Username: f.user, Password: f.password})
	if run.Status != domain.ImportDone || run.Matched != 2 || run.Imported != 2 {
		t.Fatalf("run = %+v; want both titles imported", run)
	}
	if s := h.state(t, h.item(t, "The Matrix", 0)); s.plays != 1 || s.watchedAt == nil || !s.watchedAt.Equal(watchedAt) {
		t.Errorf("The Matrix = %+v, want watched once, on the day Jellyfin says", s)
	}
	if s := h.state(t, h.item(t, "", 1)); s.plays != 3 || s.positionMS != 20*60_000 || s.watchedAt == nil {
		t.Errorf("S01E01 = %+v, want watched three times and 20 minutes into a fourth", s)
	}
	if f.signOuts != 1 {
		t.Errorf("sign-outs = %d, want the import's session ended", f.signOuts)
	}
}
