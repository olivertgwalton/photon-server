//go:build integration

package scan

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

type countingProber struct{ probes int }

func (p *countingProber) Probe(context.Context, *os.File) (media.Facts, error) {
	p.probes++
	return media.Facts{
		Container: "matroska,webm",
		Duration:  2 * time.Hour,
		Streams: []media.Stream{
			{Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Width: 3840, Height: 2160, Range: domain.RangeHDR10},
			{Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8},
		},
		Chapters: []media.Chapter{{Start: 0, End: time.Hour, Title: "One"}},
	}, nil
}

type fixture struct {
	t       *testing.T
	root    string
	lib     domain.Library
	db      *pgx.Conn
	scanner *Scanner
	prober  *countingProber
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
	p := &countingProber{}
	return &fixture{t: t, root: root, lib: lib, db: db, scanner: New(st, p, log), prober: p}
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
	r, err := f.scanner.Scan(f.t.Context(), f.lib)
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
	if n := f.count(`SELECT count(*) FROM jobs`); n != 1 {
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
	if _, err := f.scanner.Scan(t.Context(), lib); err != nil {
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

	if r := f.scan(); r.Probed != 5 {
		t.Errorf("first scan probed %d files, want 5", r.Probed)
	}
	for _, c := range []struct {
		what string
		sql  string
		want int
	}{
		{"shows", `SELECT count(*) FROM items WHERE kind = 'show' AND title = 'The Wire' AND year = 2002`, 1},
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
