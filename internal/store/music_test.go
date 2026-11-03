//go:build integration

package store

import (
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func track(title string, disc, number int, artists ...string) Track {
	rel := "Hurry Up Tomorrow/" + title + ".flac"
	return Track{
		Title: title, Disc: disc, Number: number, Artists: artists, Folder: "Hurry Up Tomorrow",
		Version: Version{ContentKey: []byte(rel), Parts: []Part{{
			RelPath: rel, Size: 1, ModTime: time.Unix(0, 0),
			Facts: &domain.Facts{Duration: 4 * time.Minute, Streams: []domain.Stream{{Kind: domain.StreamAudio, Codec: "flac"}}},
		}}},
	}
}

// An artist's page lists its albums, an album's its tracks by disc and number, and a track's names
// the album and artist it is under.
func TestAnAlbumsPageListsItsTracksInOrder(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Year: 2025, Folder: "Hurry Up Tomorrow",
		Tracks: []Track{
			track("Timeless", 2, 2, "The Weeknd", "Playboi Carti"),
			track("Cry for Me", 1, 2, "The Weeknd"),
			track("Wake Me Up", 1, 1, "The Weeknd", "Justice"),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var artist uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'artist'`).Scan(&artist); err != nil {
		t.Fatal(err)
	}
	p, err := s.Title(ctx, uuid.UUID{}, artist)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Albums) != 1 || p.Albums[0].Title != "Hurry Up Tomorrow" || p.Albums[0].Year != 2025 {
		t.Fatalf("The Weeknd's albums: %+v, want Hurry Up Tomorrow, 2025", p.Albums)
	}
	album, err := s.Title(ctx, uuid.UUID{}, p.Albums[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if album.Artist == nil || album.Artist.ID != artist {
		t.Errorf("the album's artist: %+v, want The Weeknd", album.Artist)
	}
	type listed struct {
		Disc, Number int
		Title        string
		Artists      []string
		DurationMS   int64
	}
	var got []listed
	for _, c := range album.Tracks {
		got = append(got, listed{*c.DiscNumber, *c.TrackNumber, c.Title, c.Artists, c.DurationMS})
	}
	want := []listed{
		{1, 1, "Wake Me Up", []string{"The Weeknd", "Justice"}, 240_000},
		{1, 2, "Cry for Me", []string{"The Weeknd"}, 240_000},
		{2, 2, "Timeless", []string{"The Weeknd", "Playboi Carti"}, 240_000},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tracks (-want +got):\n%s", diff)
	}
	timeless, err := s.Title(ctx, uuid.UUID{}, album.Tracks[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if timeless.Album == nil || timeless.Album.ID != album.ID || timeless.Artist == nil || timeless.Artist.ID != artist ||
		*timeless.DiscNumber != 2 || len(timeless.Versions) != 1 {
		t.Errorf("Timeless's page: album %+v, artist %+v, disc %v, %d versions; want its album, artist, disc 2 and its file",
			timeless.Album, timeless.Artist, timeless.DiscNumber, len(timeless.Versions))
	}
}

// A track's card names its album and artist, and an album's its artist, as an episode's names its
// show, so a list of either says whose it is; a track wears its album's cover.
func TestAMusicCardNamesWhoseItIs(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow",
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Hurry Up Tomorrow/cover.jpg"}},
		Tracks:  []Track{track("Timeless", 2, 2, "The Weeknd", "Playboi Carti")},
	}}); err != nil {
		t.Fatal(err)
	}
	var trackID, albumID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT t.id, t.parent_id FROM items t WHERE t.kind = 'track'`).Scan(&trackID, &albumID); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(ctx, uuid.UUID{}, []uuid.UUID{trackID, albumID})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 {
		t.Fatalf("%d cards, want 2", len(cards))
	}
	track, album := cards[0], cards[1]
	if track.Album == nil || track.Album.Title != "Hurry Up Tomorrow" || track.Artist == nil || track.Artist.Title != "The Weeknd" ||
		*track.DiscNumber != 2 || *track.TrackNumber != 2 || len(track.Artists) != 2 {
		t.Errorf("Timeless's card: album %+v, artist %+v, disc %v, track %v, artists %v; want Hurry Up Tomorrow, The Weeknd, 2.2 and both artists",
			track.Album, track.Artist, track.DiscNumber, track.TrackNumber, track.Artists)
	}
	if album.Artist == nil || album.Artist.Title != "The Weeknd" || album.Album != nil {
		t.Errorf("the album's card: artist %+v, album %+v; want The Weeknd and no album", album.Artist, album.Album)
	}
	if album.Poster == (uuid.UUID{}) || track.Poster != album.Poster {
		t.Errorf("Timeless wears poster %v, its album has %v; want the album's cover", track.Poster, album.Poster)
	}
}

// A refresh of a music library asks its providers of its artists and albums again, and not of its
// tracks, which their tags alone describe.
func TestAMusicLibrarysRefreshAsksAgainOfItsArtistsAndAlbums(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow", Tracks: []Track{track("Timeless", 2, 2)},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshLibrary(ctx, lib.ID, domain.RefreshAll); err != nil {
		t.Fatal(err)
	}
	var kinds []domain.ItemKind
	rows, err := s.pool.Query(ctx, `SELECT i.kind FROM jobs j JOIN items i ON i.id = j.subject WHERE j.kind = 'identify' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k domain.ItemKind
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, k)
	}
	if want := []domain.ItemKind{domain.ItemAlbum, domain.ItemArtist}; !cmp.Equal(kinds, want) {
		t.Errorf("refresh asked of %v, want %v", kinds, want)
	}
}

// A track keeps the ReplayGain its tags give, which its page and its playback answer, and takes the
// gain they give when it is tagged again.
func TestATrackKeepsItsReplayGain(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	save := func(gain domain.ReplayGain) {
		t.Helper()
		tagged := track("Timeless", 1, 1, "The Weeknd")
		tagged.Version.Gain = gain
		_, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
			Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow", Tracks: []Track{tagged},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	gained := domain.ReplayGain{Track: &domain.Gain{DB: -6.48, Peak: 0.988556}, Album: &domain.Gain{DB: -7.1}}
	save(gained)
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'track'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(gained, page.Versions[0].Gain); diff != "" {
		t.Errorf("the page's gain (-want +got):\n%s", diff)
	}
	played, err := s.Playable(ctx, uuid.UUID{}, id, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(gained, played.Gain); diff != "" {
		t.Errorf("the playback's gain (-want +got):\n%s", diff)
	}

	again := domain.ReplayGain{Track: &domain.Gain{DB: -5}}
	save(again)
	played, err = s.Playable(ctx, uuid.UUID{}, id, uuid.UUID{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(again, played.Gain); diff != "" {
		t.Errorf("tagged again (-want +got):\n%s", diff)
	}
}

// A track's lyrics are asked by its artist, title, album and length, of the sources its library
// ranks for them; what a source said is kept, and a none is believed for a while.
func TestATracksLyricsAreAskedAndKept(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow",
		Tracks: []Track{track("Timeless", 1, 1, "The Weeknd", "Playboi Carti")},
	}}); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'track'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ask, err := s.LyricsAsk(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := LyricsAsk{
		Query:   domain.LyricsQuery{Artist: "The Weeknd", Title: "Timeless", Album: "Hurry Up Tomorrow", Duration: 4 * time.Minute},
		Sources: []domain.FieldSource{domain.SourceLRCLIB},
	}
	if diff := cmp.Diff(want, ask); diff != "" {
		t.Errorf("asked by (-want +got):\n%s", diff)
	}
	if _, kept, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || kept {
		t.Errorf("before asking: kept %v, %v", kept, err)
	}
	if err := s.KeepLyrics(ctx, id, domain.SourceLRCLIB, ""); err != nil {
		t.Fatal(err)
	}
	if text, kept, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || !kept || text != "" {
		t.Errorf("a none: %q %v %v, want it kept", text, kept, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE fetched_lyrics SET fetched_at = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	if _, kept, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || kept {
		t.Errorf("a none a month old is still believed, or %v", err)
	}
	if err := s.KeepLyrics(ctx, id, domain.SourceLRCLIB, "[00:01.00]Timeless"); err != nil {
		t.Fatal(err)
	}
	if text, kept, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || !kept || text != "[00:01.00]Timeless" {
		t.Errorf("lyrics found: %q %v %v", text, kept, err)
	}
}
