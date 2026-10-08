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
	SetEpisodeOrder(ctx context.Context, id uuid.UUID, order domain.EpisodeOrder) error
	Refresh(ctx context.Context, id uuid.UUID, mode domain.RefreshMode) error
	AnalyseTitle(ctx context.Context, id uuid.UUID) error
	Unmatch(ctx context.Context, id uuid.UUID) error
	SetTitleLocale(ctx context.Context, id uuid.UUID, loc domain.Locale) error
	SplitTitle(ctx context.Context, id uuid.UUID) error
	TitleFiles(ctx context.Context, id uuid.UUID) ([]store.LibraryFile, error)
	ForgetTitle(ctx context.Context, id uuid.UUID) error
	SetMarkers(ctx context.Context, version uuid.UUID, markers []domain.Marker, absent []domain.MarkerAbsent) error
	IdentifySubject(ctx context.Context, id uuid.UUID) (store.Subject, bool, error)
	ArtworkCandidates(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) ([]store.ArtworkCandidate, error)
	ChooseArtwork(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind, picture uuid.UUID) error
	ForgetArtworkChoice(ctx context.Context, id uuid.UUID, kind domain.ArtworkKind) error
}

type editJSON struct {
	Title         string         `json:"title,omitzero"`
	SortTitle     string         `json:"sort_title,omitzero"`
	OriginalTitle string         `json:"original_title,omitzero"`
	Overview      string         `json:"overview,omitzero"`
	Tagline       string         `json:"tagline,omitzero"`
	Certificate   string         `json:"certificate,omitzero"`
	ReleaseDate   string         `json:"release_date,omitzero"`
	Year          int            `json:"year,omitzero"`
	Genres        []string       `json:"genres,omitzero"`
	Studios       []string       `json:"studios,omitzero"`
	Locked        []domain.Field `json:"locked,omitzero"`
}

// editTitle writes what an admin says of a title, field by field; what it locks no source changes.
func (a *API) editTitle(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req editJSON
	if !a.decode(w, r, &req) {
		return
	}
	m := domain.Metadata{
		Title: req.Title, SortTitle: req.SortTitle, OriginalTitle: req.OriginalTitle, Overview: req.Overview,
		Tagline: req.Tagline, Certificate: req.Certificate, Year: req.Year, Genres: req.Genres, Studios: req.Studios,
		Locked: req.Locked,
	}
	if req.ReleaseDate != "" {
		d, err := time.Parse(time.DateOnly, req.ReleaseDate)
		if err != nil {
			writeProblem(w, a.logger, codeInvalidBody, "release_date is a date, 2006-01-02")
			return
		}
		m.ReleaseDate = d
	}
	if a.answered(w, r, a.svc.Editing.EditMetadata(r.Context(), id, m)) {
		return
	}
	a.titleUpdated(r, id)
	w.WriteHeader(http.StatusNoContent)
}

// resetEdits gives the fields named, or every one, back to the sources.
func (a *API) resetEdits(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var fields []domain.Field
	for _, f := range r.URL.Query()["field"] {
		if !slices.Contains(domain.Fields(), domain.Field(f)) {
			writeProblem(w, a.logger, codeInvalidParameter, f+" is not a field")
			return
		}
		fields = append(fields, domain.Field(f))
	}
	if a.answered(w, r, a.svc.Editing.ResetEdits(r.Context(), id, fields)) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type candidateJSON struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitzero"`
	Year          int    `json:"year,omitzero"`
	Overview      string `json:"overview,omitzero"`
	Poster        string `json:"poster,omitzero"`
}

// candidates lists what a provider has by a name, the title's own unless another is asked for, for
// an admin choosing its match, as Plex's Fix Match and Jellyfin's Identify do.
func (a *API) candidates(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	q := r.URL.Query()
	p, found, err := a.svc.Providers.Get(r.Context(), domain.FieldSource(q.Get("provider")))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	searcher, searches := provider.As[provider.Searcher](p, domain.CapabilitySearch)
	if !found || !searches {
		writeProblem(w, a.logger, codeInvalidParameter, "provider is one that searches")
		return
	}
	sub, ok, err := a.svc.Editing.IdentifySubject(r.Context(), id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if !ok || !slices.Contains(p.Info().Kinds, sub.Kind) {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	title, year := sub.Title, sub.Year
	if s := q.Get("title"); s != "" {
		title, year = s, 0
	}
	if year, ok = a.queryNumber(w, r, "year", year, 0, math.MaxInt); !ok {
		return
	}
	offered, err := searcher.Candidates(r.Context(), sub.Locale.Or(a.svc.Identity.Locale()), sub.Kind, title, year)
	if a.answered(w, r, err) {
		return
	}
	out := make([]candidateJSON, len(offered))
	for i, c := range offered {
		out[i] = candidateJSON{ID: c.ID, Title: c.Title, OriginalTitle: c.OriginalTitle, Year: c.Year, Overview: c.Overview, Poster: c.Poster}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[candidateJSON]{Items: out})
}

type pinMatchJSON struct {
	Provider domain.Provider `json:"provider"`
	ID       string          `json:"id"`
}

// pinMatch fixes a film or show to the title a provider has by the id given, and matches it again.
func (a *API) pinMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req pinMatchJSON
	if !a.decode(w, r, &req) {
		return
	}
	known := req.Provider != ""
	if _, plugin := domain.FieldSource(req.Provider).Plugin(); plugin {
		var err error
		if _, known, err = a.svc.Providers.Get(r.Context(), domain.FieldSource(req.Provider)); err != nil {
			a.internal(w, r, err)
			return
		}
	}
	if !known || req.ID == "" {
		writeProblem(w, a.logger, codeInvalidBody, "provider is one a title carries ids of, and id is set")
		return
	}
	err := a.svc.Editing.PinMatch(r.Context(), id, req.Provider, req.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no film or show has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type setEpisodeOrderJSON struct {
	Order domain.EpisodeOrder `json:"order"`
}

// setEpisodeOrder says the order a show's episode files are numbered in, as Plex's and Jellyfin's
// per-show episode ordering does, and matches its episodes again in it.
func (a *API) setEpisodeOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req setEpisodeOrderJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Order == "" {
		writeProblem(w, a.logger, codeInvalidBody, "order is aired, dvd or absolute")
		return
	}
	err := a.svc.Editing.SetEpisodeOrder(r.Context(), id, req.Order)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no show has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type refreshJSON struct {
	Mode domain.RefreshMode `json:"mode"`
}

// deleteTitle deletes a title's files from the disk, then the title, as Plex's and Jellyfin's
// Delete do, where its library allows it. The files go first: should one not, the title stays, and
// those deleted before it are missing at the next scan, as a file deleted by hand would be.
func (a *API) deleteTitle(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	files, err := a.svc.Editing.TitleFiles(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no film, show, season, episode or extra has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	for i, f := range files {
		err := library.Remove(f.Root, f.Rel)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		writeProblem(w, a.logger, codeConflict, fmt.Sprintf("%d of its %d files were deleted before %s could not be: %v",
			i, len(files), f.Rel, errors.Unwrap(err)))
		return
	}
	if a.answered(w, r, a.svc.Editing.ForgetTitle(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// split splits a film's copies apart, each other than the one that plays first a film of its own.
func (a *API) split(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.svc.Editing.SplitTitle(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no film has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type titleLocaleJSON struct {
	// MetadataLanguage is the language a film's or show's metadata is asked in, an IETF tag such
	// as en-GB, and CertificationCountry the country whose certificates, an ISO 3166-1 alpha-2 code
	// such as GB; "" is its library's.
	MetadataLanguage     string `json:"metadata_language"`
	CertificationCountry string `json:"certification_country"`
}

// setTitleLocale gives a film or show a locale of its own over its library's, as Jellyfin's item
// settings do, and describes it again in it.
func (a *API) setTitleLocale(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req titleLocaleJSON
	if !a.decode(w, r, &req) {
		return
	}
	lang, refusal := metadataLanguage(req.MetadataLanguage)
	country, countryRefusal := certificationCountry(req.CertificationCountry)
	if refusal = cmp.Or(refusal, countryRefusal); refusal != "" {
		writeProblem(w, a.logger, codeInvalidBody, refusal)
		return
	}
	err := a.svc.Editing.SetTitleLocale(r.Context(), id, domain.Locale{Language: lang, Country: country})
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no film or show has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// unmatch takes a film or show off its providers and holds it so, until its match is fixed or it
// is refreshed: the match PUT sets, gone.
func (a *API) unmatch(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.svc.Editing.Unmatch(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no film or show has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// analyse asks for a title's files to be read again, as Plex's Analyze does, with what is made
// from them after: its keyframes, previews and markers.
func (a *API) analyse(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Editing.AnalyseTitle(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// refresh asks a title's providers about it again now, ahead of the schedule.
func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
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
	err := a.svc.Editing.Refresh(r.Context(), id, req.Mode)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, a.logger, codeNotFound, "no film or show, or season or episode of one, has that id")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) editRoutes() []route {
	return []route{
		{
			pattern: "PATCH /api/v1/admin/titles/{id}", access: admin,
			summary: "Edit a title's fields, locking those named against its sources",
			body:    editJSON{}, status: http.StatusNoContent, handle: a.editTitle,
		},
		{
			pattern: "DELETE /api/v1/admin/titles/{id}/edits", access: admin,
			summary: "Give a title's edited fields back to its sources",
			query:   []param{{"field", []domain.Field{}, "The fields to give back; every one where none are named."}},
			status:  http.StatusAccepted, handle: a.resetEdits,
		},
		{
			pattern: "GET /api/v1/admin/titles/{id}/candidates", access: admin,
			summary: "List what a provider has by a title's name, to match it to",
			query: []param{
				{"provider", domain.FieldSource(""), "A provider that searches."},
				{"title", "", "The name to search for, the title's own by default."},
				{"year", 0, "The year to search in, the title's own by default."},
			},
			status: http.StatusOK, reply: listJSON[candidateJSON]{}, handle: a.candidates,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/match", access: admin,
			summary: "Match a film or show to a provider's title by its id",
			body:    pinMatchJSON{}, status: http.StatusAccepted, handle: a.pinMatch,
		},
		{
			pattern: "DELETE /api/v1/admin/titles/{id}/match", access: admin,
			summary: "Take a film or show off its providers, and keep it so until its match is fixed or it is refreshed",
			status:  http.StatusNoContent, handle: a.unmatch,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/locale", access: admin,
			summary: "Give a film or show a metadata language and certification country of its own, and describe it again in them",
			body:    titleLocaleJSON{}, status: http.StatusAccepted, handle: a.setTitleLocale,
		},
		{
			pattern: "POST /api/v1/admin/titles/{id}/split", access: admin,
			summary: "Split a film's copies apart: each but the one that plays first becomes a film of its own, and stays so",
			status:  http.StatusNoContent, handle: a.split,
		},
		{
			pattern: "DELETE /api/v1/admin/titles/{id}", access: admin,
			summary: "Delete a title's files from the disk, then the title, where its library allows it",
			status:  http.StatusNoContent, handle: a.deleteTitle,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/episode-order", access: admin,
			summary: "Say the order a show's episode files are numbered in",
			body:    setEpisodeOrderJSON{}, status: http.StatusAccepted, handle: a.setEpisodeOrder,
		},
		{
			pattern: "POST /api/v1/admin/titles/{id}/refresh", access: admin,
			summary: "Ask a title's providers about it again now: a season or episode as its show",
			body:    refreshJSON{}, status: http.StatusAccepted, handle: a.refresh,
		},
		{
			pattern: "POST /api/v1/admin/titles/{id}/analysis", access: admin,
			summary: "Read a title's files again, a show's or season's episodes', and remake what is made from them",
			status:  http.StatusAccepted, handle: a.analyse,
		},
	}
}
