package themerr

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type fakeStore struct {
	subject store.ThemeSubject
	saved   map[uuid.UUID]string
}

func (s *fakeStore) ThemeSubject(context.Context, uuid.UUID) (store.ThemeSubject, bool, error) {
	return s.subject, true, nil
}

func (s *fakeStore) SaveFetchedTheme(_ context.Context, _, id uuid.UUID, url string) error {
	s.saved[id] = url
	s.subject.Theme, s.subject.URL = id, url
	return nil
}

type fakeMisses map[string]time.Duration

func (fakeMisses) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

func (m fakeMisses) NoteThemeMissing(_ context.Context, key string, ttl time.Duration) error {
	m[key] = ttl
	return nil
}

func (m fakeMisses) ThemeMissing(_ context.Context, key string) (bool, error) {
	_, ok := m[key]
	return ok, nil
}

// fakeYTDLP answers as yt-dlp does: a link naming "blocked" is refused by YouTube, one naming
// "flaky" fails fetching its data, and any other is written where it is asked to be. Each run is
// counted in runs.
func fakeYTDLP(t *testing.T) (path, runs string) {
	t.Helper()
	dir := t.TempDir()
	path, runs = filepath.Join(dir, "yt-dlp"), filepath.Join(dir, "runs")
	script := `#!/bin/sh
echo run >> '` + runs + `'
for a; do [ "$prev" = --paths ] && out=$a; prev=$a; link=$a; done
case "$link" in
*blocked*) echo "ERROR: [youtube] blocked: Video unavailable. This video contains content from SME, who has blocked it on copyright grounds" >&2; exit 1 ;;
*flaky*) echo "ERROR: unable to download video data: HTTP Error 403: Forbidden" >&2; exit 1 ;;
esac
printf 'm4a of %s' "$link" > "$out/theme.m4a"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, runs
}

func ran(t *testing.T, runs string) int {
	t.Helper()
	b, err := os.ReadFile(runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "run")
}

// fakeDB is ThemerrDB listing links by path; any other path is a 404, as GitHub Pages answers.
func fakeDB(t *testing.T, links map[string]string) (string, *atomic.Int32) {
	t.Helper()
	var asked atomic.Int32
	db := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		link, ok := links[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id": 1, "youtube_theme_added": 1694834191, "youtube_theme_url": "`+link+`"}`)
	}))
	t.Cleanup(db.Close)
	return db.URL + "/", &asked
}

// A title ThemerrDB lists has its link's sound kept, once; a changed link is fetched again; a film
// it lists by IMDb's id alone is found by that.
func TestAListedThemeIsKeptAndFetchedAgainOnlyWhenItsLinkChanges(t *testing.T) {
	links := map[string]string{
		"/movies/themoviedb/603.json": "https://www.youtube.com/watch?v=SLBACEP6LsI",
		"/movies/imdb/tt0113277.json": "https://www.youtube.com/watch?v=heat",
	}
	db, _ := fakeDB(t, links)
	ytdlp, runs := fakeYTDLP(t)
	cache, err := artwork.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	fetch := func(st *fakeStore) {
		t.Helper()
		if err := Fetch(st, cache, fakeMisses{}, db, ytdlp, "ffmpeg", slog.New(slog.DiscardHandler))(t.Context(), uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
	}
	kept := func(st *fakeStore) string {
		t.Helper()
		f, err := cache.Kept(st.subject.Theme)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		return string(b)
	}

	matrix := &fakeStore{subject: store.ThemeSubject{Kind: domain.ItemMovie, TMDB: "603", IMDb: "tt0133093"}, saved: map[uuid.UUID]string{}}
	fetch(matrix)
	if got := kept(matrix); got != "m4a of https://www.youtube.com/watch?v=SLBACEP6LsI" || len(matrix.saved) != 1 {
		t.Fatalf("kept %q, saved %v; want the listed link's sound, once", got, matrix.saved)
	}
	fetch(matrix)
	if n := ran(t, runs); n != 1 || len(matrix.saved) != 1 {
		t.Errorf("asked again with the same link: yt-dlp ran %d times, saved %v; want once", n, matrix.saved)
	}
	links["/movies/themoviedb/603.json"] = "https://www.youtube.com/watch?v=new"
	fetch(matrix)
	if got := kept(matrix); got != "m4a of https://www.youtube.com/watch?v=new" {
		t.Errorf("after ThemerrDB changed its link, kept %q, want the new link's", got)
	}

	heat := &fakeStore{subject: store.ThemeSubject{Kind: domain.ItemMovie, TMDB: "949", IMDb: "tt0113277"}, saved: map[uuid.UUID]string{}}
	fetch(heat)
	if got := kept(heat); got != "m4a of https://www.youtube.com/watch?v=heat" {
		t.Errorf("a film listed by IMDb's id: kept %q", got)
	}
}

// A title ThemerrDB does not list, and a link YouTube refuses, are answers of no theme, each asked
// about once for a while; a link whose data could not be fetched is a failure to try again.
func TestNoThemeIsAnAnswerAndAFailedFetchIsNot(t *testing.T) {
	db, asked := fakeDB(t, map[string]string{
		"/tv_shows/themoviedb/1.json": "https://www.youtube.com/watch?v=blocked",
		"/tv_shows/themoviedb/2.json": "https://www.youtube.com/watch?v=flaky",
	})
	ytdlp, runs := fakeYTDLP(t)
	cache, err := artwork.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	misses := fakeMisses{}
	show := func(tmdb string) *fakeStore {
		return &fakeStore{subject: store.ThemeSubject{Kind: domain.ItemShow, TMDB: tmdb, IMDb: "tt0903747"}, saved: map[uuid.UUID]string{}}
	}
	unlisted, blocked := show("1396"), show("1")
	ids := map[*fakeStore]uuid.UUID{unlisted: uuid.NewV7(), blocked: uuid.NewV7()}
	for range 2 {
		for st, id := range ids {
			if err := Fetch(st, cache, misses, db, ytdlp, "ffmpeg", slog.New(slog.DiscardHandler))(t.Context(), id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(unlisted.saved)+len(blocked.saved) != 0 {
		t.Errorf("saved %v and %v, want no theme", unlisted.saved, blocked.saved)
	}
	if n := ran(t, runs); n != 1 || misses["https://www.youtube.com/watch?v=blocked"] != refusedFor {
		t.Errorf("a refused link: yt-dlp ran %d times, noted %v; want once, for a week", n, misses)
	}
	if n := asked.Load(); n != 3 {
		t.Errorf("ThemerrDB asked %d times, want once for the unlisted show (by TMDB's id alone) and twice for the blocked one", n)
	}

	if err := Fetch(show("2"), cache, misses, db, ytdlp, "ffmpeg", slog.New(slog.DiscardHandler))(t.Context(), uuid.NewV7()); err == nil {
		t.Error("a link whose data could not be fetched answered no error, want one to try again")
	}
}
