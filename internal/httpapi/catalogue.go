package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	defaultWallLimit = 50
	maxWallLimit     = 200
)

type catalogue interface {
	Libraries(ctx context.Context) ([]domain.Library, error)
	Wall(ctx context.Context, lib uuid.UUID, p store.WallPage) ([]store.Card, string, error)
	Title(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
	Search(ctx context.Context, q store.SearchQuery) ([]store.Card, error)
}

type libraryJSON struct {
	ID   uuid.UUID          `json:"id"`
	Name string             `json:"name"`
	Kind domain.LibraryKind `json:"kind"`
}

type cardJSON struct {
	ID          uuid.UUID        `json:"id"`
	Kind        domain.ItemKind  `json:"kind"`
	Title       string           `json:"title"`
	Year        int              `json:"year,omitzero"`
	ReleaseDate domain.Date      `json:"release_date,omitzero"`
	AddedAt     time.Time        `json:"added_at"`
	Poster      uuid.UUID        `json:"poster,omitzero"`
	Backdrop    uuid.UUID        `json:"backdrop,omitzero"`
	State       store.TitleState `json:"state,omitzero"`
}

func (a *API) libraries(w http.ResponseWriter, r *http.Request) {
	libs, err := a.svc.Catalogue.Libraries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]libraryJSON, len(libs))
	for i, l := range libs {
		out[i] = libraryJSON{ID: l.ID, Name: l.Name, Kind: l.Kind}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": out})
}

func (a *API) wall(w http.ResponseWriter, r *http.Request) {
	lib, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	q := r.URL.Query()
	page := store.WallPage{Profile: sessionOf(r).Profile.ID, After: q.Get("after"), Limit: defaultWallLimit}
	if page.Sort, err = domain.ParseWallSort(cmp.Or(q.Get("sort"), string(domain.SortTitle))); err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	if page.Order, err = domain.ParseOrder(cmp.Or(q.Get("order"), string(page.Sort.DefaultOrder()))); err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	if s := q.Get("limit"); s != "" {
		if page.Limit, err = strconv.Atoi(s); err != nil || page.Limit < 1 || page.Limit > maxWallLimit {
			writeProblem(w, a.logger, codeInvalidParameter, "limit is a number from 1 to "+strconv.Itoa(maxWallLimit))
			return
		}
	}
	cards, next, err := a.svc.Catalogue.Wall(r.Context(), lib, page)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
		return
	case errors.Is(err, store.ErrBadCursor):
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, struct {
		Items []cardJSON `json:"items"`
		Next  string     `json:"next,omitzero"`
	}{cardsJSON(cards), next})
}

func cardsJSON(cards []store.Card) []cardJSON {
	out := make([]cardJSON, len(cards))
	for i, c := range cards {
		out[i] = cardJSON{
			ID: c.ID, Kind: c.Kind, Title: c.Title, Year: c.Year, ReleaseDate: domain.Date(c.ReleaseDate), AddedAt: c.AddedAt,
			Poster: c.Poster, Backdrop: c.Backdrop, State: c.State,
		}
	}
	return out
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.SearchQuery{Profile: sessionOf(r).Profile.ID, Text: q.Get("q"), Limit: defaultWallLimit}
	if query.Text == "" {
		writeProblem(w, a.logger, codeInvalidParameter, "q is what to search for")
		return
	}
	var err error
	if s := q.Get("library"); s != "" {
		if query.Library, err = uuid.Parse(s); err != nil {
			writeProblem(w, a.logger, codeInvalidParameter, "library is not an id")
			return
		}
	}
	if s := q.Get("limit"); s != "" {
		if query.Limit, err = strconv.Atoi(s); err != nil || query.Limit < 1 || query.Limit > maxWallLimit {
			writeProblem(w, a.logger, codeInvalidParameter, "limit is a number from 1 to "+strconv.Itoa(maxWallLimit))
			return
		}
	}
	cards, err := a.svc.Catalogue.Search(r.Context(), query)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, map[string]any{"items": cardsJSON(cards)})
}

func (a *API) title(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	page, err := a.svc.Catalogue.Title(r.Context(), sessionOf(r).Profile.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "")
	case err != nil:
		a.internal(w, r, err)
	default:
		writeJSON(w, a.logger, "application/json", http.StatusOK, page)
	}
}
