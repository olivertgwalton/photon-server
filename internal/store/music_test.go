//go:build integration

package store

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
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
// show, so a list of either says whose it is; a track wears its album's cover, and says, as its
// page does, that it is explicit.
func TestAMusicCardNamesWhoseItIs(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	timeless := track("Timeless", 2, 2, "The Weeknd", "Playboi Carti")
	timeless.Advisory = domain.AdvisoryExplicit
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow",
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Hurry Up Tomorrow/cover.jpg"}},
		Tracks:  []Track{timeless},
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
	page, err := s.Title(ctx, uuid.UUID{}, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if track.Advisory != domain.AdvisoryExplicit || page.Advisory != domain.AdvisoryExplicit || album.Advisory != "" {
		t.Errorf("Timeless's card says %q and its page %q, its album %q; want explicit, explicit and nothing",
			track.Advisory, page.Advisory, album.Advisory)
	}
	if album.Poster == (uuid.UUID{}) || track.Poster != album.Poster {
		t.Errorf("Timeless wears poster %v, its album has %v; want the album's cover", track.Poster, album.Poster)
	}
}

// A track's best picture is the cover it wears, its album's, as an app asking for the track's
// picture by the track alone is answered, as Finamp's player asks.
func TestATracksBestPictureIsItsAlbumsCover(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow",
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, Path: "Hurry Up Tomorrow/cover.jpg"}},
		Tracks:  []Track{track("Sao Paulo", 1, 4, "The Weeknd", "Anitta")},
	}}); err != nil {
		t.Fatal(err)
	}
	var trackID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'track'`).Scan(&trackID); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(ctx, uuid.UUID{}, []uuid.UUID{trackID})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	best, err := s.BestPicture(ctx, trackID, []domain.ArtworkKind{domain.ArtworkPoster, domain.ArtworkThumb})
	if err != nil || cards[0].Poster == (uuid.UUID{}) || best != cards[0].Poster {
		t.Errorf("the track's best picture: %v, %v; want the cover its card wears, %v", best, err, cards[0].Poster)
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
// ranks for them, by a job queued as it is found; what a source said is kept, timed, and a none is
// believed for a while. A full refresh asks again, the lyrics standing until it is answered, and a
// track whose own file carries lyrics asks no source.
func TestATracksLyricsAreAskedAndKept(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	sung := track("Cry For Me", 1, 2, "The Weeknd")
	sung.Version.Lyrics = domain.Untimed
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Folder: "Hurry Up Tomorrow",
		Tracks: []Track{track("Timeless", 1, 1, "The Weeknd", "Playboi Carti"), sung},
	}}); err != nil {
		t.Fatal(err)
	}
	var id, own uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'track' AND title = 'Timeless'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items WHERE kind = 'track' AND title = 'Cry For Me'`).Scan(&own); err != nil {
		t.Fatal(err)
	}
	queued := func() []uuid.UUID {
		t.Helper()
		ids, err := queryColumn[uuid.UUID](ctx, s.pool, `SELECT subject FROM jobs WHERE kind = 'lyrics'`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE kind = 'lyrics'`); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	timings := func() map[uuid.UUID]domain.LyricsTiming {
		t.Helper()
		m, err := s.lyricsTimings(ctx, []*model.Item{{ID: id, Kind: domain.ItemTrack}, {ID: own, Kind: domain.ItemTrack}})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if got := queued(); !slices.Equal(got, []uuid.UUID{id}) {
		t.Errorf("lyrics asked for %v, want for Timeless alone, as Cry For Me carries its own", got)
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
	if _, current, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || current {
		t.Errorf("before asking: current %v, %v", current, err)
	}

	if err := s.KeepLyrics(ctx, id, domain.SourceLRCLIB, "", ""); err != nil {
		t.Fatal(err)
	}
	if text, current, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || !current || text != "" {
		t.Errorf("a none: %q %v %v, want it current", text, current, err)
	}
	if err := s.AskLyrics(ctx, id); err != nil || len(queued()) != 0 {
		t.Errorf("a none just answered is asked again, or %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE fetched_lyrics SET fetched_at = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	if _, current, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || current {
		t.Errorf("a none a month old is still believed, or %v", err)
	}
	if err := s.AskLyrics(ctx, id); err != nil || len(queued()) != 1 {
		t.Errorf("a none a month old is not asked again, or %v", err)
	}

	if err := s.KeepLyrics(ctx, id, domain.SourceLRCLIB, "[00:01.00]Timeless", domain.TimedByLine); err != nil {
		t.Fatal(err)
	}
	if text, current, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB); err != nil || !current || text != "[00:01.00]Timeless" {
		t.Errorf("lyrics found: %q %v %v", text, current, err)
	}
	wantTimings := map[uuid.UUID]domain.LyricsTiming{id: domain.TimedByLine, own: domain.Untimed}
	if diff := cmp.Diff(wantTimings, timings()); diff != "" {
		t.Errorf("timings (-want +got):\n%s", diff)
	}

	if err := s.RefreshLibrary(ctx, lib.ID, domain.RefreshAll); err != nil {
		t.Fatal(err)
	}
	text, current, err := s.KeptLyrics(ctx, id, domain.SourceLRCLIB)
	if err != nil || current || text != "[00:01.00]Timeless" || !slices.Equal(queued(), []uuid.UUID{id}) {
		t.Errorf("after a full refresh: %q, current %v, %v; want them asked again, standing meanwhile", text, current, err)
	}
	if diff := cmp.Diff(wantTimings, timings()); diff != "" {
		t.Errorf("timings while asked again (-want +got):\n%s", diff)
	}
}

// An album is favourited but not watched nor put on a watchlist, so its wall is offered that alone.
func TestAnAlbumWallIsOfferedFavouritesAlone(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Music", domain.LibraryMusic, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveMusicFolder(ctx, lib.ID, "Hurry Up Tomorrow", []byte("seen"), []Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Year: 2025, Folder: "Hurry Up Tomorrow",
		Tracks: []Track{track("Wake Me Up", 1, 1, "The Weeknd")},
	}}); err != nil {
		t.Fatal(err)
	}
	facets, err := s.Facets(ctx, []uuid.UUID{lib.ID}, uuid.UUID{}, domain.WallAlbums)
	if err != nil || !slices.Equal(facets.Marks, []domain.Mark{domain.MarkFavourite}) {
		t.Errorf("an album wall's marks = %v, %v; want favourites alone", facets.Marks, err)
	}
}
