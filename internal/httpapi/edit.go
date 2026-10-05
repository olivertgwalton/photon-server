package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type editing interface {
	EditMetadata(ctx context.Context, id uuid.UUID, m domain.Metadata) error
	ResetEdits(ctx context.Context, id uuid.UUID, fields []domain.Field) error
	PinMatch(ctx context.Context, id uuid.UUID, p domain.Provider, value string) error
	SetEpisodeOrder(ctx context.Context, id uuid.UUID, order domain.EpisodeOrder) error
	Refresh(ctx context.Context, id uuid.UUID, mode domain.RefreshMode) error
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
	id, ok := a.pathID(w, r)
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
	for _, f := range req.Locked {
		if !slices.Contains(domain.Fields(), f) {
			writeProblem(w, a.logger, codeInvalidBody, string(f)+" is not a field")
			return
		}
	}
	if a.answered(w, r, a.svc.Editing.EditMetadata(r.Context(), id, m)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetEdits gives the fields named, or every one, back to the sources.
func (a *API) resetEdits(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
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
	Poster        string `json:"poster,omitzero"`
}

// candidates lists what a provider has by a name, the title's own unless another is asked for, for
// an admin choosing its match, as Plex's Fix Match and Jellyfin's Identify do.
func (a *API) candidates(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
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
	if s := q.Get("year"); s != "" {
		if year, err = strconv.Atoi(s); err != nil {
			writeProblem(w, a.logger, codeInvalidParameter, "year is a year")
			return
		}
	}
	offered, err := searcher.Candidates(r.Context(), sub.Kind, title, year)
	if errors.Is(err, provider.ErrUnavailable) {
		writeProblem(w, a.logger, codeProviderUnavailable, err.Error())
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]candidateJSON, len(offered))
	for i, c := range offered {
		out[i] = candidateJSON{ID: c.ID, Title: c.Title, OriginalTitle: c.OriginalTitle, Year: c.Year, Poster: c.Poster}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[candidateJSON]{Items: out})
}

type pinMatchJSON struct {
	Provider domain.Provider `json:"provider"`
	ID       string          `json:"id"`
}

// pinMatch fixes a film or show to the title a provider has by the id given, and matches it again.
func (a *API) pinMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req pinMatchJSON
	if !a.decode(w, r, &req) {
		return
	}
	known := slices.Contains(domain.Providers(), req.Provider)
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
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req setEpisodeOrderJSON
	if !a.decode(w, r, &req) {
		return
	}
	if !slices.Contains(domain.EpisodeOrders(), req.Order) {
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

// refresh asks a title's providers about it again now, ahead of the schedule.
func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req refreshJSON
	if !a.decode(w, r, &req) {
		return
	}
	if !slices.Contains(domain.RefreshModes(), req.Mode) {
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

var artworkKindParam = param{"kind", domain.ArtworkKind(""), "The kind of picture."}

// artworkCandidateJSON is a picture a provider has, served at /api/v1/artwork/{id} like a title's
// own, so a browser shows it sized and from this server.
type artworkCandidateJSON struct {
	ID       uuid.UUID          `json:"id"`
	Source   domain.FieldSource `json:"source"`
	Language string             `json:"language,omitzero"`
	Width    int                `json:"width,omitzero"`
	Height   int                `json:"height,omitzero"`
	Chosen   bool               `json:"chosen"`
}

// artworkCandidates lists the pictures of a kind each provider has for a title, as Jellyfin's Edit
// Images and Plex's poster chooser do.
func (a *API) artworkCandidates(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	kind := domain.ArtworkKind(r.URL.Query().Get("kind"))
	if !slices.Contains(domain.ArtworkKinds(), kind) {
		writeProblem(w, a.logger, codeInvalidParameter, "kind is a kind of picture")
		return
	}
	offered, err := a.svc.Editing.ArtworkCandidates(r.Context(), id, kind)
	if a.answered(w, r, err) {
		return
	}
	out := make([]artworkCandidateJSON, len(offered))
	for i, c := range offered {
		out[i] = artworkCandidateJSON{ID: c.ID, Source: c.Source, Language: c.Language, Width: c.Width, Height: c.Height, Chosen: c.Chosen}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[artworkCandidateJSON]{Items: out})
}

// pathArtworkKind answers the kind of picture a path names, or writes why not.
func (a *API) pathArtworkKind(w http.ResponseWriter, r *http.Request) (domain.ArtworkKind, bool) {
	kind := domain.ArtworkKind(r.PathValue("kind"))
	if !slices.Contains(domain.ArtworkKinds(), kind) {
		writeProblem(w, a.logger, codeNotFound, "")
		return "", false
	}
	return kind, true
}

type chooseArtworkJSON struct {
	ID uuid.UUID `json:"id"`
}

// chooseArtwork makes a provider's picture a title's own of its kind, over every source and
// through every refresh.
func (a *API) chooseArtwork(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	kind, ok := a.pathArtworkKind(w, r)
	if !ok {
		return
	}
	var req chooseArtworkJSON
	if !a.decode(w, r, &req) {
		return
	}
	err := a.svc.Editing.ChooseArtwork(r.Context(), id, kind, req.ID)
	if errors.Is(err, store.ErrNotACandidate) {
		writeProblem(w, a.logger, codeInvalidBody, "id is one of the title's candidates of that kind")
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// forgetArtwork gives a title's picture of a kind back to its sources.
func (a *API) forgetArtwork(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	kind, ok := a.pathArtworkKind(w, r)
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Editing.ForgetArtworkChoice(r.Context(), id, kind)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// markerJSON is a stretch of a copy, on its whole timeline.
type markerJSON struct {
	Kind    domain.MarkerKind `json:"kind"`
	StartMS int64             `json:"start_ms"`
	EndMS   int64             `json:"end_ms"`
}

// markerAbsentJSON says a part of a copy, counted from 0, has no stretch of a kind.
type markerAbsentJSON struct {
	Kind domain.MarkerKind `json:"kind"`
	Part int               `json:"part"`
}

type markersJSON struct {
	Markers []markerJSON       `json:"markers"`
	Absent  []markerAbsentJSON `json:"absent,omitzero"`
}

// setMarkers says where a copy's intro, credits, recap and preview are, on its whole timeline as
// its chapters are, and which of its parts have none of a kind, over whatever its chapters or
// fingerprints say; saying nothing clears what was said.
func (a *API) setMarkers(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req markersJSON
	if !a.decode(w, r, &req) {
		return
	}
	markers := make([]domain.Marker, len(req.Markers))
	for i, m := range req.Markers {
		if !slices.Contains(domain.MarkerKinds(), m.Kind) {
			writeProblem(w, a.logger, codeInvalidBody, "a marker's kind is intro, credits, recap or preview")
			return
		}
		if m.StartMS < 0 || m.EndMS <= m.StartMS {
			writeProblem(w, a.logger, codeInvalidBody, "a marker ends after it starts, at 0 or later")
			return
		}
		markers[i] = domain.Marker{Kind: m.Kind, StartMS: m.StartMS, EndMS: m.EndMS}
	}
	absent := make([]domain.MarkerAbsent, len(req.Absent))
	for i, m := range req.Absent {
		if !slices.Contains(domain.MarkerKinds(), m.Kind) {
			writeProblem(w, a.logger, codeInvalidBody, "a marker's kind is intro, credits, recap or preview")
			return
		}
		if m.Part < 0 {
			writeProblem(w, a.logger, codeInvalidBody, "a part is counted from 0")
			return
		}
		absent[i] = domain.MarkerAbsent{Kind: m.Kind, Part: m.Part}
	}
	err := a.svc.Editing.SetMarkers(r.Context(), id, markers, absent)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "no copy has that id")
		return
	case errors.Is(err, store.ErrMarkerOutsidePart), errors.Is(err, store.ErrMarkerRepeated), errors.Is(err, store.ErrMarkerNoPart):
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	if a.answered(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
