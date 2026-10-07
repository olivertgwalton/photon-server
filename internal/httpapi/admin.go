package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// requireAdmin admits a signed-in admin.
func (a *API) requireAdmin(next http.Handler) http.Handler {
	return a.requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionOf(r).Profile.Role != domain.RoleAdmin {
			writeProblem(w, a.logger, codeForbidden, "only an admin may")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

type libraryAdmin interface {
	Libraries(ctx context.Context) ([]domain.Library, error)
	Library(ctx context.Context, id uuid.UUID) (domain.Library, error)
	AddLibrary(ctx context.Context, name string, kind domain.LibraryKind, root string) (domain.Library, error)
	SetLibrary(ctx context.Context, id uuid.UUID, change store.LibraryChange) error
	RemoveLibrary(ctx context.Context, id uuid.UUID) error
	ScanFolders(ctx context.Context, lib uuid.UUID, folders []string, delay time.Duration) error
	RefreshLibrary(ctx context.Context, lib uuid.UUID, mode domain.RefreshMode) error
	LibraryCounts(ctx context.Context, profile uuid.UUID) (map[uuid.UUID]domain.TitleCounts, error)
	CertificateCountries(ctx context.Context) ([]string, error)
}

// adminLibraryListingJSON is a library as an admin keeps it, with everything it holds.
type adminLibraryListingJSON struct {
	adminLibraryJSON
	Counts countsJSON `json:"counts"`
}

// countsJSON is how many of each kind of title a library holds.
type countsJSON struct {
	Movies   int `json:"movies"`
	Shows    int `json:"shows"`
	Seasons  int `json:"seasons"`
	Episodes int `json:"episodes"`
	// Collections are how many its collections listing holds, so a client knows whether to offer
	// one without asking it.
	Collections int `json:"collections"`
}

// adminLibraryJSON is a library as an admin sees it: where it is and how it is kept.
type adminLibraryJSON struct {
	ID           uuid.UUID              `json:"id"`
	Name         string                 `json:"name"`
	Kind         domain.LibraryKind     `json:"kind"`
	Root         string                 `json:"root"`
	Sources      []kindSourcesJSON      `json:"sources"`
	RemoteExtras []domain.ExtraKind     `json:"remote_extras"`
	Monitor      domain.Monitor         `json:"monitor"`
	RefreshDays  int                    `json:"refresh_days"`
	Previews     domain.PreviewLevel    `json:"previews"`
	Markers      domain.MarkerDetection `json:"markers"`
	Keyframes    domain.KeyframeMode    `json:"keyframes"`
	Themes       domain.ThemeLookup     `json:"themes"`
	Deletion     domain.MediaDeletion   `json:"deletion"`
	// MetadataLanguage and CertificationCountry are what its metadata is asked in, absent for the
	// server's own.
	MetadataLanguage     string `json:"metadata_language,omitzero"`
	CertificationCountry string `json:"certification_country,omitzero"`
	// ArtworkLanguage is which of its titles' pictures it takes first: localized, those in its
	// language, then English, then wordless; or any, the most liked.
	ArtworkLanguage domain.ArtworkLanguage `json:"artwork_language"`
	// TitleLanguage is which title it gives its films and shows: localized, in its language; or
	// original, in the title's own.
	TitleLanguage domain.TitleLanguage `json:"title_language"`
	// CollectionMode is how its wall shows its collections: grouped, in place of the titles they
	// hold; shown, beside them; or hidden.
	CollectionMode domain.CollectionMode `json:"collection_mode"`
	// SubtitleLanguages are the languages it fetches subtitles in for copies with none in them,
	// as BCP 47 tags; none is no fetching. SubtitleMatch is which it takes: release, one made for
	// the copy's very file; or any, the best found.
	SubtitleLanguages []string             `json:"subtitle_languages"`
	SubtitleMatch     domain.SubtitleMatch `json:"subtitle_match"`
}

// kindSourcesJSON ranks where a kind of item a library holds takes its metadata and its pictures
// from, most trusted first, as Jellyfin's metadata downloaders and image fetchers: a lower source
// only fills what those above it left empty. A source not enabled keeps its place but is not asked.
type kindSourcesJSON struct {
	Kind     domain.ItemKind    `json:"kind"`
	Metadata []rankedSourceJSON `json:"metadata"`
	Images   []rankedSourceJSON `json:"images"`
}

type rankedSourceJSON struct {
	Source  domain.FieldSource `json:"source"`
	Enabled bool               `json:"enabled"`
}

func adminLibrary(l domain.Library) adminLibraryJSON {
	j := adminLibraryJSON{
		ID: l.ID, Name: l.Name, Kind: l.Kind, Root: l.Root, Sources: []kindSourcesJSON{},
		RemoteExtras: nonNil(l.RemoteExtras), Monitor: l.Monitor, RefreshDays: l.RefreshDays,
		Previews: l.Previews, Markers: l.Markers, Keyframes: l.Keyframes, Themes: l.Themes, Deletion: l.Deletion,
		MetadataLanguage: l.Locale.Language, CertificationCountry: l.Locale.Country, ArtworkLanguage: l.Locale.Artwork,
		TitleLanguage: l.Titles, CollectionMode: l.Collections, SubtitleLanguages: make([]string, len(l.SubtitleLanguages)),
		SubtitleMatch: l.SubtitleMatch,
	}
	for i, t := range l.SubtitleLanguages {
		j.SubtitleLanguages[i] = t.String()
	}
	ranked := func(list []domain.RankedSource) []rankedSourceJSON {
		out := make([]rankedSourceJSON, len(list))
		for i, r := range list {
			out[i] = rankedSourceJSON(r)
		}
		return out
	}
	for _, k := range l.Sources {
		j.Sources = append(j.Sources, kindSourcesJSON{Kind: k.Kind, Metadata: ranked(k.Metadata), Images: ranked(k.Images)})
	}
	return j
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (a *API) adminLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := a.svc.Libraries.Libraries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// Counted as the server sees them, whatever the admin's own profile may.
	counts, err := a.svc.Libraries.LibraryCounts(r.Context(), uuid.UUID{})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]adminLibraryListingJSON, len(libs))
	for i, l := range libs {
		out[i] = adminLibraryListingJSON{adminLibrary(l), countsJSON(counts[l.ID])}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[adminLibraryListingJSON]{Items: out})
}

type addLibraryJSON struct {
	Name string             `json:"name"`
	Kind domain.LibraryKind `json:"kind"`
	// Root is an absolute path to a folder on the server.
	Root string `json:"root"`
}

// addLibrary adds a library of a folder on the server and scans it at once.
func (a *API) addLibrary(w http.ResponseWriter, r *http.Request) {
	var req addLibraryJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Kind == "" || req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is set and kind is movies or shows")
		return
	}
	if !filepath.IsAbs(req.Root) {
		writeProblem(w, a.logger, codeInvalidBody, "root is an absolute path")
		return
	}
	root := filepath.Clean(req.Root)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		writeProblem(w, a.logger, codeInvalidBody, "root is not a folder the server can read")
		return
	}
	lib, err := a.svc.Libraries.AddLibrary(r.Context(), req.Name, req.Kind, root)
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventLibraryAdded, Library: lib.ID, Details: map[string]any{"name": lib.Name}})
	if err := a.svc.Libraries.ScanFolders(r.Context(), lib.ID, []string{"."}, 0); err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, adminLibrary(lib))
}

// libraryChangeJSON changes what it sets and leaves the rest.
type libraryChangeJSON struct {
	Name string `json:"name,omitzero"`
	// Sources replace, for each kind given, the metadata and the images rankings given; one left
	// out is left as it is.
	Sources      []kindSourcesChangeJSON `json:"sources,omitzero"`
	RemoteExtras []domain.ExtraKind      `json:"remote_extras,omitzero"`
	Monitor      domain.Monitor          `json:"monitor,omitzero"`
	// RefreshDays is how often its metadata is refreshed, 0 for never.
	RefreshDays *int                   `json:"refresh_days,omitzero"`
	Previews    domain.PreviewLevel    `json:"previews,omitzero"`
	Markers     domain.MarkerDetection `json:"markers,omitzero"`
	// Keyframes is how its files' keyframes are found: index reads the container's own index,
	// full walks a file that has none in the maintenance window, off finds none.
	Keyframes domain.KeyframeMode `json:"keyframes,omitzero"`
	// Themes is where its titles' theme tunes are found: local is the files beside them; themerr
	// those and, for a film or show with none, the YouTube link ThemerrDB lists, downloaded with
	// yt-dlp, which the server must have; off none.
	Themes domain.ThemeLookup `json:"themes,omitzero"`
	// Deletion is whether an admin may delete its titles with their files: off, or files.
	Deletion domain.MediaDeletion `json:"deletion,omitzero"`
	// MetadataLanguage is the language its metadata is asked in, an IETF tag such as en-GB, and
	// CertificationCountry the country whose certificates, an ISO 3166-1 alpha-2 code such as GB;
	// "" is the server's own. Changing either describes its titles again.
	MetadataLanguage     *string `json:"metadata_language,omitzero"`
	CertificationCountry *string `json:"certification_country,omitzero"`
	// ArtworkLanguage is which of its titles' pictures it takes first; changing it describes them
	// again.
	ArtworkLanguage domain.ArtworkLanguage `json:"artwork_language,omitzero"`
	// TitleLanguage is which title it gives its films and shows; changing it describes them again.
	TitleLanguage domain.TitleLanguage `json:"title_language,omitzero"`
	// CollectionMode is how its wall shows its collections.
	CollectionMode domain.CollectionMode `json:"collection_mode,omitzero"`
	// SubtitleLanguages replace the languages it fetches subtitles in; [] is none.
	SubtitleLanguages []string             `json:"subtitle_languages,omitzero"`
	SubtitleMatch     domain.SubtitleMatch `json:"subtitle_match,omitzero"`
}

type kindSourcesChangeJSON struct {
	Kind     domain.ItemKind    `json:"kind"`
	Metadata []rankedSourceJSON `json:"metadata,omitzero"`
	Images   []rankedSourceJSON `json:"images,omitzero"`
}

// setLibrary changes what is sent of a library: its name, whether it is watched, where each kind
// of item's metadata and pictures come from, the kinds of video it keeps providers' links to, what previews it makes,
// how it finds markers and keyframes, and where it finds theme tunes.
func (a *API) setLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req libraryChangeJSON
	if !a.decode(w, r, &req) {
		return
	}
	lib, err := a.svc.Libraries.Library(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	change := store.LibraryChange{
		Name: req.Name, RemoteExtras: req.RemoteExtras, Monitor: req.Monitor, RefreshDays: req.RefreshDays,
		Previews: req.Previews, Markers: req.Markers, Keyframes: req.Keyframes, Themes: req.Themes,
		Deletion: req.Deletion, Collections: req.CollectionMode, SubtitleMatch: req.SubtitleMatch,
	}
	if req.SubtitleLanguages != nil {
		change.SubtitleLanguages = make([]language.Tag, 0, len(req.SubtitleLanguages))
		for _, s := range req.SubtitleLanguages {
			t, err := language.Parse(s)
			if err != nil || t == language.Und {
				writeProblem(w, a.logger, codeInvalidBody, "subtitle_languages are BCP 47 tags, such as fr or pt-BR")
				return
			}
			if !slices.Contains(change.SubtitleLanguages, t) {
				change.SubtitleLanguages = append(change.SubtitleLanguages, t)
			}
		}
	}
	if d := req.RefreshDays; d != nil && (*d < 0 || *d > 365) {
		writeProblem(w, a.logger, codeInvalidBody, "refresh_days is from 0, never, to 365")
		return
	}
	for _, field := range []struct {
		value *string
		check func(string) (string, string)
	}{{req.MetadataLanguage, metadataLanguage}, {req.CertificationCountry, certificationCountry}} {
		if field.value == nil {
			continue
		}
		canonical, refusal := field.check(*field.value)
		if refusal != "" {
			writeProblem(w, a.logger, codeInvalidBody, refusal)
			return
		}
		*field.value = canonical
	}
	change.MetadataLanguage, change.CertificationCountry = req.MetadataLanguage, req.CertificationCountry
	change.ArtworkLanguage, change.TitleLanguage = req.ArtworkLanguage, req.TitleLanguage
	if req.Themes == domain.ThemesThemerr && a.svc.Setup.Tools.YTDLP.Path == "" {
		writeProblem(w, a.logger, codeConflict, "themerr needs yt-dlp, which this server does not have")
		return
	}
	ranked := func(list []rankedSourceJSON) []domain.RankedSource {
		if list == nil {
			return nil
		}
		out := make([]domain.RankedSource, len(list))
		for i, r := range list {
			out[i] = domain.RankedSource(r)
		}
		return out
	}
	for _, k := range req.Sources {
		change.Sources = append(change.Sources, domain.KindSources{Kind: k.Kind, Metadata: ranked(k.Metadata), Images: ranked(k.Images)})
	}
	if err := domain.CheckSources(lib.Kind, change.Sources); err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	if a.answered(w, r, a.svc.Libraries.SetLibrary(r.Context(), id, change)) {
		return
	}
	lib, err = a.svc.Libraries.Library(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, adminLibrary(lib))
}

func (a *API) removeLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	lib, err := a.svc.Libraries.Library(r.Context(), id)
	if a.answered(w, r, err) || a.answered(w, r, a.svc.Libraries.RemoveLibrary(r.Context(), id)) {
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventLibraryRemoved, Details: map[string]any{"name": lib.Name}})
	w.WriteHeader(http.StatusNoContent)
}

// scanLibrary queues a scan of a library now, or with path, of the folder of it a path is in, as
// Plex's refresh?path= scans one after a download lands.
func (a *API) scanLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	lib, err := a.svc.Libraries.Library(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	folder := "."
	if p := r.URL.Query().Get("path"); p != "" {
		if !filepath.IsAbs(p) {
			writeProblem(w, a.logger, codeInvalidParameter, "path is an absolute path")
			return
		}
		if folder, ok = library.Changed(lib.Root, filepath.Clean(p)); !ok {
			writeProblem(w, a.logger, codeInvalidParameter, "path is not inside the library")
			return
		}
	}
	if err := a.svc.Libraries.ScanFolders(r.Context(), id, []string{folder}, 0); err != nil {
		a.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// refreshLibrary asks a library's providers about its films and shows again, behind what a scan
// has just found.
func (a *API) refreshLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req refreshJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Mode == "" {
		writeProblem(w, a.logger, codeInvalidBody, "mode is missing or all")
		return
	}
	if a.answered(w, r, a.svc.Libraries.RefreshLibrary(r.Context(), id, req.Mode)) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type localesJSON struct {
	// Languages a library may ask its metadata in, IETF tags: those TMDB has translations in.
	Languages []string `json:"languages"`
	// Countries a library may take its certificates from, ISO 3166-1 alpha-2 codes: those whose
	// certificates the server can read for parental controls.
	Countries []string `json:"countries"`
}

// locales lists what a library may ask its metadata in, as Jellyfin's Localization cultures and
// countries do.
func (a *API) locales(w http.ResponseWriter, r *http.Request) {
	countries, err := a.svc.Libraries.CertificateCountries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, localesJSON{Languages: domain.MetadataLanguages(), Countries: countries})
}

// metadataLanguage is a metadata language as sent, written as its standard writes it, or why it is
// refused; "" stays "", the one above's.
func metadataLanguage(s string) (canonical, refusal string) {
	if s == "" {
		return "", ""
	}
	tag, err := language.Parse(s)
	if err != nil {
		return "", "metadata_language is an IETF language tag, such as en-GB"
	}
	return tag.String(), ""
}

// certificationCountry is a certification country as sent, written as its standard writes it, or
// why it is refused; "" stays "", the one above's.
func certificationCountry(s string) (canonical, refusal string) {
	if s == "" {
		return "", ""
	}
	region, err := language.ParseRegion(s)
	if err != nil || !region.IsCountry() {
		return "", "certification_country is an ISO 3166-1 alpha-2 country, such as GB"
	}
	return region.String(), ""
}
