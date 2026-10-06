//go:build integration

package store

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store/model"
	"github.com/olivertgwalton/photon-server/internal/store/query"
)

func TestBetterSourcesSurviveRescans(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	scanned := func(title string) model.Item {
		t.Helper()
		film := Film{Title: title, Year: 1982, Folder: "Thing", Copies: []Copy{{
			ContentKey: []byte("thing"),
			Parts:      []Part{{RelPath: "Thing/Thing.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}},
		}}}
		if _, err := s.SaveFolder(ctx, lib.ID, "Thing", []byte("v1"), []Film{film}, nil); err != nil {
			t.Fatal(err)
		}
		items, err := s.q.Item.WithContext(ctx).Find()
		if err != nil || len(items) != 1 {
			t.Fatalf("items = %v, %v; want one", items, err)
		}
		return *items[0]
	}
	apply := func(id model.UUID, source domain.FieldSource, m domain.Metadata) {
		t.Helper()
		if err := s.q.Transaction(func(tx *query.Query) error {
			return applyMetadata(ctx, tx, id, source, m)
		}); err != nil {
			t.Fatal(err)
		}
	}

	item := scanned("the thing")
	apply(item.ID, domain.SourceTMDB, domain.Metadata{Title: "The Thing", Overview: "Antarctica.", Genres: []string{"Horror"}})
	apply(item.ID, domain.SourceUser, domain.Metadata{Overview: "Kurt Russell in the snow."})
	apply(item.ID, domain.SourceTMDB, domain.Metadata{Overview: "Antarctica, again."})

	item = scanned("the thing")
	if item.Title != "The Thing" || item.SortTitle != "thing" {
		t.Errorf("after a rescan, title = %q (sort %q), want TMDB's", item.Title, item.SortTitle)
	}
	if item.Overview == nil || *item.Overview != "Kurt Russell in the snow." {
		t.Errorf("overview = %v, want the user's", item.Overview)
	}
	if len(item.Genres) != 1 || item.Genres[0] != "Horror" {
		t.Errorf("genres = %v, want TMDB's", item.Genres)
	}

	// The file's title still finds the film, though it is no longer the film's title.
	if again := scanned("the thing"); again.ID != item.ID {
		t.Errorf("rescan made a new item")
	}
}

func TestNFOSaysMoreThanFileNamesButNotOverTypedIDs(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "alien", Folder: "Alien", IDs: map[domain.Provider]string{domain.ProviderTMDB: "348"},
		NFO: &domain.Metadata{Title: "Alien", Year: 1979, IDs: map[domain.Provider]string{
			domain.ProviderTMDB: "999", domain.ProviderIMDb: "tt0078748",
		}},
		Copies: []Copy{{ContentKey: []byte("alien"), Parts: []Part{{
			RelPath: "Alien/Alien.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Alien", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Alien" || item.Year == nil || *item.Year != 1979 {
		t.Errorf("title, year = %q, %v; want the NFO's", item.Title, item.Year)
	}
	ids, err := s.q.ExternalID.WithContext(ctx).Find()
	if err != nil {
		t.Fatal(err)
	}
	got := map[domain.Provider]string{}
	for _, id := range ids {
		got[id.Provider] = id.Value
	}
	if got[domain.ProviderTMDB] != "348" || got[domain.ProviderIMDb] != "tt0078748" {
		t.Errorf("ids = %v; want the folder's TMDB id and the NFO's IMDb id", got)
	}
}

func TestALockedFieldIsLeftForTheReader(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "heat", Folder: "Heat",
		NFO: &domain.Metadata{Title: "Heat", Locked: []domain.Field{domain.FieldOverview}},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
			RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.UUID(item.ID)
	if err := s.SaveIdentity(ctx, id, domain.SourceTMDB, domain.Metadata{Overview: "A crime saga.", Tagline: "A Los Angeles crime saga."}, nil); err != nil {
		t.Fatal(err)
	}
	item, _ = s.q.Item.WithContext(ctx).Take()
	if item.Overview != nil || item.Tagline == nil {
		t.Errorf("after a match, overview = %v and tagline = %v; want the locked overview empty and the tagline TMDB's", item.Overview, item.Tagline)
	}
	if err := s.q.Transaction(func(tx *query.Query) error {
		return applyMetadata(ctx, tx, item.ID, domain.SourceUser, domain.Metadata{Overview: "Pacino and De Niro."})
	}); err != nil {
		t.Fatal(err)
	}
	item, _ = s.q.Item.WithContext(ctx).Take()
	if item.Overview == nil || *item.Overview != "Pacino and De Niro." {
		t.Errorf("a reader's overview on a locked field = %v, want it written", item.Overview)
	}
}

func TestALibraryChoosesItsSourcesAndTheirOrder(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{
		Title: "heat", Folder: "Heat", NFO: &domain.Metadata{Title: "Heat (NFO)"},
		Copies: []Copy{{ContentKey: []byte("heat"), Parts: []Part{{
			RelPath: "Heat/Heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{},
		}}}},
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Heat", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	title := func() string {
		t.Helper()
		it, err := s.q.Item.WithContext(ctx).Where(s.q.Item.ID.Eq(item.ID)).Take()
		if err != nil {
			t.Fatal(err)
		}
		return it.Title
	}
	match := domain.Metadata{Title: "Heat (TMDB)"}

	if err := s.SaveIdentity(ctx, uuid.UUID(item.ID), domain.SourceTMDB, match, nil); err != nil {
		t.Fatal(err)
	}
	if got := title(); got != "Heat (NFO)" {
		t.Errorf("by default, title = %q, want the NFO's over TMDB's", got)
	}

	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceTMDB, domain.SourceNFO)}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.q.Folder.WithContext(ctx).Count(); n != 0 {
		t.Errorf("%d folders still fingerprinted after the sources changed, want none", n)
	}
	if n, _ := s.q.Job.WithContext(ctx).Where(s.q.Job.Kind.Eq(string(domain.JobIdentify))).Count(); n != 1 {
		t.Errorf("%d identify jobs after the sources changed, want the film's", n)
	}
	if err := s.SaveIdentity(ctx, uuid.UUID(item.ID), domain.SourceTMDB, match, nil); err != nil {
		t.Fatal(err)
	}
	if got := title(); got != "Heat (TMDB)" {
		t.Errorf("trusting TMDB first, title = %q, want TMDB's", got)
	}

	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: metadataFrom(domain.LibraryMovies, domain.SourceNFO)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIdentity(ctx, uuid.UUID(item.ID), domain.SourceTMDB, domain.Metadata{Overview: "From TMDB."}, nil); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.q.Item.WithContext(ctx).Where(s.q.Item.ID.Eq(item.ID)).Take(); it.Overview != nil {
		t.Errorf("a library that takes no TMDB took its overview %q", *it.Overview)
	}
	libs, err := s.Libraries(ctx)
	if err != nil || len(libs) != 1 || !libs[0].Takes(domain.SourceNFO) || libs[0].Takes(domain.SourceTMDB) {
		t.Errorf("libraries = %+v, %v; want Films taking only NFOs", libs, err)
	}
}

func TestEachKindOfItemTakesItsOwnSources(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	on := func(src domain.FieldSource) domain.RankedSource {
		return domain.RankedSource{Source: src, Enabled: true}
	}
	off := func(src domain.FieldSource) domain.RankedSource { return domain.RankedSource{Source: src} }
	// TMDB describes the show and TheTVDB its episodes, as Jellyfin's per-type downloaders can; the
	// show's pictures are TheTVDB's alone, with TMDB kept in its place but unticked.
	show := domain.KindSources{
		Kind:     domain.ItemShow,
		Metadata: []domain.RankedSource{on(domain.SourceTMDB), on(domain.SourceTVDB)},
		Images:   []domain.RankedSource{on(domain.SourceTVDB), off(domain.SourceTMDB)},
	}
	episode := domain.KindSources{
		Kind:     domain.ItemEpisode,
		Metadata: []domain.RankedSource{on(domain.SourceTVDB), on(domain.SourceTMDB)},
		Images:   []domain.RankedSource{on(domain.SourceTMDB)},
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: []domain.KindSources{show, episode}}); err != nil {
		t.Fatal(err)
	}
	ep := Episode{
		Season: 1, Episodes: []int{1}, Title: "Firefly", Folder: "Firefly/Season 1", ByNumber: true,
		Copies: []Copy{{ContentKey: []byte("f1"), Parts: []Part{{RelPath: "Firefly/Season 1/S01E01.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{}}}}},
	}
	if _, err := s.SaveShowFolder(ctx, lib.ID, "Firefly/Season 1", []byte("v1"), Show{Title: "Firefly", Folder: "Firefly"}, []Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	i := s.q.Item
	row, _ := i.WithContext(ctx).Where(i.Kind.Eq(string(domain.ItemShow))).Take()
	for _, src := range []domain.FieldSource{domain.SourceTMDB, domain.SourceTVDB} {
		said := func(what string) string { return what + " (" + string(src) + ")" }
		picture := func(kind domain.ArtworkKind) []domain.Artwork {
			return []domain.Artwork{{Kind: kind, URL: "https://images.test/" + string(src) + ".jpg"}}
		}
		if err := s.SaveIdentity(ctx, uuid.UUID(row.ID), src, domain.Metadata{Title: said("Firefly"), Artwork: picture(domain.ArtworkPoster)},
			map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{1: {Title: said("The Train Job"), Artwork: picture(domain.ArtworkThumb)}}}}); err != nil {
			t.Fatal(err)
		}
	}
	title := func(kind domain.ItemKind) (string, []domain.FieldSource) {
		t.Helper()
		it, err := i.WithContext(ctx).Where(i.Kind.Eq(string(kind))).Take()
		if err != nil {
			t.Fatal(err)
		}
		var pictures []domain.FieldSource
		a := s.q.Artwork
		if err := a.WithContext(ctx).Where(a.ItemID.Eq(it.ID)).Order(a.Source).Pluck(a.Source, &pictures); err != nil {
			t.Fatal(err)
		}
		return it.Title, pictures
	}
	if got, pictures := title(domain.ItemShow); got != "Firefly (tmdb)" || !slices.Equal(pictures, []domain.FieldSource{domain.SourceTVDB}) {
		t.Errorf("show = %q with pictures from %v; want TMDB's title and TheTVDB's pictures alone", got, pictures)
	}
	if got, pictures := title(domain.ItemEpisode); got != "The Train Job (tvdb)" || !slices.Equal(pictures, []domain.FieldSource{domain.SourceTMDB}) {
		t.Errorf("episode = %q with pictures from %v; want TheTVDB's title and TMDB's pictures alone", got, pictures)
	}
	libs, err := s.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := libs[0].Sources; len(got) != 3 || !slices.Equal(got[0].Images, show.Images) || !slices.Equal(got[1].Metadata, domain.DefaultSources(domain.LibraryShows)[1].Metadata) {
		t.Errorf("sources = %+v; want the show's unticked TMDB kept in its place and the seasons' left as they were", got)
	}
	// The episodes' own sources changing has the show's next match ask about them again.
	episode.Images = []domain.RankedSource{on(domain.SourceTVDB)}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{Sources: []domain.KindSources{episode}}); err != nil {
		t.Fatal(err)
	}
	if sub, _, err := s.IdentifySubject(ctx, uuid.UUID(row.ID)); err != nil || !slices.Equal(sub.Seasons, []int{1}) {
		t.Errorf("seasons asked about = %v, %v; want the first again", sub.Seasons, err)
	}
}
