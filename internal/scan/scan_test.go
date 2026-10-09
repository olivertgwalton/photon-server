//go:build integration

package scan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

type fakeProber struct{}

func (fakeProber) Probe(context.Context, *os.File) (domain.Facts, error) {
	return domain.Facts{
		Container: "matroska,webm",
		Duration:  2 * time.Hour,
		Streams: []domain.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 2160, Range: domain.RangeHDR10},
			{Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8},
		},
		Chapters: []domain.Chapter{{Start: 0, End: time.Hour, Title: "One"}},
	}, nil
}

type fixture struct {
	t       *testing.T
	root    string
	lib     domain.Library
	db      *pgx.Conn
	st      *store.Store
	scanner *Scanner
}

func newFixture(t *testing.T, kind domain.LibraryKind) *fixture {
	t.Helper()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	db, err := pgx.Connect(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(context.Background()) })
	root := t.TempDir()
	lib, err := st.AddLibrary(t.Context(), "Library", kind, root)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, root: root, lib: lib, db: db, st: st, scanner: New(st, fakeProber{}, log)}
}

// put writes a file whose bytes are its seed repeated, so different seeds are different copies.
func (f *fixture) put(rel, seed string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Repeat([]byte(seed), 50_000), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) scan() Report {
	f.t.Helper()
	r, err := f.scanner.Scan(f.t.Context(), f.lib, []string{"."}, func(domain.ScanProgress) {}, func(store.Changed) {})
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *fixture) count(sql string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(f.t.Context(), sql).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestScanFilms(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995) [tmdbid-949]/Heat (1995) - 2160p.mkv", "heat-uhd")
	f.put("Heat (1995) [tmdbid-949]/Heat (1995) - 1080p.mkv", "heat-hd")
	f.put("Lawrence of Arabia (1962)/Lawrence of Arabia (1962) cd1.mkv", "lawrence-1")
	f.put("Lawrence of Arabia (1962)/Lawrence of Arabia (1962) cd2.mkv", "lawrence-2")
	f.put("Alien (1979).mkv", "alien")

	if r := f.scan(); r.Probed != 5 {
		t.Errorf("first scan probed %d parts, want 5", r.Probed)
	}
	if n := f.count("SELECT count(*) FROM items"); n != 3 {
		t.Errorf("%d titles, want 3", n)
	}
	if n := f.count(`SELECT count(*) FROM versions v JOIN items i ON i.id = v.item_id WHERE i.title = 'Heat' AND i.year = 1995`); n != 2 {
		t.Errorf("Heat has %d versions, want 2", n)
	}
	if n := f.count(`SELECT count(*) FROM external_ids WHERE provider = 'tmdb' AND value = '949'`); n != 1 {
		t.Error("Heat's TMDB id from its folder was not kept")
	}
	if n := f.count(`SELECT count(*) FROM parts p JOIN versions v ON v.id = p.version_id JOIN items i ON i.id = v.item_id
		WHERE i.title = 'Lawrence of Arabia' AND (p.idx, p.offset_ms) IN ((0, 0), (1, 7200000))`); n != 2 {
		t.Error("Lawrence's two parts are not one copy on one timeline")
	}
	if n := f.count(`SELECT count(*) FROM streams WHERE kind = 'video' AND video_range = 'hdr10'`); n != 5 {
		t.Errorf("%d video streams recorded, want 5", n)
	}

	if n := f.count(`SELECT count(*) FROM jobs WHERE kind = 'keyframes'`); n != 5 {
		t.Errorf("%d keyframe jobs queued, want one per new part with video (5)", n)
	}

	if r := f.scan(); r.Probed != 0 || r.Unchanged != r.Folders {
		t.Errorf("rescanning an unchanged library: %+v, want no probes and every folder unchanged", r)
	}
}

func TestRenameKeepsIdentity(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.scan()
	var item, version string
	if err := f.db.QueryRow(t.Context(), "SELECT v.item_id::text, v.id::text FROM versions v").Scan(&item, &version); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(f.root, "Heat (1995)"), filepath.Join(f.root, "Heat")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(f.root, "Heat", "Heat (1995).mkv"), filepath.Join(f.root, "Heat", "heat.mkv")); err != nil {
		t.Fatal(err)
	}
	if r := f.scan(); r.Probed != 0 {
		t.Errorf("a renamed file was probed again (%d probes)", r.Probed)
	}
	if n := f.count(`SELECT count(*) FROM jobs WHERE kind = 'keyframes'`); n != 1 {
		t.Errorf("%d jobs after a rename, want the original one only", n)
	}
	var gotItem, gotVersion, path string
	err := f.db.QueryRow(t.Context(),
		`SELECT v.item_id::text, v.id::text, f.rel_path FROM versions v
		JOIN parts p ON p.version_id = v.id JOIN part_files f ON f.part_id = p.id`).
		Scan(&gotItem, &gotVersion, &path)
	if err != nil {
		t.Fatal(err)
	}
	if gotItem != item || gotVersion != version || path != "Heat/heat.mkv" {
		t.Errorf("after the rename: title %s, version %s at %q; want title %s, version %s at Heat/heat.mkv",
			gotItem, gotVersion, path, item, version)
	}
	if n := f.count("SELECT count(*) FROM items"); n != 1 {
		t.Errorf("%d titles after the rename, want 1", n)
	}
}

func TestRemovedFileIsMissingNotForgotten(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Alien (1979)/Alien (1979).mkv", "alien")
	f.scan()
	if err := os.Remove(filepath.Join(f.root, "Heat (1995)", "Heat (1995).mkv")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if n := f.count(`SELECT count(*) FROM versions v JOIN items i ON i.id = v.item_id
		WHERE i.title = 'Heat' AND v.missing_since IS NOT NULL`); n != 1 {
		t.Error("the removed copy is not marked missing")
	}
	if n := f.count(`SELECT count(*) FROM versions WHERE missing_since IS NULL`); n != 1 {
		t.Error("the copy still on disk is marked missing")
	}

	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.scan()
	if n := f.count(`SELECT count(*) FROM versions WHERE missing_since IS NOT NULL`); n != 0 {
		t.Error("a copy that came back is still missing")
	}

	if err := os.RemoveAll(filepath.Join(f.root, "Alien (1979)")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if n := f.count(`SELECT count(*) FROM folders WHERE path = 'Alien (1979)'`); n != 0 {
		t.Error("a deleted folder is still remembered")
	}
}

// A library of links into another mount, as Riven and other debrid setups make them, scans as
// the files behind the links; a link the mount no longer answers is left out and counted, and a
// root that cannot be read fails the scan rather than emptying the library.
func TestALibraryOfLinksScansWhatTheyLeadTo(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	mount := t.TempDir()
	if err := os.WriteFile(filepath.Join(mount, "heat.mkv"), bytes.Repeat([]byte("heat"), 50_000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.root, "Heat (1995)"), 0o755); err != nil {
		t.Fatal(err)
	}
	for target, name := range map[string]string{
		"heat.mkv": "Heat (1995)/Heat (1995).mkv",
		"gone.mkv": "Heat (1995)/Heat (1995) - 1080p.mkv",
	} {
		if err := os.Symlink(filepath.Join(mount, target), filepath.Join(f.root, name)); err != nil {
			t.Fatal(err)
		}
	}
	r := f.scan()
	if r.Probed != 1 || r.Skipped != 1 {
		t.Errorf("probed %d and left out %d, want 1 and 1", r.Probed, r.Skipped)
	}
	if n := f.count(`SELECT count(*) FROM items WHERE title = 'Heat'`); n != 1 {
		t.Error("the linked film was not found")
	}

	f.lib.Root = filepath.Join(f.root, "unmounted")
	if _, err := f.scanner.Scan(t.Context(), f.lib, []string{"."}, func(domain.ScanProgress) {}, func(store.Changed) {}); err == nil {
		t.Error("a root that cannot be read scanned")
	}
	if n := f.count(`SELECT count(*) FROM versions WHERE missing_since IS NULL`); n != 1 {
		t.Error("an unreadable root took the library's titles with it")
	}
}

// Plex's rule: a byte-identical copy is another place to read one version. Removing either copy
// costs nothing; removing both makes the version missing.
func TestIdenticalCopiesAreOneVersionInTwoPlaces(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Backup/Heat (1995)/Heat (1995).mkv", "heat")
	f.scan()
	if n := f.count("SELECT count(*) FROM versions"); n != 1 {
		t.Errorf("%d versions, want 1", n)
	}
	if n := f.count("SELECT count(*) FROM part_files"); n != 2 {
		t.Errorf("%d places to read it, want 2", n)
	}
	if err := os.RemoveAll(filepath.Join(f.root, "Backup")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if n := f.count("SELECT count(*) FROM versions WHERE missing_since IS NULL"); n != 1 {
		t.Error("removing one of two copies made the version missing")
	}
	if n := f.count("SELECT count(*) FROM part_files"); n != 1 {
		t.Errorf("%d places after removing the backup, want 1", n)
	}
	if err := os.RemoveAll(filepath.Join(f.root, "Heat (1995)")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if n := f.count("SELECT count(*) FROM versions WHERE missing_since IS NOT NULL"); n != 1 {
		t.Error("removing every copy did not make the version missing")
	}
}

// episodes answers each episode as its number and the paths its copies are read from.
func (f *fixture) episodes() string {
	f.t.Helper()
	return f.title(`SELECT coalesce(string_agg(e, '; ' ORDER BY e), '') FROM (
		SELECT i.episode_number || ': ' || string_agg(pf.rel_path, ', ' ORDER BY pf.rel_path) AS e
		FROM items i JOIN versions v ON v.item_id = i.id JOIN parts p ON p.version_id = v.id
		JOIN part_files pf ON pf.part_id = p.id WHERE i.kind = 'episode' GROUP BY i.id) s`)
}

// riven shows a double-length episode once under each of its numbers. Each is an episode of its
// own, read from its own path, as Jellyfin keeps an item per path.
func TestTheSameBytesUnderTwoNumbersAreTwoEpisodes(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("Friends (1994)/Season 01/s01e01.mkv", "pilot")
	f.put("Friends (1994)/Season 01/s01e02.mkv", "pilot")
	f.scan()
	if got, want := f.episodes(), "1: Friends (1994)/Season 01/s01e01.mkv; 2: Friends (1994)/Season 01/s01e02.mkv"; got != want {
		t.Errorf("episodes %q, want %q", got, want)
	}
	if n := f.count(`SELECT count(*) FROM (SELECT key FROM title_keys GROUP BY key HAVING count(*) > 1) s`); n != 0 {
		t.Errorf("the two episodes share %d keys, want none: they are not one title", n)
	}
	if r := f.scan(); r.Probed != 0 || r.Unchanged != r.Folders {
		t.Errorf("rescanning: %+v, want nothing read again", r)
	}
}

// One episode holding both paths is how such a pair was kept before; the next scan splits it.
func TestAnEpisodeHeldUnderTwoNumbersIsSplitKeepingItsID(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("Friends (1994)/Season 09/s09e24.mkv", "barbados")
	f.scan()
	id := f.title(`SELECT id::text FROM items WHERE kind = 'episode'`)
	f.put("Friends (1994)/Season 09/s09e23.mkv", "barbados")
	info, err := os.Stat(filepath.Join(f.root, "Friends (1994)/Season 09/s09e23.mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(t.Context(), `
		INSERT INTO part_files (part_id, library_id, rel_path, size_bytes, mtime_ns)
		SELECT part_id, library_id, 'Friends (1994)/Season 09/s09e23.mkv', $1, $2 FROM part_files`,
		info.Size(), info.ModTime().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(t.Context(), `DELETE FROM jobs WHERE kind = 'identify'`); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if got, want := f.episodes(), "23: Friends (1994)/Season 09/s09e23.mkv; 24: Friends (1994)/Season 09/s09e24.mkv"; got != want {
		t.Errorf("episodes %q, want %q", got, want)
	}
	if got := f.title(`SELECT id::text FROM items WHERE kind = 'episode' AND episode_number = 24`); got != id {
		t.Errorf("episode 24 is %s, want %s kept", got, id)
	}
	if n := f.count(`SELECT count(*) FROM jobs j JOIN items i ON i.id = j.subject WHERE j.kind = 'identify' AND i.kind = 'show'`); n != 1 {
		t.Error("the show is not matched again to describe the episode split off")
	}
}

func TestARenamedEpisodeKeepsItsItem(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("Friends (1994)/Season 01/s01e01.mkv", "pilot")
	f.scan()
	id := f.title(`SELECT id::text FROM items WHERE kind = 'episode'`)
	dir := filepath.Join(f.root, "Friends (1994)/Season 01")
	if err := os.Rename(filepath.Join(dir, "s01e01.mkv"), filepath.Join(dir, "s01e02.mkv")); err != nil {
		t.Fatal(err)
	}
	if r := f.scan(); r.Probed != 0 {
		t.Errorf("a renamed file was probed again (%d probes)", r.Probed)
	}
	if got := f.title(`SELECT id::text || ' ' || episode_number FROM items WHERE kind = 'episode'`); got != id+" 2" {
		t.Errorf("episodes %q, want %s numbered 2", got, id)
	}
}

// failingOnce fails to probe each file named once, as a debrid mount fails a read now and then.
type failingOnce struct {
	fakeProber
	mu    sync.Mutex
	names map[string]bool
}

func (p *failingOnce) Probe(ctx context.Context, f *os.File) (domain.Facts, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if name := filepath.Base(f.Name()); p.names[name] {
		delete(p.names, name)
		return domain.Facts{}, errors.New("input/output error")
	}
	return p.fakeProber.Probe(ctx, f)
}

func TestAFileThatFailedToReadIsTriedAgainAtTheNextScan(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.scanner = New(f.st, &failingOnce{names: map[string]bool{"Desperate Housewives - s03e18.mkv": true}}, slog.New(slog.DiscardHandler))
	for _, e := range []string{"17", "18", "19"} {
		f.put("Desperate Housewives (2004)/Season 03/Desperate Housewives - s03e"+e+".mkv", "e"+e)
	}
	if r := f.scan(); r.Skipped != 1 {
		t.Errorf("left out %d files, want the one that failed", r.Skipped)
	}
	if r := f.scan(); r.Probed != 1 || r.Skipped != 0 {
		t.Errorf("the next scan probed %d and left out %d, want only the file that failed probed", r.Probed, r.Skipped)
	}
	if n := f.count(`SELECT count(*) FROM items WHERE kind = 'episode' AND episode_number = 18`); n != 1 {
		t.Error("the file that failed is not an episode after the next scan")
	}
	if r := f.scan(); r.Probed != 0 || r.Unchanged != r.Folders {
		t.Errorf("a scan after the file was read: %+v, want every folder unchanged", r)
	}
}

// notMedia finds no media in the files named, as ffprobe finds none in a file cut short.
type notMedia struct {
	fakeProber
	names map[string]bool
}

func (p *notMedia) Probe(ctx context.Context, f *os.File) (domain.Facts, error) {
	if p.names[filepath.Base(f.Name())] {
		return domain.Facts{}, media.ErrNotMedia
	}
	return p.fakeProber.Probe(ctx, f)
}

func TestAFileThatIsNotMediaIsNotProbedAgainUntilItChanges(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	p := &notMedia{names: map[string]bool{"Heat (1995).mkv": true}}
	f.scanner = New(f.st, p, slog.New(slog.DiscardHandler))
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	if r := f.scan(); r.Skipped != 1 {
		t.Errorf("left out %d files, want the one that is not media", r.Skipped)
	}
	if r := f.scan(); r.Probed != 0 || r.Unchanged != r.Folders {
		t.Errorf("the next scan: %+v, want the file not probed again", r)
	}
	f.put("Heat (1995)/Heat (1995).mkv", "heat, whole")
	delete(p.names, "Heat (1995).mkv")
	if r := f.scan(); r.Probed != 1 || r.Skipped != 0 {
		t.Errorf("after the file changed: %+v, want it probed", r)
	}
}

func TestOneFileInTwoLibrariesIsACopyInEach(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.scan()

	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "Heat"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(f.root, "Heat (1995)", "Heat (1995).mkv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "Heat", "Heat.mkv"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := f.scanner.store.AddLibrary(t.Context(), "Second", domain.LibraryMovies, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.scanner.Scan(t.Context(), lib, []string{"."}, func(domain.ScanProgress) {}, func(store.Changed) {}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT count(DISTINCT i.library_id) FROM versions v JOIN items i ON i.id = v.item_id`); n != 2 {
		t.Errorf("the file is a copy in %d libraries, want 2", n)
	}
	if n := f.count(`SELECT count(*) FROM versions v JOIN items i ON i.id = v.item_id WHERE v.library_id <> i.library_id`); n != 0 {
		t.Error("a version belongs to a title in another library")
	}
}

func TestScanShows(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("The Wire (2002) [tvdbid-79126]/Season 1/The Wire S01E01.mkv", "s1e1")
	f.put("The Wire (2002) [tvdbid-79126]/Season 1/The Wire S01E02.mkv", "s1e2")
	f.put("The Wire (2002) [tvdbid-79126]/Season 2/The Wire S02E01 - 720p.mkv", "s2e1-hd")
	f.put("The Wire (2002) [tvdbid-79126]/Season 2/The Wire S02E01 - 1080p.mkv", "s2e1-fhd")
	f.put("The Wire (2002) [tvdbid-79126]/Specials/The Wire S00E01.mkv", "special")
	f.put("The Wire (2002) [tvdbid-79126]/Extras/Making Of.mkv", "extra")
	f.put("Stray.mkv", "stray")

	if r := f.scan(); r.Probed != 6 {
		t.Errorf("first scan probed %d files, want 5 episodes and 1 extra", r.Probed)
	}
	for _, c := range []struct {
		what string
		sql  string
		want int
	}{
		{"shows", `SELECT count(*) FROM items WHERE kind = 'show' AND title = 'The Wire' AND year = 2002`, 1},
		{"the show's extra", `SELECT count(*) FROM items e JOIN items s ON s.id = e.parent_id WHERE e.kind = 'extra' AND s.kind = 'show'`, 1},
		{"seasons 0, 1 and 2", `SELECT count(*) FROM items s JOIN items sh ON sh.id = s.parent_id
			WHERE s.kind = 'season' AND sh.kind = 'show' AND s.season_number IN (0, 1, 2)`, 3},
		{"episodes", `SELECT count(*) FROM items e JOIN items s ON s.id = e.parent_id WHERE e.kind = 'episode' AND s.kind = 'season'`, 4},
		{"copies of S02E01", `SELECT count(*) FROM versions v JOIN items e ON e.id = v.item_id
			WHERE e.season_number = 2 AND e.episode_number = 1`, 2},
		{"the show's TVDB id", `SELECT count(*) FROM external_ids x JOIN items i ON i.id = x.item_id
			WHERE i.kind = 'show' AND x.provider = 'tvdb' AND x.value = '79126'`, 1},
	} {
		if n := f.count(c.sql); n != c.want {
			t.Errorf("%s: %d, want %d", c.what, n, c.want)
		}
	}
	if r := f.scan(); r.Probed != 0 {
		t.Errorf("rescanning probed %d files", r.Probed)
	}
}

func TestExternalSubtitles(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Heat (1995)/Heat (1995).en.sdh.srt", "1")
	f.put("Heat (1995)/Subs/Heat (1995).fr.forced.srt", "2")
	f.scan()
	if n := f.count(`SELECT count(*) FROM subtitle_files WHERE language = 'en' AND hearing_impaired AND codec = 'subrip'`); n != 1 {
		t.Error("the English SDH subtitle beside the film was not recorded")
	}
	if n := f.count(`SELECT count(*) FROM subtitle_files WHERE rel_path = 'Heat (1995)/Subs/Heat (1995).fr.forced.srt' AND forced`); n != 1 {
		t.Error("the forced French subtitle in Subs was not recorded")
	}

	if err := os.Remove(filepath.Join(f.root, "Heat (1995)", "Subs", "Heat (1995).fr.forced.srt")); err != nil {
		t.Fatal(err)
	}
	f.put("Heat (1995)/Heat (1995).de.srt", "3")
	if r := f.scan(); r.Probed != 0 {
		t.Errorf("a subtitle change probed the film again (%d)", r.Probed)
	}
	if n := f.count(`SELECT count(*) FROM subtitle_files`); n != 2 {
		t.Errorf("%d subtitles after removing one and adding one, want 2", n)
	}
	if n := f.count(`SELECT count(*) FROM subtitle_files WHERE language = 'de'`); n != 1 {
		t.Error("the added German subtitle was not recorded")
	}
}

func TestExtrasBelongToTheirTitles(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Heat (1995)/Heat (1995)-trailer.mkv", "heat-trailer")
	f.put("Heat (1995)/Featurettes/Making Of.mkv", "heat-making")
	f.put("Heat (1995)/Sample/heat-sample.mkv", "heat-sample")
	f.put("Collection/Alien (1979).mkv", "alien")
	f.put("Collection/Aliens (1986).mkv", "aliens")
	f.put("Collection/Trailers/Teaser.mkv", "teaser")

	r := f.scan()
	if r.Skipped != 1 {
		t.Errorf("left out %d files, want the trailer no single film owns", r.Skipped)
	}
	for _, c := range []struct {
		what string
		sql  string
		want int
	}{
		{"Heat's trailer", `SELECT count(*) FROM items e JOIN items m ON m.id = e.parent_id
			WHERE e.kind = 'extra' AND e.extra_kind = 'trailer' AND e.title = 'Trailer' AND m.title = 'Heat'`, 1},
		{"Heat's featurette", `SELECT count(*) FROM items e JOIN items m ON m.id = e.parent_id
			WHERE e.extra_kind = 'featurette' AND e.title = 'Making Of' AND m.title = 'Heat'`, 1},
		{"extras with a playable copy", `SELECT count(*) FROM items e JOIN versions v ON v.item_id = e.id WHERE e.kind = 'extra'`, 2},
		{"films", `SELECT count(*) FROM items WHERE kind = 'movie'`, 3},
		{"anything made of the sample", `SELECT count(*) FROM items WHERE title ILIKE '%sample%'`, 0},
	} {
		if n := f.count(c.sql); n != c.want {
			t.Errorf("%s: %d, want %d", c.what, n, c.want)
		}
	}
	if r := f.scan(); r.Probed != 0 {
		t.Errorf("rescanning probed %d files", r.Probed)
	}
}

func TestShowExtrasBelongToShowSeasonOrEpisode(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("The Wire/Season 1/The Wire S01E01.mkv", "e1")
	f.put("The Wire/Season 1/The Wire S01E01-deleted.mkv", "e1-deleted")
	f.put("The Wire/Season 1/Featurettes/Making Season One.mkv", "s1-making")
	f.put("The Wire/Extras/Interview.mkv", "interview")
	f.scan()
	for owner, want := range map[string]string{"episode": "deleted_scene", "season": "featurette", "show": "other"} {
		sql := `SELECT count(*) FROM items e JOIN items o ON o.id = e.parent_id
			WHERE e.kind = 'extra' AND o.kind = '` + owner + `' AND e.extra_kind = '` + want + `'`
		if n := f.count(sql); n != 1 {
			t.Errorf("the %s owns %d %s extras, want 1", owner, n, want)
		}
	}
}

func TestAnExtraThatIsAFilmsCopyLeavesTheFilmAlone(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Alien (1979)/Alien (1979).mkv", "alien")
	f.put("Alien (1979)/Featurettes/Heat Preview.mkv", "heat")
	f.scan()
	if n := f.count(`SELECT count(*) FROM items WHERE kind = 'movie'`); n != 2 {
		t.Errorf("%d films, want Heat and Alien still films", n)
	}
	if n := f.count(`SELECT count(*) FROM part_files f JOIN parts p ON p.id = f.part_id
		JOIN versions v ON v.id = p.version_id JOIN items i ON i.id = v.item_id WHERE i.title = 'Heat'`); n != 2 {
		t.Errorf("Heat is read from %d places, want its own and the featurette's", n)
	}
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, rel), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) title(sql string) string {
	f.t.Helper()
	var s string
	if err := f.db.QueryRow(f.t.Context(), sql).Scan(&s); err != nil {
		f.t.Fatal(err)
	}
	return s
}

func TestNFOsDescribeTheirTitles(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("the wire/Season 1/the.wire.s01e01.mkv", "e1")
	f.write("the wire/tvshow.nfo", `<tvshow><title>The Wire</title><uniqueid type="tvdb">79126</uniqueid></tvshow>`)
	f.write("the wire/Season 1/the.wire.s01e01.nfo", `<episodedetails><title>The Target</title><plot>Baltimore.</plot></episodedetails>`)
	f.scan()
	if got := f.title(`SELECT title FROM items WHERE kind = 'show'`); got != "The Wire" {
		t.Errorf("show title = %q, want tvshow.nfo's", got)
	}
	if got := f.title(`SELECT title || ': ' || overview FROM items WHERE kind = 'episode'`); got != "The Target: Baltimore." {
		t.Errorf("episode = %q, want its NFO's title and plot", got)
	}
	if n := f.count(`SELECT count(*) FROM external_ids WHERE provider = 'tvdb' AND value = '79126' AND source = 'nfo'`); n != 1 {
		t.Error("the show's TVDB id from its NFO was not kept")
	}

	f.write("the wire/tvshow.nfo", `<tvshow><title>The Wire (HBO)</title></tvshow>`)
	f.scan()
	if got := f.title(`SELECT title FROM items WHERE kind = 'show'`); got != "The Wire (HBO)" {
		t.Errorf("after editing tvshow.nfo, show title = %q", got)
	}
}

func TestFilmNFO(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Alien (1979)/Alien (1979) - 1080p.mkv", "alien")
	f.write("Alien (1979)/movie.nfo", `<movie><title>Alien</title><mpaa>Rated R</mpaa></movie>`)
	f.put("Heat (1995).mkv", "heat")
	f.write("Heat (1995).nfo", `<movie><title>Heat</title><tagline>A Los Angeles crime saga.</tagline></movie>`)
	f.scan()
	if got := f.title(`SELECT certificate FROM items WHERE title = 'Alien'`); got != "R" {
		t.Errorf("Alien's certificate = %q, want movie.nfo's", got)
	}
	if got := f.title(`SELECT tagline FROM items WHERE title = 'Heat'`); got != "A Los Angeles crime saga." {
		t.Errorf("Heat's tagline = %q, want its own NFO's", got)
	}
}

func TestNFOsNameSeasonsAndNumberEpisodes(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("the wire/Season 1/the.wire.s01e01.mkv", "e1")
	f.put("the wire/Season 2/the.wire.s02e01.mkv", "e2")
	f.write("the wire/tvshow.nfo", `<tvshow><title>The Wire</title><namedseason number="1">The Street</namedseason></tvshow>`)
	f.write("the wire/Season 2/season.nfo", `<season><title>The Port</title></season>`)
	f.write("the wire/Season 2/the.wire.s02e01.nfo", `<episodedetails><title>Ebb Tide</title><season>2</season><episode>1</episode></episodedetails>
<episodedetails><title>Collateral Damage</title><season>2</season><episode>2</episode></episodedetails>`)
	f.scan()
	if got := f.title(`SELECT string_agg(title, ', ' ORDER BY season_number) FROM items WHERE kind = 'season'`); got != "The Street, The Port" {
		t.Errorf("seasons = %q, want tvshow.nfo's name for 1 and season.nfo's for 2", got)
	}
	if got := f.title(`SELECT title || ' ' || episode_number || '-' || episode_end FROM items WHERE kind = 'episode' AND season_number = 2`); got != "Ebb Tide / Collateral Damage 1-2" {
		t.Errorf("season 2's episode = %q, want both NFO episodes as 1-2", got)
	}

	f.write("the wire/tvshow.nfo", `<tvshow><title>The Wire</title><namedseason number="1">Baltimore</namedseason></tvshow>`)
	f.scan()
	if got := f.title(`SELECT title FROM items WHERE kind = 'season' AND season_number = 1`); got != "Baltimore" {
		t.Errorf("after renaming season 1 in tvshow.nfo, it is %q", got)
	}
}

func (f *fixture) id(sql string) uuid.UUID {
	f.t.Helper()
	var s string
	if err := f.db.QueryRow(f.t.Context(), sql).Scan(&s); err != nil {
		f.t.Fatal(err)
	}
	return uuid.MustParse(s)
}

func TestTitlePagesShowWhatTheScanFound(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Lawrence of Arabia (1962)/Lawrence of Arabia (1962) - 2160p cd1.mkv", "l1")
	f.put("Lawrence of Arabia (1962)/Lawrence of Arabia (1962) - 2160p cd2.mkv", "l2")
	f.put("Lawrence of Arabia (1962)/Lawrence of Arabia (1962) - 1080p.mkv", "l3")
	f.put("Lawrence of Arabia (1962)/Lawrence of Arabia (1962) - 1080p.en.srt", "sub")
	f.put("Lawrence of Arabia (1962)/trailers/Teaser.mkv", "teaser")
	f.scan()
	page, err := f.st.Title(t.Context(), uuid.UUID{}, f.id(`SELECT id::text FROM items WHERE kind = 'movie'`))
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Lawrence of Arabia" || page.Year != 1962 || len(page.Versions) != 2 {
		t.Fatalf("page = %q %d with %d versions, want Lawrence of Arabia 1962 in two", page.Title, page.Year, len(page.Versions))
	}
	long, short := page.Versions[0], page.Versions[1]
	if long.Label != "2160p" || long.Parts != 2 || long.DurationMS != 4*3600*1000 {
		t.Errorf("first version = %q in %d parts, %d ms; want the two-part 2160p copy, longest first", long.Label, long.Parts, long.DurationMS)
	}
	if len(long.Chapters) != 2 || long.Chapters[1].StartMS != 2*3600*1000 || long.Chapters[1].Part != long.Files[1].ID || long.Chapters[1].Idx != 0 {
		t.Errorf("chapters = %+v, want one per part on one timeline, each the first of its own part", long.Chapters)
	}
	if len(long.Streams) != 2 || long.Streams[0].Range != domain.RangeHDR10 {
		t.Errorf("streams = %+v, want the first part's HDR10 video and its audio", long.Streams)
	}
	if len(short.Subtitles) != 1 || short.Subtitles[0].Language != "en" || len(long.Subtitles) != 0 {
		t.Errorf("subtitles = %+v and %+v, want the English file on the 1080p copy alone", long.Subtitles, short.Subtitles)
	}
	if len(page.Extras) != 1 || page.Extras[0].Kind != domain.ExtraTrailer {
		t.Errorf("extras = %+v, want the trailer", page.Extras)
	}
}

func TestAShowsPageListsItsSeasonsAndASeasonsItsEpisodes(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("The Wire/Season 1/The Wire S01E02.mkv", "e2")
	f.put("The Wire/Season 1/The Wire S01E01.mkv", "e1")
	f.put("The Wire/Season 2/The Wire S02E01.mkv", "e3")
	f.scan()
	show, err := f.st.Title(t.Context(), uuid.UUID{}, f.id(`SELECT id::text FROM items WHERE kind = 'show'`))
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Seasons) != 2 || show.Seasons[0].Episodes != 2 || show.Seasons[1].Number != 2 {
		t.Fatalf("seasons = %+v, want season 1 with two episodes, then season 2", show.Seasons)
	}
	season, err := f.st.Title(t.Context(), uuid.UUID{}, show.Seasons[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if season.Show == nil || season.Show.ID != show.ID || len(season.Episodes) != 2 || *season.Episodes[0].Number != 1 {
		t.Errorf("season page = %+v, want its show and episodes 1 and 2 in order", season)
	}
	episode, err := f.st.Title(t.Context(), uuid.UUID{}, season.Episodes[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if episode.Season == nil || episode.Show == nil || episode.Show.Title != "The Wire" || len(episode.Versions) != 1 {
		t.Errorf("episode page = %+v, want its season, its show and its copy", episode)
	}
}

func (f *fixture) pictures(kind string) []string {
	f.t.Helper()
	rows, err := f.db.Query(f.t.Context(), `SELECT i.title || ' ' || a.kind || ' ' || a.place FROM artwork a
		JOIN items i ON i.id = a.item_id WHERE i.kind = $1 ORDER BY 1`, kind)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func TestPicturesBesideFilmsAreTheirs(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	poster := image.NewGray(image.Rect(0, 0, 20, 30))
	for n := range poster.Pix {
		poster.Pix[n] = uint8(n)
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, poster, nil); err != nil {
		t.Fatal(err)
	}
	f.put("Heat (1995)/poster.jpg", jpg.String())
	f.put("Heat (1995)/fanart.jpg", "b")
	f.put("Heat (1995)/Heat (1995)-clearlogo.png", "l")
	f.put("Alien (1979).mkv", "alien")
	f.put("Alien (1979).jpg", "a")
	f.put("folder.jpg", "root")
	f.scan()
	want := []string{
		"Alien poster Alien (1979).jpg",
		"Heat backdrop Heat (1995)/fanart.jpg",
		"Heat logo Heat (1995)/Heat (1995)-clearlogo.png",
		"Heat poster Heat (1995)/poster.jpg",
	}
	if got := f.pictures("movie"); !slices.Equal(got, want) {
		t.Errorf("pictures = %q, want %q", got, want)
	}
	rows, err := f.db.Query(t.Context(), `SELECT place FROM artwork WHERE blurhash IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	hashed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hashed, []string{"Heat (1995)/poster.jpg"}) {
		t.Errorf("pictures with a blurhash = %q, want the one that is a picture", hashed)
	}
}

func TestPicturesOfAShowItsSeasonsAndEpisodes(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	f.put("The Wire/Season 1/The Wire S01E01.mkv", "e1")
	f.put("The Wire/Season 1/The Wire S01E01.jpg", "still")
	f.put("The Wire/Season 1/folder.jpg", "s1")
	f.put("The Wire/poster.jpg", "show")
	f.put("The Wire/season01-poster.jpg", "s1root")
	f.scan()
	check := func(kind string, want ...string) {
		t.Helper()
		if got := f.pictures(kind); !slices.Equal(got, want) {
			t.Errorf("%s pictures = %q, want %q", kind, got, want)
		}
	}
	check("show", "The Wire poster The Wire/poster.jpg")
	check("season", "Season 1 poster The Wire/Season 1/folder.jpg", "Season 1 poster The Wire/season01-poster.jpg")
	check("episode", "Episode 1 thumb The Wire/Season 1/The Wire S01E01.jpg")

	if err := os.Remove(filepath.Join(f.root, "The Wire", "season01-poster.jpg")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	check("season", "Season 1 poster The Wire/Season 1/folder.jpg")
	check("show", "The Wire poster The Wire/poster.jpg")
}

func TestAScanTellsHowFarItHasGot(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Alien (1979)/Alien (1979).mkv", "alien")
	f.put("Ignored/.ignore", "")
	var told []domain.ScanProgress
	if _, err := f.scanner.Scan(t.Context(), f.lib, []string{"."}, func(p domain.ScanProgress) { told = append(told, p) }, func(store.Changed) {}); err != nil {
		t.Fatal(err)
	}
	// The library's folders are read at once, so either may be told first.
	var folders []string
	for i := range told {
		folders = append(folders, told[i].Folder)
		told[i].Folder = ""
	}
	want := []domain.ScanProgress{
		{Library: f.lib.ID, Phase: domain.ScanReading, Done: 1, Known: 4},
		{Library: f.lib.ID, Phase: domain.ScanReading, Done: 2, Known: 4},
		{Library: f.lib.ID, Phase: domain.ScanReading, Done: 3, Known: 4},
		{Library: f.lib.ID, Phase: domain.ScanRemoving, Done: 3, Known: 3},
	}
	if !slices.Equal(told, want) {
		t.Errorf("told %+v, want %+v", told, want)
	}
	if want := []string{"", "", "Alien (1979)", "Heat (1995)"}; !slices.Equal(slices.Sorted(slices.Values(folders)), want) {
		t.Errorf("told folders %q, want %q", folders, want)
	}
}

func TestAScanTellsWhichTitlesItChanged(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Alien (1979)/Alien (1979).mkv", "alien")
	scan := func() map[domain.TitleChange][]string {
		t.Helper()
		got := map[domain.TitleChange][]string{}
		_, err := f.scanner.Scan(t.Context(), f.lib, []string{"."}, func(domain.ScanProgress) {}, func(c store.Changed) {
			for change, ids := range c {
				for _, id := range ids {
					var title string
					if err := f.db.QueryRow(t.Context(), `SELECT title FROM items WHERE id = $1`, id.String()).Scan(&title); err != nil {
						t.Fatal(err)
					}
					got[change] = append(got[change], title)
				}
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, titles := range got {
			slices.Sort(titles)
		}
		return got
	}
	if got := scan(); !slices.Equal(got[domain.TitleAdded], []string{"Alien", "Heat"}) || len(got) != 1 {
		t.Errorf("first scan told %v, want Alien and Heat added", got)
	}
	f.put("Heat (1995)/Heat (1995) - 1080p.mkv", "heat-hd")
	if err := os.RemoveAll(filepath.Join(f.root, "Alien (1979)")); err != nil {
		t.Fatal(err)
	}
	// Alien's copy is missing, kept for a disk that comes back.
	if got := scan(); !slices.Equal(got[domain.TitleUpdated], []string{"Alien", "Heat"}) || len(got[domain.TitleAdded])+len(got[domain.TitleRemoved]) != 0 {
		t.Errorf("second scan told %v, want Heat and Alien updated", got)
	}
	if got := scan(); len(got[domain.TitleUpdated])+len(got[domain.TitleAdded])+len(got[domain.TitleRemoved]) != 0 {
		t.Errorf("an unchanged library told %v, want nothing", got)
	}
}

func TestAddingAnEpisodeReadsThatFileAlone(t *testing.T) {
	f := newFixture(t, domain.LibraryShows)
	season := "The Wire (2002)/Season 1/"
	f.put(season+"The Wire S01E01.mkv", "s1e1")
	f.put(season+"The Wire S01E02.mkv", "s1e2")
	f.scan()
	// Its neighbours cannot be read now, so a scan that opened them would leave them out.
	for _, name := range []string{"The Wire S01E01.mkv", "The Wire S01E02.mkv"} {
		if err := os.Chmod(filepath.Join(f.root, season, name), 0); err != nil {
			t.Fatal(err)
		}
	}
	f.put(season+"The Wire S01E03.mkv", "s1e3")
	if r := f.scan(); r.Probed != 1 || r.Skipped != 0 {
		t.Errorf("adding an episode: %+v, want the new file probed and nothing left out", r)
	}
	if n := f.count(`SELECT count(*) FROM items WHERE kind = 'episode'`); n != 3 {
		t.Errorf("%d episodes, want 3", n)
	}

	// A file only touched is read again for its content key, and is still not probed.
	touched := filepath.Join(f.root, season, "The Wire S01E01.mkv")
	if err := os.Chmod(touched, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(touched, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if r := f.scan(); r.Probed != 0 || r.Skipped != 0 {
		t.Errorf("touching an episode: %+v, want nothing probed or left out", r)
	}
}

// gatheringProber answers a probe only once readsAtOnce probes are under way, and fails one left
// waiting alone.
type gatheringProber struct {
	mu      sync.Mutex
	waiting int
	all     chan struct{}
}

func (p *gatheringProber) Probe(ctx context.Context, f *os.File) (domain.Facts, error) {
	p.mu.Lock()
	if p.waiting++; p.waiting == readsAtOnce {
		close(p.all)
	}
	p.mu.Unlock()
	select {
	case <-p.all:
		return fakeProber{}.Probe(ctx, f)
	case <-time.After(5 * time.Second):
		return domain.Facts{}, errors.New("probed alone")
	}
}

func TestAScanReadsSeveralFilesAtOnce(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.scanner = New(f.st, &gatheringProber{all: make(chan struct{})}, slog.New(slog.DiscardHandler))
	for i := range readsAtOnce {
		name := fmt.Sprintf("Film %d (2000)", i)
		f.put(name+"/"+name+".mkv", name)
	}
	if r := f.scan(); r.Skipped != 0 || r.Probed != readsAtOnce {
		t.Errorf("%+v, want every film probed, at once", r)
	}
}

func TestAScanOfAFolderReadsAndSettlesThatFolderAlone(t *testing.T) {
	f := newFixture(t, domain.LibraryMovies)
	f.put("Heat (1995)/Heat (1995).mkv", "heat")
	f.put("Alien (1979)/Alien (1979).mkv", "alien")
	f.scan()
	f.put("Heat (1995)/Heat (1995) - 1080p.mkv", "heat-hd")
	if err := os.RemoveAll(filepath.Join(f.root, "Alien (1979)")); err != nil {
		t.Fatal(err)
	}
	scan := func(folders ...string) Report {
		t.Helper()
		r, err := f.scanner.Scan(t.Context(), f.lib, folders, func(domain.ScanProgress) {}, func(store.Changed) {})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := scan("Heat (1995)"); r.Folders != 1 || r.Probed != 1 {
		t.Errorf("scanning Heat's folder: %+v, want it alone read and its new copy probed", r)
	}
	if n := f.count(`SELECT count(*) FROM versions WHERE missing_since IS NOT NULL`); n != 0 {
		t.Error("scanning Heat's folder marked Alien, outside it, missing")
	}
	// Alien's folder is gone; scanning it finds its copy nowhere.
	scan("Alien (1979)")
	if n := f.count(`SELECT count(*) FROM versions v JOIN items i ON i.id = v.item_id
		WHERE i.title = 'Alien' AND v.missing_since IS NOT NULL`); n != 1 {
		t.Error("scanning a removed folder did not mark its copy missing")
	}
	if n := f.count(`SELECT count(*) FROM versions WHERE missing_since IS NULL`); n != 2 {
		t.Errorf("%d copies of Heat present, want 2", n)
	}
}
