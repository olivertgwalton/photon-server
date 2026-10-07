package httpapi

import (
	"cmp"
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	RowPage(ctx context.Context, profile uuid.UUID, row domain.HomeRow, offset, limit int) ([]store.Card, int64, error)
	Next(ctx context.Context, profile, id uuid.UUID) (store.Card, error)
	LibraryOrder(ctx context.Context, profile uuid.UUID) ([]uuid.UUID, error)
	SetLibraryOrder(ctx context.Context, profile uuid.UUID, libs []uuid.UUID) error
}

type libraryJSON struct {
	ID   uuid.UUID          `json:"id"`
	Name string             `json:"name"`
	Kind domain.LibraryKind `json:"kind"`
}

type cardJSON struct {
	ID           uuid.UUID       `json:"id"`
	Kind         domain.ItemKind `json:"kind"`
	Title        string          `json:"title"`
	Year         int             `json:"year,omitzero"`
	ReleaseDate  domain.Date     `json:"release_date,omitzero"`
	AddedAt      time.Time       `json:"added_at"`
	Poster       uuid.UUID       `json:"poster,omitzero"`
	Backdrop     uuid.UUID       `json:"backdrop,omitzero"`
	State        titleStateJSON  `json:"state,omitzero"`
	DurationMS   int64           `json:"duration_ms,omitzero"`
	VersionCount int             `json:"version_count,omitzero"`
	// An episode's card names its show and where in it it is, and carries its still.
	Show          *titleRefJSON `json:"show,omitzero"`
	Season        *titleRefJSON `json:"season,omitzero"`
	SeasonNumber  *int          `json:"season_number,omitzero"`
	EpisodeNumber *int          `json:"episode_number,omitzero"`
	EpisodeEnd    *int          `json:"episode_end,omitzero"`
	Thumb         uuid.UUID     `json:"thumb,omitzero"`
	// Origin is who made a collection: only an admin's is changed through the admin routes.
	Origin      domain.CollectionOrigin `json:"origin,omitzero"`
	Overview    string                  `json:"overview,omitzero"`
	Logo        uuid.UUID               `json:"logo,omitzero"`
	Genres      []string                `json:"genres,omitzero"`
	Certificate string                  `json:"certificate,omitzero"`
	// Ratings are each site's score out of 100, as the title's page gives them.
	Ratings []ratingRefJSON `json:"ratings,omitzero"`
	// Blurhashes are those of its pictures that have one, by id, to draw while they load.
	Blurhashes store.Blurhashes `json:"blurhashes,omitzero"`
}

func (a *API) libraries(w http.ResponseWriter, r *http.Request) {
	libs, err := a.svc.Catalogue.Libraries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	profile := sessionOf(r).Profile.ID
	order, err := a.svc.Catalogue.LibraryOrder(r.Context(), profile)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// In the profile's order, those it has not placed after, by name.
	rank := func(id uuid.UUID) int {
		if n := slices.Index(order, id); n >= 0 {
			return n
		}
		return len(order)
	}
	slices.SortStableFunc(libs, func(x, y domain.Library) int { return cmp.Compare(rank(x.ID), rank(y.ID)) })
	out := make([]libraryJSON, len(libs))
	for i, l := range libs {
		out[i] = libraryJSON{ID: l.ID, Name: l.Name, Kind: l.Kind}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[libraryJSON]{Items: out})
}

type libraryOrderJSON struct {
	LibraryIDs []uuid.UUID `json:"library_ids"`
}

// setLibraryOrder puts the profile's libraries in an order, as its sidebar lists them.
func (a *API) setLibraryOrder(w http.ResponseWriter, r *http.Request) {
	var req libraryOrderJSON
	if !a.decode(w, r, &req) {
		return
	}
	err := a.svc.Catalogue.SetLibraryOrder(r.Context(), sessionOf(r).Profile.ID, req.LibraryIDs)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, a.logger, codeNotFound, "a library is named twice, or is no library")
	case a.answered(w, r, err):
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) wall(w http.ResponseWriter, r *http.Request) {
	lib, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	q := r.URL.Query()
	page := store.WallPage{Profile: sessionOf(r).Profile.ID}
	if page.Sort, ok = queryEnum(a, w, r, "sort", domain.SortTitle, domain.WallSorts()); !ok {
		return
	}
	if page.Order, ok = queryEnum(a, w, r, "order", page.Sort.DefaultOrder(), domain.Orders()); !ok {
		return
	}
	var err error
	if page.Filter, err = wallFilter(q); err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
	page.RatingSite = cmp.Or(page.Filter.RatingSite, domain.SiteIMDb)
	if page.Offset, page.Limit, ok = a.paging(w, r, defaultWallLimit); !ok {
		return
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
		Poster: c.Poster, Backdrop: c.Backdrop, State: titleStateJSON(c.State), DurationMS: c.DurationMS, VersionCount: c.VersionCount, Show: (*titleRefJSON)(c.Show), Season: (*titleRefJSON)(c.Season),
		SeasonNumber: c.SeasonNumber, EpisodeNumber: c.EpisodeNumber, EpisodeEnd: c.EpisodeEnd, Thumb: c.Thumb,
		Origin: c.Origin, Overview: c.Overview, Logo: c.Logo, Genres: c.Genres, Certificate: c.Certificate,
		Blurhashes: c.Blurhashes,
	}
	for _, r := range c.Ratings {
		out.Ratings = append(out.Ratings, ratingRefJSON(r))
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
	var ok bool
	if query.Library, ok = a.queryID(w, r, "library"); !ok {
		return
	}
	kinds, err := parseAll(list(q, "kind"), enum("kind", domain.SearchKinds()))
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return
	}
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
	ctx := r.Context()
	var wg sync.WaitGroup
	var titlesErr, peopleErr error
	if len(kinds) == 0 || len(query.Kinds) > 0 {
		wg.Go(func() {
			var cards []store.Card
			cards, out.Total, titlesErr = a.svc.Catalogue.Search(ctx, query)
			out.Items = cardsJSON(cards)
		})
	}
	if findPeople {
		wg.Go(func() {
			var found []store.PersonRef
			found, out.PeopleTotal, peopleErr = a.svc.People.SearchPeople(ctx, query.Text, query.Offset, query.Limit)
			out.People = make([]personRefJSON, len(found))
			for i, p := range found {
				out.People[i] = personRefJSON(p)
			}
		})
	}
	wg.Wait()
	if err := errors.Join(titlesErr, peopleErr); err != nil {
		a.internal(w, r, err)
		return
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
	writeJSON(w, a.logger, "application/json", http.StatusOK, titlePageOf(page))
}

const defaultHomeLimit = 20

type homeRowJSON struct {
	Kind domain.HomeRow `json:"kind"`
	// Collection is the collection a row of kind collection is.
	Collection *titleRefJSON `json:"collection,omitzero"`
	// Library is the library a row of its titles is of: recently added, recently released and top
	// rated are a row for each library.
	Library *libraryRefJSON `json:"library,omitzero"`
	Items   []cardJSON      `json:"items"`
}

type libraryRefJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
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
		out.Rows[i] = homeRowJSON{Kind: row.Kind, Collection: (*titleRefJSON)(row.Collection), Library: (*libraryRefJSON)(row.Library), Items: cardsJSON(row.Cards)}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// homeRow answers a page of one of the profile's own home rows, in the row's order.
func (a *API) homeRow(w http.ResponseWriter, r *http.Request) {
	offset, limit, ok := a.paging(w, r, defaultWallLimit)
	if !ok {
		return
	}
	row := domain.HomeRow(r.PathValue("row"))
	cards, total, err := a.svc.Catalogue.RowPage(r.Context(), sessionOf(r).Profile.ID, row, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[cardJSON]{cardsJSON(cards), offset, total})
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
	if offset, ok = a.queryNumber(w, r, "offset", 0, 0, math.MaxInt); !ok {
		return 0, 0, false
	}
	limit, ok = a.queryNumber(w, r, "limit", defaultLimit, 1, maxWallLimit)
	return offset, limit, ok
}
