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
	IdentifySubject(ctx context.Context, id uuid.UUID) (store.Subject, bool, error)
}

// editTitle writes what an admin says of a title, field by field; what it locks no source changes.
func (a *API) editTitle(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Title         string         `json:"title"`
		SortTitle     string         `json:"sort_title"`
		OriginalTitle string         `json:"original_title"`
		Overview      string         `json:"overview"`
		Tagline       string         `json:"tagline"`
		Certificate   string         `json:"certificate"`
		ReleaseDate   string         `json:"release_date"`
		Year          int            `json:"year"`
		Genres        []string       `json:"genres"`
		Studios       []string       `json:"studios"`
		Locked        []domain.Field `json:"locked"`
	}
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

// candidates lists what a provider has by a name, the title's own unless another is asked for, for
// an admin choosing its match, as Plex's Fix Match and Jellyfin's Identify do.
func (a *API) candidates(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	p, found := a.svc.Providers.Get(domain.FieldSource(q.Get("provider")))
	searcher, searches := p.(provider.Searcher)
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
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type candidateJSON struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		OriginalTitle string `json:"original_title,omitzero"`
		Year          int    `json:"year,omitzero"`
		Poster        string `json:"poster,omitzero"`
	}
	out := make([]candidateJSON, len(offered))
	for i, c := range offered {
		out[i] = candidateJSON{ID: strconv.Itoa(c.ID), Title: c.Title, OriginalTitle: c.OriginalTitle, Year: c.Year, Poster: c.Poster}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": out})
}

// pinMatch fixes a film or show to the title a provider has by the id given, and matches it again.
func (a *API) pinMatch(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Provider domain.Provider `json:"provider"`
		ID       string          `json:"id"`
	}
	if !a.decode(w, r, &req) {
		return
	}
	if !slices.Contains(domain.Providers(), req.Provider) || req.ID == "" {
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

// setEpisodeOrder says the order a show's episode files are numbered in, as Plex's and Jellyfin's
// per-show episode ordering does, and matches its episodes again in it.
func (a *API) setEpisodeOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Order domain.EpisodeOrder `json:"order"`
	}
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
