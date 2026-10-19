//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestLibraries(t *testing.T) {
	s := migrated(t)
	films, err := s.AddLibrary(t.Context(), "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if films.ID == (uuid.UUID{}) {
		t.Error("the new library has no id")
	}
	if _, err := s.AddLibrary(t.Context(), "Television", domain.LibraryShows, "/srv/tv"); err != nil {
		t.Fatal(err)
	}
	for _, dup := range [][2]string{{"Films", "/srv/other"}, {"Other", "/srv/films"}} {
		if _, err := s.AddLibrary(t.Context(), dup[0], domain.LibraryMovies, dup[1]); !errors.Is(err, ErrLibraryExists) {
			t.Errorf("adding %q at %q: err = %v, want %v", dup[0], dup[1], err, ErrLibraryExists)
		}
	}

	got, err := s.Libraries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Library{
		{Name: "Films", Kind: domain.LibraryMovies, Media: domain.MediaFolder, Root: "/srv/films", Sources: domain.DefaultSources(domain.LibraryMovies), RemoteExtras: []domain.ExtraKind{domain.ExtraBehindTheScenes, domain.ExtraFeaturette, domain.ExtraTrailer}, Monitor: domain.MonitorRealtime, RefreshDays: 30, Previews: domain.PreviewsAll, Markers: domain.MarkersAll, Keyframes: domain.KeyframesIndex, Themes: domain.ThemesLocal, Deletion: domain.DeletionOff, MissingCopies: domain.MissingKept, Locale: domain.Locale{Artwork: domain.ArtworkLocalized}, Titles: domain.TitlesLocalized, BoxSets: domain.BoxSetsExpanded, SubtitleLanguages: []language.Tag{}, SubtitleMatch: domain.SubtitleMatchRelease},
		{Name: "Television", Kind: domain.LibraryShows, Media: domain.MediaFolder, Root: "/srv/tv", Sources: domain.DefaultSources(domain.LibraryShows), RemoteExtras: []domain.ExtraKind{domain.ExtraBehindTheScenes, domain.ExtraFeaturette, domain.ExtraTrailer}, Monitor: domain.MonitorRealtime, RefreshDays: 30, Previews: domain.PreviewsAll, Markers: domain.MarkersAll, Keyframes: domain.KeyframesIndex, Themes: domain.ThemesLocal, Deletion: domain.DeletionOff, MissingCopies: domain.MissingKept, Locale: domain.Locale{Artwork: domain.ArtworkLocalized}, Titles: domain.TitlesLocalized, BoxSets: domain.BoxSetsExpanded, SubtitleLanguages: []language.Tag{}, SubtitleMatch: domain.SubtitleMatchRelease},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(domain.Library{}, "ID")); diff != "" {
		t.Errorf("libraries (-want +got):\n%s", diff)
	}
}

// A remote library holds a list's titles and plays another provider's streams. It has no folder,
// so none is watched or deleted from, nor read whole for previews and markers unless an admin asks.
func TestARemoteLibraryHasNoFolder(t *testing.T) {
	s := migrated(t)
	lib, err := s.AddRemoteLibrary(t.Context(), "Popular", domain.LibraryMovies, Remote{ListSource: domain.PluginSource("aio"), ListID: "movie/top", StreamSource: domain.PluginSource("aio")})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Library(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Media != domain.MediaRemote || got.Root != "" || got.ListSource != domain.PluginSource("aio") || got.ListID != "movie/top" ||
		got.StreamSource != domain.PluginSource("aio") || got.Monitor != domain.MonitorOff || got.Deletion != domain.DeletionOff ||
		got.Previews != domain.PreviewsOff || got.Markers != domain.MarkersOff {
		t.Errorf("the remote library is %+v", got)
	}
	for name, change := range map[string]LibraryChange{
		"watched":      {Monitor: domain.MonitorRealtime},
		"deleted from": {Deletion: domain.DeletionFiles},
		"made to forget the copies it is no longer offered": {MissingCopies: domain.MissingForgotten},
	} {
		if err := s.SetLibrary(t.Context(), lib.ID, change); !errors.Is(err, ErrRemoteUntouched) {
			t.Errorf("set to be %s: %v, want ErrRemoteUntouched", name, err)
		}
	}
	if err := s.SetLibrary(t.Context(), lib.ID, LibraryChange{Previews: domain.PreviewsChapters}); err != nil {
		t.Errorf("asked for chapter previews: %v", err)
	}
	if _, _, err := s.ForgetMissingCopies(t.Context(), lib.ID); !errors.Is(err, ErrRemoteUntouched) {
		t.Errorf("forgetting its copies on no disk: %v, want ErrRemoteUntouched", err)
	}
}

// An admin's ask empties what a library kept: every copy whose files have gone leaves it, the
// titles left with nothing go with them, and both are told as a scan's changes are.
func TestForgettingALibrarysCopiesOnNoDisk(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	film := Film{Title: "Heat", Folder: "H", Copies: []Copy{{
		ContentKey: []byte("c"), Parts: []Part{{RelPath: "H/heat.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}
	if _, err := s.SaveFolder(ctx, lib.ID, "H", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	var item uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT id FROM items`).Scan(&item); err != nil {
		t.Fatal(err)
	}
	// The file has gone, and the library keeps the copy, marked missing.
	if _, err := s.FinishScan(ctx, lib.ID, []string{"."}, []string{"H"}, nil); err != nil {
		t.Fatal(err)
	}
	copies, changed, err := s.ForgetMissingCopies(ctx, lib.ID)
	if err != nil || copies != 1 {
		t.Fatalf("ForgetMissingCopies = %d, %v; want the one copy on no disk", copies, err)
	}
	if !slices.Equal(changed[domain.TitleRemoved], []uuid.UUID{item}) {
		t.Errorf("told %v changed, want %v removed", changed, item)
	}
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM items`).Scan(&left); err != nil || left != 0 {
		t.Errorf("%d titles left, %v; want the film gone with its only copy", left, err)
	}
	if copies, _, err := s.ForgetMissingCopies(ctx, lib.ID); err != nil || copies != 0 {
		t.Errorf("asking again = %d, %v; want nothing left to forget", copies, err)
	}
}

func TestALibraryIsRenamedAndRemoved(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddLibrary(ctx, "Television", domain.LibraryShows, "/srv/tv"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, films.ID, LibraryChange{Name: "Television"}); !errors.Is(err, ErrLibraryExists) {
		t.Errorf("renaming onto another's name: %v, want %v", err, ErrLibraryExists)
	}
	if err := s.SetLibrary(ctx, films.ID, LibraryChange{Name: "Movies", Monitor: domain.MonitorOff, Deletion: domain.DeletionFiles}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Library(ctx, films.ID); err != nil || got.Name != "Movies" || got.Monitor != domain.MonitorOff || got.Deletion != domain.DeletionFiles {
		t.Errorf("after renaming: %+v, %v", got, err)
	}
	if err := s.RemoveLibrary(ctx, films.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Library(ctx, films.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removing: %v, want %v", err, ErrNotFound)
	}
	if err := s.RemoveLibrary(ctx, films.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing it again: %v, want %v", err, ErrNotFound)
	}
}

// metadataFrom asks sources, most trusted first, for the metadata of every kind a library of lib
// holds.
func metadataFrom(lib domain.LibraryKind, sources ...domain.FieldSource) []domain.KindSources {
	var out []domain.KindSources
	for _, kind := range lib.ItemKinds() {
		k := domain.KindSources{Kind: kind, Metadata: []domain.RankedSource{}}
		for _, src := range sources {
			k.Metadata = append(k.Metadata, domain.RankedSource{Source: src, Enabled: true})
		}
		out = append(out, k)
	}
	return out
}

func TestALibrarysLocaleDescribesItsTitlesAgain(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Jaws", []byte("v1"), []Film{{Title: "Jaws", Folder: "Jaws"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	queued := func() int {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'identify'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	french, gb := "fr-FR", "GB"
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{MetadataLanguage: &french, CertificationCountry: &gb}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Library(ctx, lib.ID); err != nil || got.Locale != (domain.Locale{Language: "fr-FR", Country: "GB", Artwork: domain.ArtworkLocalized}) {
		t.Errorf("the library: %+v, %v; want it asking in French for Britain's certificates", got.Locale, err)
	}
	if n := queued(); n != 1 {
		t.Errorf("%d titles queued to be described, want the film described again in French", n)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	// The same again is no change, and asks nothing.
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{MetadataLanguage: &french}); err != nil || queued() != 0 {
		t.Errorf("setting it as it was: %v, %d queued; want nothing asked again", err, queued())
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{ArtworkLanguage: domain.ArtworkAny}); err != nil || queued() != 1 {
		t.Errorf("taking the most liked pictures: %v, %d queued; want the film described again", err, queued())
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{TitleLanguage: domain.TitlesOriginal}); err != nil || queued() != 1 {
		t.Errorf("giving original titles: %v, %d queued; want the film described again", err, queued())
	}
	none := ""
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{MetadataLanguage: &none, CertificationCountry: &none, ArtworkLanguage: domain.ArtworkLocalized}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Library(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Locale != (domain.Locale{Artwork: domain.ArtworkLocalized}) {
		t.Errorf("given back: %+v, want the server's own", got.Locale)
	}
}

func TestAFilmAsksInALocaleOfItsOwnOverItsLibrarys(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Filme", domain.LibraryMovies, "/srv/filme")
	if err != nil {
		t.Fatal(err)
	}
	german := "de-DE"
	if err := s.SetLibrary(ctx, lib.ID, LibraryChange{MetadataLanguage: &german}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFolder(ctx, lib.ID, "Amelie", []byte("v1"), []Film{{Title: "Amelie", Folder: "Amelie"}}, nil); err != nil {
		t.Fatal(err)
	}
	film := oneItem(t, s, "kind = 'movie'").ID
	if _, err := s.pool.Exec(ctx, `DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitleLocale(ctx, film, domain.Locale{Language: "fr-FR"}, domain.TitlesOriginal); err != nil {
		t.Fatal(err)
	}
	sub, _, err := s.IdentifySubject(ctx, film)
	if err != nil || sub.Locale.Language != "fr-FR" || sub.Locale.Country != "FR" {
		t.Errorf("the film asks in %+v, %v; want its own French, and France's certificates by it", sub.Locale, err)
	}
	var queued int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = 'identify' AND subject = $1`, film).Scan(&queued); err != nil || queued != 1 {
		t.Errorf("%d identify jobs, %v; want the film described again", queued, err)
	}
	if sub.Titles != domain.TitlesOriginal {
		t.Errorf("the film gives its %s title, want its own original over its library's localized", sub.Titles)
	}
	if page, err := s.Title(ctx, uuid.UUID{}, film); err != nil || page.Locale.Language != "fr-FR" || page.Titles != domain.TitlesOriginal {
		t.Errorf("its page says %+v, %s, %v; want its own language and title", page.Locale, page.Titles, err)
	}
	if err := s.SetTitleLocale(ctx, film, domain.Locale{}, ""); err != nil {
		t.Fatal(err)
	}
	sub, _, err = s.IdentifySubject(ctx, film)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Locale.Language != "de-DE" || sub.Titles != domain.TitlesLocalized {
		t.Errorf("given back, it asks in %+v for its %s title, want its library's German and localized", sub.Locale, sub.Titles)
	}
}

func TestTheCountriesCertificatesAreReadForAreCountries(t *testing.T) {
	s := migrated(t)
	countries, err := s.CertificateCountries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(countries, "GB") || !slices.Contains(countries, "IN") {
		t.Errorf("countries %v, want Britain and India among them", countries)
	}
	for _, c := range countries {
		if len(c) != 2 {
			t.Errorf("%q is listed, which is no country's ISO 3166-1 code", c)
		}
	}
}

func TestAProfileSeesItsLibrariesInItsOwnOrder(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	var libs [3]domain.Library
	for i, name := range []string{"Films", "Television", "Archive"} {
		lib, err := s.AddLibrary(ctx, name, domain.LibraryMovies, "/srv/"+name)
		if err != nil {
			t.Fatal(err)
		}
		libs[i] = lib
	}
	films, tv := libs[0], libs[1]
	ada, err := s.AddProfile(ctx, "Ada", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kids, err := s.AddProfile(ctx, "Kids", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccess(ctx, kids.ID, ProfileAccess{Libraries: []uuid.UUID{films.ID, tv.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	seen := func(profile uuid.UUID) []string {
		t.Helper()
		libs, err := s.LibrariesSeen(ctx, profile)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, l := range libs {
			names = append(names, l.Name)
		}
		return names
	}
	if got := seen(kids.ID); !slices.Equal(got, []string{"Films", "Television"}) {
		t.Errorf("Kids sees %v, want Films then Television by name, and not the Archive it may not open", got)
	}
	if err := s.SetLibraryOrder(ctx, ada.ID, []uuid.UUID{tv.ID, films.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibraryOrder(ctx, kids.ID, []uuid.UUID{tv.ID}); err != nil {
		t.Fatal(err)
	}
	if got := seen(ada.ID); !slices.Equal(got, []string{"Television", "Films", "Archive"}) {
		t.Errorf("Ada sees %v, want Television, Films, then the Archive she has not placed", got)
	}
	if got := seen(kids.ID); !slices.Equal(got, []string{"Television", "Films"}) {
		t.Errorf("Kids sees %v, want Television then Films, in its own order", got)
	}
	// An order naming a library twice or none is refused, and the last one kept.
	for _, bad := range [][]uuid.UUID{{films.ID, films.ID}, {uuid.NewV7()}} {
		if err := s.SetLibraryOrder(ctx, ada.ID, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("ordering %v: %v, want ErrNotFound", bad, err)
		}
	}
	if err := s.RemoveLibrary(ctx, tv.ID); err != nil {
		t.Fatal(err)
	}
	if got := seen(ada.ID); !slices.Equal(got, []string{"Films", "Archive"}) {
		t.Errorf("after removing Television, Ada sees %v, want Films then the Archive", got)
	}
}
