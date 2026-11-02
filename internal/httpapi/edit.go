package httpapi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type editing interface {
	EditMetadata(ctx context.Context, id uuid.UUID, m domain.Metadata) error
	ResetEdits(ctx context.Context, id uuid.UUID, fields []domain.Field) error
	PinMatch(ctx context.Context, id uuid.UUID, p domain.Provider, value string) error
	SetEpisodeOrder(ctx context.Context, id uuid.UUID, order domain.EpisodeOrder, group string) error
	Refresh(ctx context.Context, id uuid.UUID, mode domain.RefreshMode) error
	AnalyseTitle(ctx context.Context, id uuid.UUID) error
	Unmatch(ctx context.Context, id uuid.UUID) error
	SetTitleLocale(ctx context.Context, id uuid.UUID, loc domain.Locale, titles domain.TitleLanguage) error
	SplitTitle(ctx context.Context, id uuid.UUID) error
	MergeTitle(ctx context.Context, id, other uuid.UUID) error
	TitleFiles(ctx context.Context, id uuid.UUID) ([]store.LibraryFile, error)
	ForgetTitle(ctx context.Context, id uuid.UUID) error
	SetMarkers(ctx context.Context, version uuid.UUID, markers []domain.Marker, absent []domain.MarkerAbsent) error
	IdentifySubject(ctx context.Context, id uuid.UUID) (store.Subject, bool, error)
	ArtworkCandidates(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) ([]store.ArtworkCandidate, error)
	ChooseArtwork(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind, picture uuid.UUID) error
	UploadArtwork(ctx context.Context, id, picture uuid.UUID, a domain.Artwork) error
	ForgetArtworkChoice(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) error
}

type editJSON struct {
	Title         string      `json:"title,omitzero"`
	SortTitle     string      `json:"sort_title,omitzero"`
	OriginalTitle string      `json:"original_title,omitzero"`
	Overview      string      `json:"overview,omitzero"`
	Tagline       string      `json:"tagline,omitzero"`
	Certificate   string      `json:"certificate,omitzero"`
	ReleaseDate   domain.Date `json:"release_date,omitzero"`
	Year          int         `json:"year,omitzero"`
	Genres        []string    `json:"genres,omitzero"`
	Studios       []string    `json:"studios,omitzero"`
	// AirsBeforeSeason and AirsBeforeEpisode, or AirsAfterSeason, place a special in the season it
	// aired in.
	AirsBeforeSeason  int            `json:"airs_before_season,omitzero"`
	AirsBeforeEpisode int            `json:"airs_before_episode,omitzero"`
	AirsAfterSeason   int            `json:"airs_after_season,omitzero"`
	Locked            []domain.Field `json:"locked,omitzero"`
}

// editTitle writes what an admin says of a title, field by field; what it locks no source changes.
func (a *API) editTitle(r *http.Request, req editJSON) (none, error) {
	id := pathUUID(r, "id")
	if req.AirsBeforeSeason < 0 || req.AirsBeforeEpisode < 0 || req.AirsAfterSeason < 0 {
		return none{}, refuse(codeInvalidBody, "a season or episode a special aired by is a number from 1")
	}
	m := domain.Metadata{
		Title: req.Title, SortTitle: req.SortTitle, OriginalTitle: req.OriginalTitle, Overview: req.Overview,
		Tagline: req.Tagline, Certificate: req.Certificate, Year: req.Year, Genres: req.Genres, Studios: req.Studios,
		ReleaseDate: time.Time(req.ReleaseDate), AirsBeforeSeason: req.AirsBeforeSeason, AirsBeforeEpisode: req.AirsBeforeEpisode,
		AirsAfterSeason: req.AirsAfterSeason, Locked: req.Locked,
	}
	if err := a.svc.Editing.EditMetadata(r.Context(), id, m); err != nil {
		return none{}, err
	}
	a.titleUpdated(r, id)
	return none{}, nil
}

// resetEdits gives the fields named, or every one, back to the sources.
func (a *API) resetEdits(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	var fields []domain.Field
	for _, f := range r.URL.Query()["field"] {
		if !slices.Contains(domain.Fields(), domain.Field(f)) {
			return none{}, refuse(codeInvalidParameter, f+" is not a field")
		}
		fields = append(fields, domain.Field(f))
	}
	return none{}, a.svc.Editing.ResetEdits(r.Context(), id, fields)
}

type candidateJSON struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitzero"`
	Year          int    `json:"year,omitzero"`
	Overview      string `json:"overview,omitzero"`
	Poster        uri    `json:"poster,omitzero"`
}

// candidates lists what a provider has by a name, the title's own unless another is asked for, for
// an admin choosing its match, as Plex's Fix Match and Jellyfin's Identify do.
func (a *API) candidates(r *http.Request, _ none) (listJSON[candidateJSON], error) {
	id := pathUUID(r, "id")
	q := r.URL.Query()
	p, found, err := a.svc.Providers.Get(r.Context(), domain.FieldSource(q.Get("provider")))
	if err != nil {
		return listJSON[candidateJSON]{}, err
	}
	searcher, searches := provider.As[provider.Searcher](p, domain.CapabilitySearch)
	if !found || !searches {
		return listJSON[candidateJSON]{}, refuse(codeInvalidParameter, "provider is one that searches")
	}
	sub, ok, err := a.svc.Editing.IdentifySubject(r.Context(), id)
	if err != nil {
		return listJSON[candidateJSON]{}, err
	}
	if !ok || !slices.Contains(p.Info().Kinds, sub.Kind) {
		return listJSON[candidateJSON]{}, refuse(codeNotFound, "")
	}
	title, year := sub.Title, sub.Year
	if s := q.Get("title"); s != "" {
		title, year = s, 0
	}
	if year, err = queryNumber(r, "year", year, 0, math.MaxInt); err != nil {
		return listJSON[candidateJSON]{}, err
	}
	offered, err := searcher.Candidates(r.Context(), sub.Locale.Or(a.svc.Identity.Locale()), sub.Kind, title, year)
	if err != nil {
		return listJSON[candidateJSON]{}, err
	}
	out := make([]candidateJSON, len(offered))
	for i, c := range offered {
		out[i] = candidateJSON{ID: c.ID, Title: c.Title, OriginalTitle: c.OriginalTitle, Year: c.Year, Overview: c.Overview, Poster: uri(c.Poster)}
	}
	return listJSON[candidateJSON]{Items: out}, nil
}

type pinMatchJSON struct {
	Provider domain.Provider `json:"provider"`
	ID       string          `json:"id"`
}

// pinMatch fixes a film or show to the title a provider has by the id given, and matches it again.
func (a *API) pinMatch(r *http.Request, req pinMatchJSON) (none, error) {
	id := pathUUID(r, "id")
	known := req.Provider != ""
	if _, plugin := domain.FieldSource(req.Provider).Plugin(); plugin {
		var err error
		if _, known, err = a.svc.Providers.Get(r.Context(), domain.FieldSource(req.Provider)); err != nil {
			return none{}, err
		}
	}
	if !known || req.ID == "" {
		return none{}, refuse(codeInvalidBody, "provider is one a title carries ids of, and id is set")
	}
	err := a.svc.Editing.PinMatch(r.Context(), id, req.Provider, req.ID)
	return none{}, notFoundAs(err, "no film or show has that id")
}

type setEpisodeOrderJSON struct {
	Order domain.EpisodeOrder `json:"order"`
	// Group is the TMDB episode group a show in the group order is numbered by, and only then.
	Group string `json:"group,omitzero"`
}

// setEpisodeOrder says the order a show's episode files are numbered in, as Plex's and Jellyfin's
// per-show episode ordering does, and matches its episodes again in it.
func (a *API) setEpisodeOrder(r *http.Request, req setEpisodeOrderJSON) (none, error) {
	id := pathUUID(r, "id")
	if req.Order == "" || (req.Order == domain.OrderGroup) != (req.Group != "") {
		return none{}, refuse(codeInvalidBody, "order is aired, dvd, absolute or group, and group names a TMDB episode group with group alone")
	}
	err := a.svc.Editing.SetEpisodeOrder(r.Context(), id, req.Order, req.Group)
	return none{}, notFoundAs(err, "no show has that id")
}

type episodeGroupJSON struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Kind     domain.GroupKind `json:"kind"`
	Seasons  int              `json:"seasons"`
	Episodes int              `json:"episodes"`
}

// episodeGrouper is TMDB, which has other orders of a show's episodes, its episode groups.
type episodeGrouper interface {
	EpisodeGroups(ctx context.Context, loc domain.Locale, show int) ([]domain.EpisodeGroup, error)
}

// episodeGroups lists the episode groups TMDB has a show in, for an admin choosing the one its
// files are numbered by.
func (a *API) episodeGroups(r *http.Request, _ none) (listJSON[episodeGroupJSON], error) {
	id := pathUUID(r, "id")
	sub, ok, err := a.svc.Editing.IdentifySubject(r.Context(), id)
	if err != nil {
		return listJSON[episodeGroupJSON]{}, err
	}
	if !ok || sub.Kind != domain.ItemShow {
		return listJSON[episodeGroupJSON]{}, refuse(codeNotFound, "no show has that id")
	}
	show, err := strconv.Atoi(sub.IDs[domain.ProviderTMDB])
	if err != nil {
		return listJSON[episodeGroupJSON]{}, refuse(codeConflict, "the show has no TMDB id to find its episode groups by")
	}
	p, _, err := a.svc.Providers.Get(r.Context(), domain.SourceTMDB)
	grouper, ok := p.(episodeGrouper)
	if err != nil || !ok {
		return listJSON[episodeGroupJSON]{}, cmp.Or(err, errors.New("tmdb has no episode groups"))
	}
	groups, err := grouper.EpisodeGroups(r.Context(), sub.Locale.Or(a.svc.Identity.Locale()), show)
	if err != nil {
		return listJSON[episodeGroupJSON]{}, err
	}
	out := make([]episodeGroupJSON, len(groups))
	for i, g := range groups {
		out[i] = episodeGroupJSON(g)
	}
	return listJSON[episodeGroupJSON]{Items: out}, nil
}

type refreshJSON struct {
	Mode domain.RefreshMode `json:"mode"`
}

// deleteTitle deletes a title's files from the disk, then the title, as Plex's and Jellyfin's
// Delete do, where its library allows it. The files go first: should one not, the title stays, and
// those deleted before it are missing at the next scan, as a file deleted by hand would be.
func (a *API) deleteTitle(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	files, err := a.svc.Editing.TitleFiles(r.Context(), id)
	if err != nil {
		return none{}, notFoundAs(err, "no film, show, season, episode or extra has that id")
	}
	for i, f := range files {
		err := library.Remove(f.Root, f.Rel)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return none{}, refuse(codeConflict, fmt.Sprintf("%d of its %d files were deleted before %s could not be: %v",
			i, len(files), f.Rel, errors.Unwrap(err)))
	}
	return none{}, a.svc.Editing.ForgetTitle(r.Context(), id)
}

// split splits a film's versions apart, each other than the one that plays first a film of its own.
func (a *API) split(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	err := a.svc.Editing.SplitTitle(r.Context(), id)
	return none{}, notFoundAs(err, "no film has that id")
}

type mergeJSON struct {
	// TitleID is the film merged in, which goes.
	TitleID uuid.UUID `json:"title_id"`
}

// merge merges another film into a film, as Plex's Merge does: its versions become the film's.
func (a *API) merge(r *http.Request, req mergeJSON) (none, error) {
	id := pathUUID(r, "id")
	err := a.svc.Editing.MergeTitle(r.Context(), id, req.TitleID)
	return none{}, notFoundAs(err, "no film has that id")
}

type titleLocaleJSON struct {
	// MetadataLanguage is the language a film's or show's metadata is asked in, an IETF tag such
	// as en-GB, and CertificationCountry the country whose certificates, an ISO 3166-1 alpha-2 code
	// such as GB; "" is its library's.
	MetadataLanguage     domain.LanguageTag `json:"metadata_language"`
	CertificationCountry domain.CountryCode `json:"certification_country"`
	// TitleLanguage is which title it gives, localized or original; absent is its library's.
	TitleLanguage domain.TitleLanguage `json:"title_language,omitzero"`
}

// setTitleLocale gives a film or show a locale of its own over its library's, as Jellyfin's item
// settings do, and describes it again in it.
func (a *API) setTitleLocale(r *http.Request, req titleLocaleJSON) (none, error) {
	id := pathUUID(r, "id")
	lang, refusal := metadataLanguage(string(req.MetadataLanguage))
	country, countryRefusal := certificationCountry(string(req.CertificationCountry))
	if refusal = cmp.Or(refusal, countryRefusal); refusal != "" {
		return none{}, refuse(codeInvalidBody, refusal)
	}
	err := a.svc.Editing.SetTitleLocale(r.Context(), id, domain.Locale{Language: lang, Country: country}, req.TitleLanguage)
	return none{}, notFoundAs(err, "no film or show has that id")
}

// unmatch takes a film or show off its providers and holds it so, until its match is fixed or it
// is refreshed: the match PUT sets, gone.
func (a *API) unmatch(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	err := a.svc.Editing.Unmatch(r.Context(), id)
	return none{}, notFoundAs(err, "no film or show has that id")
}

// analyse asks for a title's files to be read again, as Plex's Analyze does, with what is made
// from them after: its keyframes, previews and markers.
func (a *API) analyse(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	return none{}, a.svc.Editing.AnalyseTitle(r.Context(), id)
}

// refresh asks a title's providers about it again now, ahead of the schedule.
func (a *API) refresh(r *http.Request, req refreshJSON) (none, error) {
	id := pathUUID(r, "id")
	if req.Mode == "" {
		return none{}, refuse(codeInvalidBody, "mode is missing or all")
	}
	err := a.svc.Editing.Refresh(r.Context(), id, req.Mode)
	return none{}, notFoundAs(err, "no film or show, or season or episode of one, has that id")
}

func (a *API) editRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "PATCH /api/v1/admin/titles/{id}", access: admin,
			summary: "Edit a title's fields, locking those named against its sources",
			status:  http.StatusNoContent,
		}, a.editTitle),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/titles/{id}/edits", access: admin,
			summary: "Give a title's edited fields back to its sources",
			query:   []param{{"field", []domain.Field{}, "The fields to give back; every one where none are named."}},
			status:  http.StatusAccepted,
		}, a.resetEdits),
		a.handles(route{
			pattern: "GET /api/v1/admin/titles/{id}/candidates", access: admin,
			summary: "List what a provider has by a title's name, to match it to",
			query: []param{
				{"provider", domain.FieldSource(""), "A provider that searches."},
				{"title", "", "The name to search for, the title's own by default."},
				{"year", 0, "The year to search in, the title's own by default."},
			},
			status: http.StatusOK,
		}, a.candidates),
		a.handles(route{
			pattern: "PUT /api/v1/admin/titles/{id}/match", access: admin,
			summary: "Match a film or show to a provider's title by its id",
			status:  http.StatusAccepted,
		}, a.pinMatch),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/titles/{id}/match", access: admin,
			summary: "Take a film or show off its providers, and keep it so until its match is fixed or it is refreshed",
			status:  http.StatusNoContent,
		}, a.unmatch),
		a.handles(route{
			pattern: "PUT /api/v1/admin/titles/{id}/locale", access: admin,
			summary: "Give a film or show a metadata language, certification country and title language of its own, and describe it again in them",
			status:  http.StatusAccepted,
		}, a.setTitleLocale),
		a.handles(route{
			pattern: "POST /api/v1/admin/titles/{id}/split", access: admin,
			summary: "Split a film's versions apart: each but the one that plays first becomes a film of its own, and stays so",
			status:  http.StatusNoContent,
		}, a.split),
		a.handles(route{
			pattern: "POST /api/v1/admin/titles/{id}/merge", access: admin,
			summary: "Merge another film into a film: its versions become the film's, and stay so, its watch state, favourites and lists move with them, and it goes",
			status:  http.StatusNoContent,
		}, a.merge),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/titles/{id}", access: admin,
			summary: "Delete a title's files from the disk, then the title, where its library allows it",
			status:  http.StatusNoContent,
		}, a.deleteTitle),
		a.handles(route{
			pattern: "PUT /api/v1/admin/titles/{id}/episode-order", access: admin,
			summary: "Say the order a show's episode files are numbered in",
			status:  http.StatusAccepted,
		}, a.setEpisodeOrder),
		a.handles(route{
			pattern: "GET /api/v1/admin/titles/{id}/episode-groups", access: admin,
			summary: "List the episode groups TMDB has a show in, to number its files by",
			status:  http.StatusOK,
		}, a.episodeGroups),
		a.handles(route{
			pattern: "POST /api/v1/admin/titles/{id}/refresh", access: admin,
			summary: "Ask a title's providers about it again now: a season or episode as its show",
			status:  http.StatusAccepted,
		}, a.refresh),
		a.handles(route{
			pattern: "POST /api/v1/admin/titles/{id}/analysis", access: admin,
			summary: "Read a title's files again, a show's or season's episodes', and remake what is made from them",
			status:  http.StatusAccepted,
		}, a.analyse),
	}
}
