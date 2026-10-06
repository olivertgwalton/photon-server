//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnEditStandsUntilItIsReset(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "heat", Folder: "Heat", Copies: []Copy{{ContentKey: []byte("h"), Parts: []Part{{RelPath: "Heat/heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	id := oneItem(t, s, "true").ID
	matched := domain.Metadata{Title: "Heat", Overview: "A heist.", Tagline: "A Los Angeles crime saga", IDs: map[domain.Provider]string{domain.ProviderTMDB: "999", domain.ProviderIMDb: "tt0000001"}}
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, matched, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.EditMetadata(ctx, id, domain.Metadata{Title: "Heat (Director's Cut)", Locked: []domain.Field{domain.FieldTagline}}); err != nil {
		t.Fatal(err)
	}
	// Matched again: the edit and the lock stand, the rest is the provider's.
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, matched, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.Title(ctx, uuid.UUID{}, id)
	if err != nil || page.Title != "Heat (Director's Cut)" || page.Overview != "A heist." {
		t.Fatalf("after matching again: %q %q, %v", page.Title, page.Overview, err)
	}
	if err := s.ResetEdits(ctx, id, []domain.Field{domain.FieldTitle}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, matched, nil); err != nil {
		t.Fatal(err)
	}
	locked := matched
	locked.Tagline = "Another tagline"
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, locked, nil); err != nil {
		t.Fatal(err)
	}
	if page, _ = s.Title(ctx, uuid.UUID{}, id); page.Title != "Heat" || page.Tagline != "A Los Angeles crime saga" {
		t.Errorf("after resetting the title alone: %q, tagline %q; want TMDB's title and the tagline kept as it was locked", page.Title, page.Tagline)
	}
	if n := countRows(t, s, `SELECT count(*) FROM jobs WHERE kind = 'scan_library'`); n != 1 {
		t.Errorf("%d library scans queued after a reset, want 1", n)
	}
	if n := countRows(t, s, `SELECT count(*) FROM folders`); n != 0 {
		t.Errorf("%d folders remembered after a reset, want its folder to be read again", n)
	}

	// The wrong film was matched: the admin pins the right one.
	if err := s.PinMatch(ctx, id, domain.ProviderTMDB, "949"); err != nil {
		t.Fatal(err)
	}
	sub, _, err := s.IdentifySubject(ctx, id)
	if err != nil || sub.IDs[domain.ProviderTMDB] != "949" || sub.IDs[domain.ProviderIMDb] != "" {
		t.Errorf("after pinning: ids %v, %v; want TMDB's pinned and the IMDb id the wrong match gave gone", sub.IDs, err)
	}
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "999"}}, nil); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ = s.IdentifySubject(ctx, id); sub.IDs[domain.ProviderTMDB] != "949" {
		t.Errorf("a match gave %q, want the pinned id to stand", sub.IDs[domain.ProviderTMDB])
	}
	if err := s.EditMetadata(ctx, uuid.NewV7(), domain.Metadata{Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("editing no title: %v, want ErrNotFound", err)
	}
}

func TestAShowRenumberedIsMatchedAgainWhole(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryShows, domain.SourceNFO, domain.SourceTVDB, domain.SourceTMDB)}); err != nil {
		t.Fatal(err)
	}
	ep := Episode{
		Season: 1, Episodes: []int{1}, Title: "Firefly", Folder: "Firefly/Season 1", ByNumber: true,
		Copies: []Copy{{ContentKey: []byte("f1"), Parts: []Part{{RelPath: "Firefly/Season 1/S01E01.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Firefly/Season 1", []byte("v1"), Show{Title: "Firefly", Folder: "Firefly"}, []Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	id := oneItem(t, s, "kind = 'show'").ID
	// Matched as aired: the first file is titled as broadcast's first.
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Firefly"},
		map[int]domain.SeasonMetadata{1: {Metadata: domain.Metadata{Title: "Season 1"}, Episodes: map[int]domain.Metadata{1: {Title: "The Train Job", Artwork: []domain.Artwork{{Kind: domain.ArtworkThumb, URL: "https://image.tmdb.org/t/p/original/train.jpg"}}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Identified(ctx, id); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ := s.IdentifySubject(ctx, id); len(sub.Seasons) != 0 || sub.Order != domain.OrderAired {
		t.Fatalf("matched: %+v; want nothing left to ask, in aired order", sub)
	}
	if err := s.SetEpisodeOrder(ctx, id, domain.OrderDVD); err != nil {
		t.Fatal(err)
	}
	sub, _, err := s.IdentifySubject(ctx, id)
	if err != nil || sub.Order != domain.OrderDVD || len(sub.Seasons) != 1 {
		t.Fatalf("renumbered: %+v, %v; want season 1 asked again, on DVD", sub, err)
	}
	if err := s.SaveIdentity(ctx, id, domain.SourceTVDB, domain.Metadata{Title: "Firefly"},
		map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: "Serenity"}}}}); err != nil {
		t.Fatal(err)
	}
	e := oneItem(t, s, "kind = 'episode'")
	page, err := s.Title(ctx, uuid.UUID{}, e.ID)
	if err != nil || page.Title != "Serenity" || len(page.Artwork[domain.ArtworkThumb]) != 0 {
		t.Errorf("the first file on DVD: %q, stills %v, %v; want Serenity and the aired still gone", page.Title, page.Artwork, err)
	}
	if err := s.SetEpisodeOrder(ctx, e.ID, domain.OrderDVD); !errors.Is(err, ErrNotFound) {
		t.Errorf("renumbering an episode: %v, want ErrNotFound", err)
	}
}

func TestARefreshIsAskedAheadOfTheQueue(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	ep := func(n int) Episode {
		return Episode{
			Season: 1, Episodes: []int{n}, Title: "Firefly", Folder: "Firefly/Season 1", ByNumber: true,
			Copies: []Copy{{ContentKey: []byte{byte(n)}, Parts: []Part{{RelPath: "Firefly/Season 1/" + string(rune('0'+n)) + ".mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
		}
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Firefly/Season 1", []byte("v1"), Show{Title: "Firefly", Folder: "Firefly"}, []Episode{ep(1), ep(2)}, nil); err != nil {
		t.Fatal(err)
	}
	id := oneItem(t, s, "kind = 'show'").ID
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Firefly"},
		map[int]domain.SeasonMetadata{1: {Metadata: domain.Metadata{Title: "Season 1"}, Episodes: map[int]domain.Metadata{
			1: {Title: "Serenity"}, 2: {Title: "The Train Job"},
		}}}); err != nil {
		t.Fatal(err)
	}
	second := oneItem(t, s, "kind = 'episode' AND episode_number = 2")
	if err := s.EditMetadata(ctx, second.ID, domain.Metadata{Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	// The match is done; a scan of the library is waiting.
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE kind = 'identify'`); err != nil {
		t.Fatal(err)
	}
	if err := s.ScanLibrary(ctx, lib.ID, 0); err != nil {
		t.Fatal(err)
	}

	season := oneItem(t, s, "kind = 'season'")
	if err := s.Refresh(ctx, season.ID, domain.RefreshMissing); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimJobs(ctx, []domain.JobKind{domain.JobScanLibrary, domain.JobIdentify}, nil, uuid.NewV7(), time.Minute, 1)
	if err != nil || len(claimed) != 1 || claimed[0].Kind != domain.JobIdentify || claimed[0].Subject != id {
		t.Fatalf("claimed %+v, %v; want the show's match, asked after the scan was queued", claimed, err)
	}
	if sub, _, _ := s.IdentifySubject(ctx, id); len(sub.Seasons) != 0 {
		t.Errorf("refreshing what is missing asks about seasons %v; want none, all are described", sub.Seasons)
	}

	if err := s.Refresh(ctx, id, domain.RefreshAll); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ := s.IdentifySubject(ctx, id); len(sub.Seasons) != 1 {
		t.Errorf("refreshing all asks about seasons %v; want season 1 again", sub.Seasons)
	}
	for n, want := range map[int]string{1: "Serenity", 2: "Mine"} {
		page, err := s.Title(ctx, uuid.UUID{}, oneItem(t, s, "kind = 'episode' AND episode_number = $1", n).ID)
		if err != nil || page.Title != want {
			t.Errorf("episode %d before the match: %q, %v; want %q to stand", n, page.Title, err, want)
		}
	}
	if err := s.Refresh(ctx, uuid.NewV7(), domain.RefreshAll); !errors.Is(err, ErrNotFound) {
		t.Errorf("refreshing nothing: %v, want ErrNotFound", err)
	}
}

func TestALibraryRefreshTakesWhatIsMissingAfterNewTitles(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := func(title string) uuid.UUID {
		saved, err := s.SaveFolder(ctx, lib.ID, title, []byte("v1"), []Film{{Title: title, Folder: title}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return saved.Titles[domain.TitleAdded][0]
	}
	poster := []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://image.example/heat.jpg"}}
	described := map[string]domain.Metadata{
		"Heat":  {Title: "Heat", Overview: "A thief and a detective.", Artwork: poster},
		"Ronin": {Title: "Ronin", Overview: "Mercenaries and a case."},
		"Alien": {Title: "Alien"},
	}
	ids := map[uuid.UUID]string{}
	for title, m := range described {
		id := film(title)
		ids[id] = title
		if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, m, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Identified(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	queued := func() []string {
		rows, err := s.pool.Query(ctx, `SELECT subject FROM jobs WHERE kind = 'identify'`)
		if err != nil {
			t.Fatal(err)
		}
		subjects, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, id := range subjects {
			titles = append(titles, ids[id])
		}
		slices.Sort(titles)
		return titles
	}
	// Each was matched when the scan found it.
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE kind = 'identify'`); err != nil {
		t.Fatal(err)
	}

	if err := s.RefreshLibrary(ctx, lib.ID, domain.RefreshMissing); err != nil {
		t.Fatal(err)
	}
	if got := queued(); !slices.Equal(got, []string{"Alien", "Ronin"}) {
		t.Errorf("refreshing what is missing queued %v; want Alien, with no overview, and Ronin, with no poster", got)
	}
	ids[film("Thief")] = "Thief"
	claimed, err := s.ClaimJobs(ctx, []domain.JobKind{domain.JobIdentify}, nil, uuid.NewV7(), time.Minute, 1)
	if err != nil || len(claimed) != 1 || ids[claimed[0].Subject] != "Thief" {
		t.Errorf("claimed %+v, %v; want Thief, just found, ahead of the refresh", claimed, err)
	}

	if err := s.RefreshLibrary(ctx, lib.ID, domain.RefreshAll); err != nil {
		t.Fatal(err)
	}
	if got := queued(); !slices.Equal(got, []string{"Alien", "Heat", "Ronin", "Thief"}) {
		t.Errorf("refreshing all queued %v; want every film", got)
	}
	if err := s.RefreshLibrary(ctx, uuid.NewV7(), domain.RefreshAll); !errors.Is(err, ErrNotFound) {
		t.Errorf("refreshing no library: %v, want ErrNotFound", err)
	}
}

func TestALibraryRefreshOfAllAsksAboutEverySeason(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	ep := Episode{
		Season: 1, Episodes: []int{1}, Title: "Firefly", Folder: "Firefly/Season 1", ByNumber: true,
		Copies: []Copy{{ContentKey: []byte{1}, Parts: []Part{{RelPath: "Firefly/Season 1/1.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Firefly/Season 1", []byte("v1"), Show{Title: "Firefly", Folder: "Firefly"}, []Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	id := oneItem(t, s, "kind = 'show'").ID
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Title: "Firefly"},
		map[int]domain.SeasonMetadata{1: {Metadata: domain.Metadata{Title: "Season 1"}, Episodes: map[int]domain.Metadata{1: {Title: "Serenity"}}}}); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ := s.IdentifySubject(ctx, id); len(sub.Seasons) != 0 {
		t.Fatalf("seasons %v asked about after the match; want none", sub.Seasons)
	}
	if err := s.RefreshLibrary(ctx, lib.ID, domain.RefreshAll); err != nil {
		t.Fatal(err)
	}
	if sub, _, _ := s.IdentifySubject(ctx, id); !slices.Equal(sub.Seasons, []int{1}) {
		t.Errorf("refreshing the library asks about seasons %v; want season 1 again", sub.Seasons)
	}
}
