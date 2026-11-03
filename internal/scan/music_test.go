//go:build integration

package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/testtool"
)

// track writes a second of sound at rel, tagged with tags, "KEY=value" each.
func (f *fixture) track(rel string, tags ...string) {
	f.t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	m := map[string][]string{}
	for _, tag := range tags {
		k, v, _ := strings.Cut(tag, "=")
		m[k] = append(m[k], v)
	}
	testtool.Audio(f.t, p, m, nil)
}

// music is the library's tracks, "artist / album / disc.number title", in order.
func (f *fixture) music() string {
	f.t.Helper()
	rows, err := f.db.Query(f.t.Context(), `
		SELECT ar.title || ' / ' || al.title || ' / ' || coalesce(t.disc_number, 0) || '.' || coalesce(t.track_number, 0) || ' ' || t.title
		FROM items t JOIN items al ON al.id = t.parent_id JOIN items ar ON ar.id = al.parent_id
		WHERE t.kind = 'track' ORDER BY 1`)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// Two discs in one folder, numbered as a release of several discs numbers them and tagged with
// no disc, are one album of two discs, as the test album's rip is.
func TestTheDiscsOfAnAlbumNumberedByItsFilesAreOneAlbum(t *testing.T) {
	f := newFixture(t, domain.LibraryMusic)
	album := "The_Weeknd-Hurry_Up_Tomorrow-2CD-FLAC-2025/"
	f.track(album+"101-the_weeknd-wake_me_up.flac", "TITLE=Wake Me Up", "ARTIST=The Weeknd", "ALBUM=Hurry Up Tomorrow", "TRACKNUMBER=1")
	f.track(album+"102-the_weeknd-cry_for_me.flac", "TITLE=Cry for Me", "ARTIST=The Weeknd", "ALBUM=Hurry Up Tomorrow", "TRACKNUMBER=2")
	f.track(album+"201-the_weeknd-i_cant_wait.flac", "TITLE=I Can't Wait", "ARTIST=The Weeknd", "ALBUM=Hurry Up Tomorrow", "TRACKNUMBER=1")
	f.track(album+"202-the_weeknd-timeless.flac", "TITLE=Timeless", "ARTIST=The Weeknd", "ARTIST=Playboi Carti", "ALBUM=Hurry Up Tomorrow")
	f.scan()
	want := `The Weeknd / Hurry Up Tomorrow / 1.1 Wake Me Up
The Weeknd / Hurry Up Tomorrow / 1.2 Cry for Me
The Weeknd / Hurry Up Tomorrow / 2.1 I Can't Wait
The Weeknd / Hurry Up Tomorrow / 2.2 Timeless`
	if got := f.music(); got != want {
		t.Errorf("tracks:\n%s\nwant:\n%s", got, want)
	}
	if n := f.count(`SELECT count(*) FROM items WHERE kind = 'track' AND artists = '["The Weeknd", "Playboi Carti"]'`); n != 1 {
		t.Error("Timeless does not name both its artists")
	}
	if n := f.count(`SELECT count(*) FROM jobs WHERE kind IN ('keyframes', 'previews', 'markers')`); n != 0 {
		t.Errorf("%d jobs queued for pictures a track does not have, want none", n)
	}
	if r := f.scan(); r.Probed != 0 || r.Unchanged != r.Folders {
		t.Errorf("rescanning unchanged music: %+v, want no probes and every folder unchanged", r)
	}
}

// An album's discs in folders of their own are one album, under the artist its tags name, and
// an album of several artists naming none is various artists'.
func TestAnAlbumsDiscFoldersAreOneAlbumAndACompilationIsVariousArtists(t *testing.T) {
	f := newFixture(t, domain.LibraryMusic)
	f.track("Pink Floyd/The Wall/CD1/01 In the Flesh.flac", "TITLE=In the Flesh?", "ALBUMARTIST=Pink Floyd", "ALBUM=The Wall", "TRACKNUMBER=1")
	f.track("Pink Floyd/The Wall/CD2/01 Hey You.flac", "TITLE=Hey You", "ALBUMARTIST=Pink Floyd", "ALBUM=The Wall", "TRACKNUMBER=1")
	f.track("Now 1/01.flac", "TITLE=One", "ARTIST=Alpha", "ALBUM=Now 1")
	f.track("Now 1/02.flac", "TITLE=Two", "ARTIST=Beta", "ALBUM=Now 1")
	f.track("Loose/03 - untagged_song.mp3")
	f.scan()
	want := `Pink Floyd / The Wall / 1.1 In the Flesh?
Pink Floyd / The Wall / 2.1 Hey You
Unknown Artist / Loose / 0.3 untagged song
Various Artists / Now 1 / 0.1 One
Various Artists / Now 1 / 0.2 Two`
	if got := f.music(); got != want {
		t.Errorf("tracks:\n%s\nwant:\n%s", got, want)
	}
	if n := f.count(`SELECT count(*) FROM items WHERE kind = 'artist'`); n != 3 {
		t.Errorf("%d artists, want 3", n)
	}
}

// A track gone, from a library that forgets what it no longer finds, takes its album and artist with
// it once they hold nothing.
func TestAnEmptiedAlbumAndArtistGo(t *testing.T) {
	f := newFixture(t, domain.LibraryMusic)
	if _, err := f.db.Exec(t.Context(), `UPDATE libraries SET missing_versions = $1`, domain.MissingForgotten); err != nil {
		t.Fatal(err)
	}
	f.track("Adele/25/01 Hello.flac", "TITLE=Hello", "ALBUMARTIST=Adele", "ALBUM=25", "TRACKNUMBER=1")
	f.track("Adele/21/01 Rolling.flac", "TITLE=Rolling in the Deep", "ALBUMARTIST=Adele", "ALBUM=21", "TRACKNUMBER=1")
	f.scan()
	if err := os.RemoveAll(filepath.Join(f.root, "Adele/25")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if got, want := f.music(), "Adele / 21 / 0.1 Rolling in the Deep"; got != want {
		t.Errorf("tracks: %s, want %s", got, want)
	}
	if err := os.RemoveAll(filepath.Join(f.root, "Adele")); err != nil {
		t.Fatal(err)
	}
	f.scan()
	if n := f.count(`SELECT count(*) FROM items`); n != 0 {
		t.Errorf("%d items left of an emptied library, want none", n)
	}
}

// The MusicBrainz ids Picard tags an album and its artist with are theirs, and a new artist and
// album are asked of their providers.
func TestAnAlbumKeepsTheMusicBrainzIdsItsTagsCarry(t *testing.T) {
	f := newFixture(t, domain.LibraryMusic)
	f.track("Hurry Up Tomorrow/01.flac", "TITLE=Wake Me Up", "ALBUMARTIST=The Weeknd", "ALBUM=Hurry Up Tomorrow",
		"MUSICBRAINZ_RELEASEGROUPID=e35b2a1c-4d0a-4b56-9e7d-5f5d0a6e5a11", "MUSICBRAINZ_ALBUMARTISTID=c8b03190-306c-4120-bb0b-6f2ebfc06ea9")
	f.scan()
	for kind, mbid := range map[string]string{"album": "e35b2a1c-4d0a-4b56-9e7d-5f5d0a6e5a11", "artist": "c8b03190-306c-4120-bb0b-6f2ebfc06ea9"} {
		if n := f.count(`SELECT count(*) FROM external_ids x JOIN items i ON i.id = x.item_id
			WHERE i.kind = '` + kind + `' AND x.provider = 'musicbrainz' AND x.source = 'tags' AND x.value = '` + mbid + `'`); n != 1 {
			t.Errorf("the %s does not keep its MusicBrainz id from its tags", kind)
		}
	}
	if n := f.count(`SELECT count(*) FROM jobs j JOIN items i ON i.id = j.subject WHERE j.kind = 'identify' AND i.kind IN ('artist', 'album')`); n != 2 {
		t.Errorf("%d identify jobs for the artist and album, want 2", n)
	}
}

// A track's lyrics in an LRC file beside it, or else in its tags, are its own, timed as they are
// written; only a track carrying none is asked of its library's lyrics source.
func TestATrackCarriesItsOwnLyricsOrAsksForThem(t *testing.T) {
	f := newFixture(t, domain.LibraryMusic)
	f.track("Album/01.flac", "TITLE=Timed", "ARTIST=A", "ALBUM=Album", "LYRICS=Not these")
	if err := os.WriteFile(filepath.Join(f.root, "Album/01.lrc"), []byte("[00:01.00]Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.track("Album/02.flac", "TITLE=Tagged", "ARTIST=A", "ALBUM=Album", "LYRICS=Hello")
	f.track("Album/03.flac", "TITLE=Bare", "ARTIST=A", "ALBUM=Album")
	f.scan()
	rows, err := f.db.Query(t.Context(), `
		SELECT i.title || ' ' || coalesce(v.lyrics, 'none') || ' ' || count(j.subject)
		FROM items i JOIN versions v ON v.item_id = i.id LEFT JOIN jobs j ON j.subject = i.id AND j.kind = 'lyrics'
		WHERE i.kind = 'track' GROUP BY i.title, v.lyrics ORDER BY i.title`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if want := "Bare none 1\nTagged untimed 0\nTimed line 0"; strings.Join(got, "\n") != want {
		t.Errorf("tracks' own lyrics and fetches:\n%s\nwant:\n%s", strings.Join(got, "\n"), want)
	}
}
