//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
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
		{Name: "Films", Kind: domain.LibraryMovies, Media: domain.MediaFolder, Root: "/srv/films", Sources: domain.DefaultSources(domain.LibraryMovies), RemoteExtras: []domain.ExtraKind{domain.ExtraBehindTheScenes, domain.ExtraFeaturette, domain.ExtraTrailer}, Monitor: domain.MonitorRealtime, RefreshDays: 30, Previews: domain.PreviewsAll, Markers: domain.MarkersAll, Keyframes: domain.KeyframesIndex, Themes: domain.ThemesLocal, Deletion: domain.DeletionOff, Locale: domain.Locale{Artwork: domain.ArtworkLocalized}, Titles: domain.TitlesLocalized, Collections: domain.CollectionsGrouped, SubtitleLanguages: []language.Tag{}, SubtitleMatch: domain.SubtitleMatchRelease},
		{Name: "Television", Kind: domain.LibraryShows, Media: domain.MediaFolder, Root: "/srv/tv", Sources: domain.DefaultSources(domain.LibraryShows), RemoteExtras: []domain.ExtraKind{domain.ExtraBehindTheScenes, domain.ExtraFeaturette, domain.ExtraTrailer}, Monitor: domain.MonitorRealtime, RefreshDays: 30, Previews: domain.PreviewsAll, Markers: domain.MarkersAll, Keyframes: domain.KeyframesIndex, Themes: domain.ThemesLocal, Deletion: domain.DeletionOff, Locale: domain.Locale{Artwork: domain.ArtworkLocalized}, Titles: domain.TitlesLocalized, Collections: domain.CollectionsGrouped, SubtitleLanguages: []language.Tag{}, SubtitleMatch: domain.SubtitleMatchRelease},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(domain.Library{}, "ID")); diff != "" {
		t.Errorf("libraries (-want +got):\n%s", diff)
	}
}

// A remote library holds a list's titles and plays another provider's streams. It has no folder,
// so none is watched or deleted from, nor read whole for previews and markers unless an admin asks.
func TestARemoteLibraryHasNoFolder(t *testing.T) {
	s := migrated(t)
	lib, err := s.AddRemoteLibrary(t.Context(), "Popular", domain.LibraryMovies, domain.PluginSource("aio"), "movie/top", domain.PluginSource("aio"))
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
	} {
		if err := s.SetLibrary(t.Context(), lib.ID, change); !errors.Is(err, ErrRemoteUntouched) {
			t.Errorf("set to be %s: %v, want ErrRemoteUntouched", name, err)
		}
	}
	if err := s.SetLibrary(t.Context(), lib.ID, LibraryChange{Previews: domain.PreviewsChapters}); err != nil {
		t.Errorf("asked for chapter previews: %v", err)
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
	if err := s.SetTitleLocale(ctx, film, domain.Locale{Language: "fr-FR"}); err != nil {
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
	if page, err := s.Title(ctx, uuid.UUID{}, film); err != nil || page.Locale.Language != "fr-FR" {
		t.Errorf("its page says %+v, %v; want its own language", page.Locale, err)
	}
	if err := s.SetTitleLocale(ctx, film, domain.Locale{}); err != nil {
		t.Fatal(err)
	}
	sub, _, err = s.IdentifySubject(ctx, film)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Locale.Language != "de-DE" {
		t.Errorf("given back, it asks in %+v, want its library's German", sub.Locale)
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
