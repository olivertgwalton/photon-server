package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	Wall(ctx context.Context, lib uuid.UUID, p store.WallPage) ([]store.Card, int64, error)
	Letters(ctx context.Context, lib, profile uuid.UUID, f store.WallFilter) ([]store.Letter, error)
	Facets(ctx context.Context, lib, profile uuid.UUID) (store.Facets, error)
	Similar(ctx context.Context, profile, id uuid.UUID) ([]store.Card, error)
	Title(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
	Search(ctx context.Context, q store.SearchQuery) ([]store.Card, int64, error)
	Home(ctx context.Context, profile uuid.UUID, limit int) ([]store.HomeRow, error)
	Next(ctx context.Context, profile, id uuid.UUID) (store.Card, error)
	LibraryCounts(ctx context.Context, profile uuid.UUID) (map[uuid.UUID]domain.TitleCounts, error)
}

type libraryJSON struct {
	ID     uuid.UUID          `json:"id"`
	Name   string             `json:"name"`
	Kind   domain.LibraryKind `json:"kind"`
	Counts countsJSON         `json:"counts"`
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
	DurationMS  int64            `json:"duration_ms,omitzero"`
	// An episode's card names its show and where in it it is, and carries its still.
	Show          *store.TitleRef `json:"show,omitzero"`
	SeasonNumber  *int            `json:"season_number,omitzero"`
	EpisodeNumber *int            `json:"episode_number,omitzero"`
	EpisodeEnd    *int            `json:"episode_end,omitzero"`
	Thumb         uuid.UUID       `json:"thumb,omitzero"`
	// Origin is who made a collection: only an admin's is changed through the admin routes.
	Origin      domain.CollectionOrigin `json:"origin,omitzero"`
	Overview    string                  `json:"overview,omitzero"`
	Logo        uuid.UUID               `json:"logo,omitzero"`
	Genres      []string                `json:"genres,omitzero"`
	Certificate string                  `json:"certificate,omitzero"`
	// Ratings are each site's score out of 100, as the title's page gives them.
	Ratings []store.RatingRef `json:"ratings,omitzero"`
	// Blurhashes are those of its pictures that have one, by id, to draw while they load.
	Blurhashes store.Blurhashes `json:"blurhashes,omitzero"`
}

func (a *API) libraries(w http.ResponseWriter, r *http.Request) {
	libs, err := a.svc.Catalogue.Libraries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	counts, err := a.svc.Catalogue.LibraryCounts(r.Context(), sessionOf(r).Profile.ID)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]libraryJSON, len(libs))
	for i, l := range libs {
		out[i] = libraryJSON{ID: l.ID, Name: l.Name, Kind: l.Kind, Counts: countsJSON(counts[l.ID])}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[libraryJSON]{Items: out})
}

func (a *API) wall(w http.ResponseWriter, r *http.Request) {
	lib, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	q := r.URL.Query()
	page := store.WallPage{Profile: sessionOf(r).Profile.ID, Limit: defaultWallLimit}
	var err error
	if page.Sort, err = domain.Parse("sort", cmp.Or(q.Get("sort"), string(domain.SortTitle)), domain.WallSorts()); err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	if page.Order, err = domain.Parse("order", cmp.Or(q.Get("order"), string(page.Sort.DefaultOrder())), domain.Orders()); err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	if s := q.Get("limit"); s != "" {
		if page.Limit, err = strconv.Atoi(s); err != nil || page.Limit < 1 || page.Limit > maxWallLimit {
			writeProblem(w, a.logger, codeInvalidParameter, "limit is a number from 1 to "+strconv.Itoa(maxWallLimit))
			return
		}
	}
	if page.Filter, err = wallFilter(q); err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	page.RatingSite = cmp.Or(page.Filter.RatingSite, domain.SiteIMDb)
	if s := q.Get("offset"); s != "" {
		if page.Offset, err = strconv.Atoi(s); err != nil || page.Offset < 0 {
			writeProblem(w, a.logger, codeInvalidParameter, "offset is a number from 0")
			return
		}
	}
	cards, total, err := a.svc.Catalogue.Wall(r.Context(), lib, page)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[cardJSON]{cardsJSON(cards), page.Offset, total})
}

// letters answers how many of a library's titles sort under each letter, in title order, so a
// client jumps to a letter at the sum of those before it.
func (a *API) letters(w http.ResponseWriter, r *http.Request) {
	lib, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	f, err := wallFilter(r.URL.Query())
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	letters, err := a.svc.Catalogue.Letters(r.Context(), lib, sessionOf(r).Profile.ID, f)
	if a.answered(w, r, err) {
		return
	}
	out := make([]letterJSON, len(letters))
	for i, l := range letters {
		out[i] = letterJSON(l)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[letterJSON]{Items: out})
}

type letterJSON struct {
	Letter string `json:"letter"`
	Count  int    `json:"count"`
}

func cardsJSON(cards []store.Card) []cardJSON {
	out := make([]cardJSON, len(cards))
	for i, c := range cards {
		out[i] = cardOf(c)
	}
	return out
}

func cardOf(c store.Card) cardJSON {
	out := cardJSON{
		ID: c.ID, Kind: c.Kind, Title: c.Title, Year: c.Year, ReleaseDate: domain.Date(c.ReleaseDate), AddedAt: c.AddedAt,
		Poster: c.Poster, Backdrop: c.Backdrop, State: c.State, DurationMS: c.DurationMS, Show: c.Show,
		SeasonNumber: c.SeasonNumber, EpisodeNumber: c.EpisodeNumber, EpisodeEnd: c.EpisodeEnd, Thumb: c.Thumb,
		Origin: c.Origin, Overview: c.Overview, Logo: c.Logo, Genres: c.Genres, Certificate: c.Certificate,
		Blurhashes: c.Blurhashes,
	}
	for _, r := range c.Ratings {
		out.Ratings = append(out.Ratings, store.RatingRef(r))
	}
	return out
}

type personRefJSON struct {
	ID         uuid.UUID        `json:"id"`
	Name       string           `json:"name"`
	Photo      uuid.UUID        `json:"photo,omitzero"`
	Blurhashes store.Blurhashes `json:"blurhashes,omitzero"`
}

// searchJSON is a page of the titles (films, shows, collections and episodes) and of the people
// found, both from offset, and how many of each there are in all; a section the kinds asked for
// leave out is empty.
type searchJSON struct {
	Items       []cardJSON      `json:"items"`
	People      []personRefJSON `json:"people"`
	Offset      int             `json:"offset"`
	Total       int64           `json:"total"`
	PeopleTotal int64           `json:"people_total"`
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.SearchQuery{Profile: sessionOf(r).Profile.ID, Text: q.Get("q")}
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
	kinds, err := parseAll(list(q, "kind"), enum("kind", domain.SearchKinds()))
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	var ok bool
	if query.Offset, query.Limit, ok = a.paging(w, r, defaultWallLimit); !ok {
		return
	}
	findPeople := len(kinds) == 0
	for _, k := range kinds {
		switch k {
		case domain.SearchPerson:
			findPeople = true
		case domain.SearchMovie, domain.SearchShow, domain.SearchCollection, domain.SearchEpisode:
			query.Kinds = append(query.Kinds, domain.ItemKind(k))
		}
	}
	out := searchJSON{Items: []cardJSON{}, People: []personRefJSON{}, Offset: query.Offset}
	if len(kinds) == 0 || len(query.Kinds) > 0 {
		cards, total, err := a.svc.Catalogue.Search(r.Context(), query)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		out.Items, out.Total = cardsJSON(cards), total
	}
	if findPeople {
		found, total, err := a.svc.People.SearchPeople(r.Context(), query.Text, query.Offset, query.Limit)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		out.People, out.PeopleTotal = make([]personRefJSON, len(found)), total
		for i, p := range found {
			out.People[i] = personRefJSON(p)
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

func (a *API) title(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	profile := sessionOf(r).Profile.ID
	page, err := a.svc.Catalogue.Title(r.Context(), profile, id)
	if a.answered(w, r, err) {
		return
	}
	if a.answered(w, r, a.chooseTracks(r.Context(), profile, &page)) {
		return
	}
	// Signed until the end of the day after next, so an address stands all day and a client's
	// cache keeps finding the picture under it.
	until := time.Now().Truncate(24 * time.Hour).Add(48 * time.Hour)
	page.SignChapterImages(func(path string) string { return a.svc.Signer.Sign(path, until) })
	writeJSON(w, a.logger, "application/json", http.StatusOK, page)
}

const defaultHomeLimit = 20

type homeRowJSON struct {
	Kind  domain.HomeRow `json:"kind"`
	Items []cardJSON     `json:"items"`
}

type homeJSON struct {
	Rows []homeRowJSON `json:"rows"`
}

// home answers the profile's home page: its rows with anything in them, in the order to show.
func (a *API) home(w http.ResponseWriter, r *http.Request) {
	// The route takes no offset.
	_, limit, ok := a.paging(w, r, defaultHomeLimit)
	if !ok {
		return
	}
	rows, err := a.svc.Catalogue.Home(r.Context(), sessionOf(r).Profile.ID, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := homeJSON{Rows: make([]homeRowJSON, len(rows))}
	for i, row := range rows {
		out.Rows[i] = homeRowJSON{Kind: row.Kind, Items: cardsJSON(row.Cards)}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// wallFilterParameters are what a wall, and its letters, are narrowed by: each list repeated or
// comma-separated, any of its values.
var wallFilterParameters = []param{
	{"starts_with", "", "A letter, or # for anything before A."},
	{"mark", []domain.Mark{}, ""},
	{"genre", []string{}, ""},
	{"year", []int{}, ""},
	{"certificate", []string{}, ""},
	{"studio", []string{}, ""},
	{"resolution", []domain.Resolution{}, ""},
	{"range", []domain.Range{}, ""},
	{"rating_site", domain.RatingSite(""), "The site min_rating and the rating sort go by, IMDb by default."},
	{"min_rating", 0.0, "A score from 0 to 100."},
	{"person", []uuid.UUID{}, "People credited."},
}

// list is the values of the list parameter name, repeated or comma-separated.
func list(q url.Values, name string) []string {
	var out []string
	for _, v := range q[name] {
		out = append(out, strings.Split(v, ",")...)
	}
	return out
}

func wallFilter(q url.Values) (store.WallFilter, error) {
	var f store.WallFilter
	if s := q.Get("starts_with"); s != "" {
		if f.StartsWith = strings.ToUpper(s); f.StartsWith != "#" && (len(f.StartsWith) != 1 || f.StartsWith < "A" || f.StartsWith > "Z") {
			return f, errors.New("starts_with is a letter or #")
		}
	}
	var err error
	if f.Marks, err = parseAll(list(q, "mark"), enum("mark", domain.Marks())); err != nil {
		return f, err
	}
	if f.Resolutions, err = parseAll(list(q, "resolution"), enum("resolution", domain.Resolutions())); err != nil {
		return f, err
	}
	if f.Ranges, err = parseAll(list(q, "range"), enum("range", domain.Ranges())); err != nil {
		return f, err
	}
	if f.Years, err = parseAll(list(q, "year"), strconv.Atoi); err != nil {
		return f, errors.New("year is a year")
	}
	f.Genres, f.Certificates, f.Studios = list(q, "genre"), list(q, "certificate"), list(q, "studio")
	if f.People, err = parseAll(list(q, "person"), uuid.Parse); err != nil {
		return f, errors.New("person is a person's id")
	}
	if s := q.Get("rating_site"); s != "" {
		if f.RatingSite, err = domain.Parse("rating site", s, domain.RatingSites()); err != nil {
			return f, err
		}
	}
	if s := q.Get("min_rating"); s != "" {
		if f.MinRating, err = strconv.ParseFloat(s, 64); err != nil || f.MinRating < 0 || f.MinRating > 100 {
			return f, errors.New("min_rating is a score from 0 to 100")
		}
		f.RatingSite = cmp.Or(f.RatingSite, domain.SiteIMDb)
	}
	return f, nil
}

// enum parses one of all for parseAll.
func enum[T ~string](what string, all []T) func(string) (T, error) {
	return func(s string) (T, error) { return domain.Parse(what, s, all) }
}

func parseAll[T any](values []string, parse func(string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(values))
	for _, v := range values {
		p, err := parse(v)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

type facetsJSON struct {
	Genres       []string            `json:"genres"`
	Years        []int               `json:"years"`
	Certificates []string            `json:"certificates"`
	Studios      []string            `json:"studios"`
	Resolutions  []domain.Resolution `json:"resolutions"`
	Ranges       []domain.Range      `json:"ranges"`
	RatingSites  []domain.RatingSite `json:"rating_sites"`
	Marks        []domain.Mark       `json:"marks"`
}

// facets answers the values a library's titles have, which its wall can be narrowed to.
func (a *API) facets(w http.ResponseWriter, r *http.Request) {
	lib, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	f, err := a.svc.Catalogue.Facets(r.Context(), lib, sessionOf(r).Profile.ID)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, facetsJSON{
		nonNil(f.Genres), nonNil(f.Years), nonNil(f.Certificates), nonNil(f.Studios), nonNil(f.Resolutions),
		nonNil(f.Ranges), nonNil(f.RatingSites), domain.Marks(),
	})
}

// similar answers the titles most like one, for "More like this".
func (a *API) similar(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	cards, err := a.svc.Catalogue.Similar(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[cardJSON]{Items: cardsJSON(cards)})
}

// next answers the episode to play after a title: after an episode the one that follows it, and
// of a show or season the one the profile is at, as Jellyfin's NextUp and Plex's onDeck.
func (a *API) next(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	card, err := a.svc.Catalogue.Next(r.Context(), sessionOf(r).Profile.ID, id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, cardOf(card))
}

// paging reads a page's offset and limit, as walls page.
func (a *API) paging(w http.ResponseWriter, r *http.Request, defaultLimit int) (offset, limit int, ok bool) {
	q := r.URL.Query()
	offset, limit = 0, defaultLimit
	var err error
	if s := q.Get("offset"); s != "" {
		if offset, err = strconv.Atoi(s); err != nil || offset < 0 {
			writeProblem(w, a.logger, codeInvalidParameter, "offset is a number from 0")
			return 0, 0, false
		}
	}
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > maxWallLimit {
			writeProblem(w, a.logger, codeInvalidParameter, "limit is a number from 1 to "+strconv.Itoa(maxWallLimit))
			return 0, 0, false
		}
	}
	return offset, limit, true
}
