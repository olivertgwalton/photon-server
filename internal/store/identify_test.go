//go:build integration

package store

import (
	"maps"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
)

func TestIdentityDescribesAShowsEpisodes(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	episode := func(n int) Episode {
		key := []byte{byte(n)}
		return Episode{
			Season: 1, Episodes: []int{n}, Title: "the wire", Folder: "The Wire/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: key, Parts: []Part{{
				RelPath: "The Wire/Season 1/" + string(rune('0'+n)) + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
			}}}},
		}
	}
	show := Show{Title: "the wire", Folder: "The Wire", IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire/Season 1", []byte("v1"), show, []Episode{episode(1), episode(2)}, nil); err != nil {
		t.Fatal(err)
	}
	row := oneItem(t, s, "kind = 'show'")
	if n := countRows(t, s, `SELECT count(*) FROM jobs WHERE kind = 'identify' AND subject = $1`, row.ID); n != 1 {
		t.Errorf("%d identify jobs for the new show, want 1", n)
	}

	sub, ok, err := s.IdentifySubject(ctx, row.ID)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if sub.IDs[domain.ProviderTVDB] != "79126" || len(sub.Seasons) != 1 || sub.Seasons[0] != 1 {
		t.Errorf("subject = %+v, want the TVDB id and season 1", sub)
	}

	err = s.SaveIdentity(ctx, row.ID, domain.SourceTMDB, domain.Metadata{Title: "The Wire", IDs: map[domain.Provider]string{
		domain.ProviderTMDB: "1438", domain.ProviderTVDB: "1",
	}}, map[int]domain.SeasonMetadata{1: {
		Metadata: domain.Metadata{Title: "Book One: The Target", Overview: "The drug trade."},
		Episodes: map[int]domain.Metadata{1: {Title: "The Target"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := queryRows[model.Item](ctx, s.pool, `SELECT `+itemColumns+` FROM items`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		if it.Kind == domain.ItemSeason && (it.Overview == nil || *it.Overview != "The drug trade.") {
			t.Errorf("season overview %v, want the provider's", it.Overview)
		}
		key := string(it.Kind)
		if it.EpisodeNumber != nil {
			key += string(rune('0' + *it.EpisodeNumber))
		}
		got[key] = it.Title
	}
	want := map[string]string{"show": "The Wire", "season": "Season 1", "episode1": "The Target", "episode2": "the wire"}
	if !maps.Equal(got, want) {
		t.Errorf("titles = %v, want %v", got, want)
	}
	var tvdb string
	if err := s.pool.QueryRow(ctx, `SELECT value FROM external_ids WHERE item_id = $1 AND provider = 'tvdb'`, row.ID).Scan(&tvdb); err != nil || tvdb != "79126" {
		t.Errorf("a matched TVDB id replaced the folder's: %s, %v", tvdb, err)
	}

	seasons := func() []int {
		t.Helper()
		sub, _, err := s.IdentifySubject(ctx, row.ID)
		if err != nil {
			t.Fatal(err)
		}
		return sub.Seasons
	}
	if got := seasons(); !slices.Equal(got, []int{1}) {
		t.Errorf("with an episode still undescribed, seasons to ask for = %v, want [1]", got)
	}
	err = s.SaveIdentity(ctx, row.ID, domain.SourceTMDB, domain.Metadata{}, map[int]domain.SeasonMetadata{1: {
		Metadata: domain.Metadata{Title: "Season 1"}, Episodes: map[int]domain.Metadata{2: {Title: "The Detail"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := seasons(); len(got) != 0 {
		t.Errorf("with every episode described, seasons to ask for = %v, want none", got)
	}
	next := episode(3)
	next.Season, next.Folder = 2, "The Wire/Season 2"
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire/Season 2", []byte("v1"), show, []Episode{next}, nil); err != nil {
		t.Fatal(err)
	}
	if got := seasons(); !slices.Equal(got, []int{2}) {
		t.Errorf("after a new season, seasons to ask for = %v, want [2]", got)
	}
}

func TestALibraryKeepsTheVideoKindsItAsksFor(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "alien", Folder: "Alien", Copies: []Copy{{ContentKey: []byte("alien"), Parts: []Part{{
		RelPath: "Alien/Alien.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
	}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Alien", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item := oneItem(t, s, "true")
	match := domain.Metadata{Videos: []domain.RemoteVideo{
		{Kind: domain.ExtraTrailer, Site: "YouTube", Key: "trailer", Name: "Trailer"},
		{Kind: domain.ExtraBlooper, Site: "YouTube", Key: "gag", Name: "Gag reel"},
		{Kind: domain.ExtraOther, Site: "YouTube", Key: "titles", Name: "Titles"},
	}}
	kept := func() []string {
		t.Helper()
		if err := s.SaveIdentity(ctx, item.ID, domain.SourceTMDB, match, nil); err != nil {
			t.Fatal(err)
		}
		rows, err := s.pool.Query(ctx, `SELECT key FROM remote_videos ORDER BY position`)
		if err != nil {
			t.Fatal(err)
		}
		keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return keys
	}
	if got := kept(); !slices.Equal(got, []string{"trailer"}) {
		t.Errorf("by default, kept %v; want the trailer alone", got)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{RemoteExtras: []domain.ExtraKind{domain.ExtraBlooper, domain.ExtraOther}}); err != nil {
		t.Fatal(err)
	}
	if got := kept(); !slices.Equal(got, []string{"gag", "titles"}) {
		t.Errorf("keeping bloopers and other, kept %v; want those two in order and no trailer", got)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{RemoteExtras: []domain.ExtraKind{}}); err != nil {
		t.Fatal(err)
	}
	if got := kept(); len(got) != 0 {
		t.Errorf("keeping none, kept %v", got)
	}
}

func TestAMatchKeepsItsProvidersPictures(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	episode := Episode{
		Season: 1, Episodes: []int{1}, Title: "pilot", Folder: "The Wire/Season 1", ByNumber: true,
		Copies: []Copy{{ContentKey: []byte("e1"), Parts: []Part{{
			RelPath: "The Wire/Season 1/1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "The Wire/Season 1", []byte("v1"), Show{Title: "the wire", Folder: "The Wire"}, []Episode{episode}, nil); err != nil {
		t.Fatal(err)
	}
	show := oneItem(t, s, "kind = 'show'")
	match := func(posters ...string) {
		t.Helper()
		m := domain.Metadata{}
		for _, p := range posters {
			m.Artwork = append(m.Artwork, domain.Artwork{Kind: domain.ArtworkPoster, URL: p})
		}
		seasons := map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{
			1: {Artwork: []domain.Artwork{{Kind: domain.ArtworkThumb, URL: "still"}}},
		}}}
		if err := s.SaveIdentity(ctx, show.ID, domain.SourceTMDB, m, seasons); err != nil {
			t.Fatal(err)
		}
	}
	places := func() []string {
		t.Helper()
		rows, err := s.pool.Query(ctx, `SELECT place FROM artwork ORDER BY kind, position`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	match("one", "two")
	if got := places(); !slices.Equal(got, []string{"one", "two", "still"}) {
		t.Errorf("pictures = %v, want the show's two posters and the episode's still", got)
	}
	match("three")
	if got := places(); !slices.Equal(got, []string{"three", "still"}) {
		t.Errorf("after a second match, pictures = %v, want its poster alone and the still", got)
	}
}

func TestAVideoOnYouTubeIsPicturedByItsStill(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "alien", Folder: "Alien", Copies: []Copy{{ContentKey: []byte("alien"), Parts: []Part{{
		RelPath: "Alien/Alien.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
	}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Alien", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item := oneItem(t, s, "true")
	match := domain.Metadata{Videos: []domain.RemoteVideo{
		{Kind: domain.ExtraTrailer, Site: "YouTube", Key: "LjLamj-b0I8", Name: "Trailer"},
		{Kind: domain.ExtraTrailer, Site: "Vimeo", Key: "1234", Name: "Teaser"},
	}}
	videos := func() []VideoLink {
		t.Helper()
		if err := s.SaveIdentity(ctx, item.ID, domain.SourceTMDB, match, nil); err != nil {
			t.Fatal(err)
		}
		page, err := s.Title(ctx, uuid.UUID{}, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		return page.Videos
	}
	first := videos()
	if len(first) != 2 || first[0].Thumb == (uuid.UUID{}) || first[1].Thumb != (uuid.UUID{}) {
		t.Fatalf("videos %+v; want YouTube's pictured and Vimeo's not", first)
	}
	pic, err := s.Picture(ctx, first[0].Thumb)
	if err != nil || pic.URL != "https://i.ytimg.com/vi/LjLamj-b0I8/hqdefault.jpg" {
		t.Errorf("the still is %+v, %v; want YouTube's", pic, err)
	}
	if live, err := s.LivePictures(ctx, []uuid.UUID{first[0].Thumb}); err != nil || !live[first[0].Thumb] {
		t.Errorf("the still is not kept by the sweep: %v, %v", live, err)
	}
	if again := videos(); again[0].Thumb != first[0].Thumb {
		t.Error("matching again gave the same video a new still, to fetch again")
	}
}
