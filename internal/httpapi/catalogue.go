package httpapi

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/remote"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

const (
	defaultWallLimit = 50
	maxWallLimit     = 200
)

type catalogue interface {
	LibrariesSeen(ctx context.Context, profile uuid.UUID) ([]*store.SeenLibrary, error)
	Wall(ctx context.Context, libs []uuid.UUID, p store.WallPage) ([]uuid.UUID, store.Extent, error)
	Cards(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]store.Card, error)
	Letters(ctx context.Context, lib, profile uuid.UUID, wall domain.WallKind, f store.WallFilter) ([]store.Letter, error)
	Facets(ctx context.Context, libs []uuid.UUID, profile uuid.UUID, wall domain.WallKind) (store.Facets, error)
	Similar(ctx context.Context, profile, id uuid.UUID) ([]store.Card, error)
	Title(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
	Search(ctx context.Context, q store.SearchQuery) ([]store.Card, store.Extent, error)
	RowPage(ctx context.Context, profile uuid.UUID, row domain.HomeRow, page store.Page) ([]store.Card, store.Extent, error)
	LibraryRow(ctx context.Context, profile uuid.UUID, row domain.HomeRow, library *store.SeenLibrary, limit int) ([]store.Card, error)
	Next(ctx context.Context, profile, id uuid.UUID) (store.Card, error)
	PlayOrder(ctx context.Context, profile, id uuid.UUID) ([]store.Card, error)
	Calendar(ctx context.Context, q store.CalendarQuery) ([]store.CalendarDay, error)
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
	// An episode's card names its show and where in it it is, and carries its still and its
	// season's poster, which a search shows it by.
	Show          *titleRefJSON `json:"show,omitzero"`
	Season        *titleRefJSON `json:"season,omitzero"`
	SeasonPoster  uuid.UUID     `json:"season_poster,omitzero"`
	SeasonNumber  *int          `json:"season_number,omitzero"`
	EpisodeNumber *int          `json:"episode_number,omitzero"`
	EpisodeEnd    *int          `json:"episode_end,omitzero"`
	// An album's card names its artist, and a track's its album and artist, its place on the album
	// and the artists it names, which may be others.
	Artist      *titleRefJSON `json:"artist,omitzero"`
	Album       *titleRefJSON `json:"album,omitzero"`
	DiscNumber  *int          `json:"disc_number,omitzero"`
	TrackNumber *int          `json:"track_number,omitzero"`
	Artists     []string      `json:"artists,omitzero"`
	// A book's card names its author, and the series it is in and its place there, which may be 1.5.
	Author      *titleRefJSON `json:"author,omitzero"`
	Series      string        `json:"series,omitzero"`
	SeriesIndex *float64      `json:"series_index,omitzero"`
	// Thumb is the picture a card is drawn by where it is drawn wide: the still, or on a row
	// about where the profile is, the picture its card_art chose.
	Thumb uuid.UUID `json:"thumb,omitzero"`
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

func (a *API) libraries(r *http.Request, _ none) (listJSON[libraryJSON], error) {
	libs, err := a.svc.Catalogue.LibrariesSeen(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if err != nil {
		return listJSON[libraryJSON]{}, err
	}
	out := make([]libraryJSON, len(libs))
	for i, l := range libs {
		out[i] = libraryJSON{ID: l.ID, Name: l.Name, Kind: l.Kind}
	}
	return listJSON[libraryJSON]{Items: out}, nil
}

type libraryOrderJSON struct {
	LibraryIDs []uuid.UUID `json:"library_ids"`
}

// setLibraryOrder puts the profile's libraries in an order, as its sidebar lists them.
func (a *API) setLibraryOrder(r *http.Request, req libraryOrderJSON) (none, error) {
	err := a.svc.Catalogue.SetLibraryOrder(r.Context(), auth.SessionOf(r.Context()).Profile.ID, req.LibraryIDs)
	return none{}, notFoundAs(err, "a library is named twice, or is no library")
}

func (a *API) wall(r *http.Request, _ none) (pageJSON[cardJSON], error) {
	lib := pathUUID(r, "id")
	q := r.URL.Query()
	page := store.WallPage{Profile: auth.SessionOf(r.Context()).Profile.ID}
	var err error
	if page.Kind, err = queryEnum(r, "kind", domain.WallTitles, domain.WallKinds()); err != nil {
		return pageJSON[cardJSON]{}, err
	}
	if page.Sort, err = queryEnum(r, "sort", domain.SortTitle, domain.WallSorts()); err != nil {
		return pageJSON[cardJSON]{}, err
	}
	if page.Order, err = queryEnum(r, "order", page.Sort.DefaultOrder(), domain.Orders()); err != nil {
		return pageJSON[cardJSON]{}, err
	}
	if page.Filter, err = wallFilter(q); err != nil {
		return pageJSON[cardJSON]{}, refuse(codeInvalidParameter, err.Error())
	}
	page.RatingSite = cmp.Or(page.Filter.RatingSite, domain.SiteIMDb)
	paged, err := paging(r, defaultWallLimit)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	page.Offset, page.Limit, page.Count = paged.Offset, paged.Limit, paged.Count
	ids, extent, err := a.svc.Catalogue.Wall(r.Context(), []uuid.UUID{lib}, page)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	cards, err := a.svc.Catalogue.Cards(r.Context(), page.Profile, ids)
	if err != nil {
		return pageJSON[cardJSON]{}, err
	}
	return pageAt(cardsJSON(cards), page.Offset, extent), nil
}

// letters answers how many of a library's titles sort under each letter, in title order, so a
// client jumps to a letter at the sum of those before it.
func (a *API) letters(r *http.Request, _ none) (listJSON[letterJSON], error) {
	lib := pathUUID(r, "id")
	kind, err := queryEnum(r, "kind", domain.WallTitles, domain.WallKinds())
	if err != nil {
		return listJSON[letterJSON]{}, err
	}
	f, err := wallFilter(r.URL.Query())
	if err != nil {
		return listJSON[letterJSON]{}, refuse(codeInvalidParameter, err.Error())
	}
	letters, err := a.svc.Catalogue.Letters(r.Context(), lib, auth.SessionOf(r.Context()).Profile.ID, kind, f)
	if err != nil {
		return listJSON[letterJSON]{}, err
	}
	out := make([]letterJSON, len(letters))
	for i, l := range letters {
		out[i] = letterJSON(l)
	}
	return listJSON[letterJSON]{Items: out}, nil
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
		SeasonPoster: c.SeasonPoster, SeasonNumber: c.SeasonNumber, EpisodeNumber: c.EpisodeNumber, EpisodeEnd: c.EpisodeEnd, Thumb: c.Thumb,
		Origin: c.Origin, Overview: c.Overview, Logo: c.Logo, Genres: c.Genres, Certificate: domain.Bare(c.Certificate),
		Artist: (*titleRefJSON)(c.Artist), Album: (*titleRefJSON)(c.Album), DiscNumber: c.DiscNumber, TrackNumber: c.TrackNumber,
		Artists: c.Artists, Author: (*titleRefJSON)(c.Author), Series: c.Series, SeriesIndex: c.SeriesIndex, Blurhashes: c.Blurhashes,
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

// searchJSON is a page of the titles (films, shows, collections and episodes) and one of the
// people found, both from the same offset; a page the kinds asked for leave out is empty.
// Requestable are, in a search of every library's first page, the titles libraries of files'
// providers find that no library holds, which a profile may ask for; they are no titles of the
// server's, so no card of them is among Titles.
type searchJSON struct {
	Titles      pageJSON[cardJSON]      `json:"titles"`
	People      pageJSON[personRefJSON] `json:"people"`
	Requestable listJSON[cardJSON]      `json:"requestable"`
}

func (a *API) search(r *http.Request, _ none) (searchJSON, error) {
	q := r.URL.Query()
	query := store.SearchQuery{Profile: auth.SessionOf(r.Context()).Profile.ID, Text: q.Get("q")}
	if query.Text == "" {
		return searchJSON{}, refuse(codeInvalidParameter, "q is what to search for")
	}
	var err error
	if query.Library, err = queryID(r, "library"); err != nil {
		return searchJSON{}, err
	}
	kinds, err := parseAll(list(q, "kind"), enum("kind", domain.SearchKinds()))
	if err != nil {
		return searchJSON{}, refuse(codeInvalidParameter, err.Error())
	}
	page, err := paging(r, defaultWallLimit)
	if err != nil {
		return searchJSON{}, err
	}
	query.Offset, query.Limit, query.Count = page.Offset, page.Limit, page.Count
	findPeople := len(kinds) == 0
	for _, k := range kinds {
		switch k {
		case domain.SearchPerson:
			findPeople = true
		case domain.SearchMovie, domain.SearchShow, domain.SearchCollection, domain.SearchEpisode:
			query.Kinds = append(query.Kinds, domain.ItemKind(k))
		}
	}
	out := searchJSON{
		Titles: pageJSON[cardJSON]{Offset: query.Offset},
		People: pageJSON[personRefJSON]{Offset: query.Offset},
	}
	ctx := r.Context()
	var wg sync.WaitGroup
	var titlesErr, peopleErr, requestableErr error
	if len(kinds) == 0 || len(query.Kinds) > 0 {
		everyLibrary := query.Offset == 0 && query.Library == (uuid.UUID{})
		wg.Go(func() {
			var cards []store.Card
			var extent store.Extent
			cards, extent, titlesErr = a.svc.Catalogue.Search(ctx, query)
			// A search of every library shows first, in its first page, what its remote libraries'
			// providers find that they do not hold yet, which becomes theirs as it is opened.
			if titlesErr == nil && everyLibrary {
				var found []store.Discovery
				found, titlesErr = a.svc.Discover.Find(ctx, query.Profile, domain.MediaRemote, query.Text, query.Kinds)
				for _, d := range found {
					cards = append(cards, d.Card())
				}
				if extent.Total != nil {
					extent.Total = new(*extent.Total + int64(len(found)))
				}
			}
			out.Titles = pageAt(cardsJSON(cards), query.Offset, extent)
		})
		if everyLibrary {
			wg.Go(func() {
				var found []store.Discovery
				found, requestableErr = a.svc.Discover.Find(ctx, query.Profile, domain.MediaFolder, query.Text, query.Kinds)
				out.Requestable.Items = each(found, func(d store.Discovery) cardJSON { return cardOf(d.Card()) })
			})
		}
	}
	if findPeople {
		wg.Go(func() {
			found, extent, err := a.svc.People.SearchPeople(ctx, query.Text, page)
			out.People = pageAt(make([]personRefJSON, len(found)), query.Offset, extent)
			for i, p := range found {
				out.People.Items[i] = personRefJSON(p)
			}
			peopleErr = err
		})
	}
	wg.Wait()
	if err := errors.Join(titlesErr, peopleErr, requestableErr); err != nil {
		return searchJSON{}, err
	}
	return out, nil
}

func (a *API) title(w http.ResponseWriter, r *http.Request) {
	id := pathUUID(r, "id")
	profile := auth.SessionOf(r.Context()).Profile.ID
	// A remote film or episode is given a version as its page opens, as a player chooses its version
	// from the page before it plays; one its provider cannot give is a page with none, and one
	// it is fetching says so.
	copied := a.svc.RemoteVersions.Ensure(r.Context(), id)
	if copied != nil && !errors.Is(copied, remote.ErrFetching) {
		a.logger.WarnContext(r.Context(), "no version of a remote title", slog.Any("err", copied))
	}
	page, err := a.svc.Catalogue.Title(r.Context(), profile, id)
	// One no library holds may be a title a search found for the profile to ask for.
	requestable := errors.Is(err, store.ErrNotFound)
	if requestable {
		page, err = a.svc.RequestablePages.Page(r.Context(), profile, id)
	}
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
	out := titlePageOf(page, words.Negotiate(w, r))
	out.Fetching = errors.Is(copied, remote.ErrFetching)
	out.Requestable = requestable
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// wallKindParameter is what a wall pages, as Plex's type and Jellyfin's IncludeItemTypes ask for.
var wallKindParameter = param{"kind", domain.WallTitles, "What to page: titles, films or shows alone, or a library's episodes across its shows."}

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

func wallFilter(q url.Values) (store.WallFilter, error) {
	f := store.WallFilter{StartsWith: strings.ToUpper(q.Get("starts_with"))}
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
		if f.MinRating, err = strconv.ParseFloat(s, 64); err != nil {
			return f, errors.New("min_rating is a score from 0 to 100")
		}
		f.RatingSite = cmp.Or(f.RatingSite, domain.SiteIMDb)
	}
	return f, f.Check()
}

type facetsJSON struct {
	Genres       []string               `json:"genres"`
	Years        []int                  `json:"years"`
	Certificates []certificateFacetJSON `json:"certificates"`
	Studios      []string               `json:"studios"`
	Resolutions  []domain.Resolution    `json:"resolutions"`
	Ranges       []domain.Range         `json:"ranges"`
	RatingSites  []domain.RatingSite    `json:"rating_sites"`
	Marks        []domain.Mark          `json:"marks"`
}

// certificateFacetJSON is a certificate a wall can be narrowed to: Value, with its country, is what
// the filter takes, Name what a viewer is shown.
type certificateFacetJSON struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// facets answers the values a library's titles have, which its wall can be narrowed to.
func (a *API) facets(r *http.Request, _ none) (facetsJSON, error) {
	lib := pathUUID(r, "id")
	kind, err := queryEnum(r, "kind", domain.WallTitles, domain.WallKinds())
	if err != nil {
		return facetsJSON{}, err
	}
	f, err := a.svc.Catalogue.Facets(r.Context(), []uuid.UUID{lib}, auth.SessionOf(r.Context()).Profile.ID, kind)
	if err != nil {
		return facetsJSON{}, err
	}
	return facetsJSON{
		f.Genres, f.Years,
		each(f.Certificates, func(c string) certificateFacetJSON { return certificateFacetJSON{domain.Bare(c), c} }),
		f.Studios, f.Resolutions,
		f.Ranges, f.RatingSites, domain.Marks(),
	}, nil
}

// similar answers the titles most like one, for "More like this".
func (a *API) similar(r *http.Request, _ none) (listJSON[cardJSON], error) {
	id := pathUUID(r, "id")
	cards, err := a.svc.Catalogue.Similar(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if err != nil {
		return listJSON[cardJSON]{}, err
	}
	return listJSON[cardJSON]{Items: cardsJSON(cards)}, nil
}

// next answers the episode to play after a title: after an episode the one that follows it, and
// of a show or season the one the profile is at, as Jellyfin's NextUp and Plex's onDeck.
func (a *API) next(r *http.Request, _ none) (cardJSON, error) {
	id := pathUUID(r, "id")
	card, err := a.svc.Catalogue.Next(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if err != nil {
		return cardJSON{}, err
	}
	return cardOf(card), nil
}

// playable answers the films and episodes a title stands for, in the order they play, for a client
// to shuffle a show, season or collection.
func (a *API) playable(r *http.Request, _ none) (listJSON[cardJSON], error) {
	id := pathUUID(r, "id")
	cards, err := a.svc.Catalogue.PlayOrder(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if err != nil {
		return listJSON[cardJSON]{}, err
	}
	return listJSON[cardJSON]{Items: cardsJSON(cards)}, nil
}

func (a *API) catalogueRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "PUT /api/v1/profile/library-order", access: signedIn,
			summary: "Put the profile's libraries in an order; those left out follow, by name",
			status:  http.StatusNoContent,
		}, a.setLibraryOrder),
		a.handles(route{
			pattern: "GET /api/v1/libraries", access: signedIn, summary: "List the libraries the profile sees",
			status: http.StatusOK,
		}, a.libraries),
		a.handles(route{
			pattern: "GET /api/v1/libraries/{id}/titles", access: signedIn, summary: "Page a library's titles",
			query: slices.Concat([]param{
				wallKindParameter,
				{"sort", domain.SortTitle, "The order, title by default; of episodes, by show."},
				{"order", domain.Ascending, "Its direction; titles from A, anything else the newest first, by default."},
			}, pageParams, wallFilterParameters),
			status: http.StatusOK,
		}, a.wall),
		a.handles(route{
			pattern: "GET /api/v1/libraries/{id}/letters", access: signedIn,
			summary: "Count a library's titles under each letter, in title order",
			query:   append([]param{wallKindParameter}, wallFilterParameters...), status: http.StatusOK,
		}, a.letters),
		a.handles(route{
			pattern: "GET /api/v1/libraries/{id}/facets", access: signedIn,
			summary: "The values a library's titles can be narrowed to",
			query:   []param{wallKindParameter}, status: http.StatusOK,
		}, a.facets),
		a.handles(route{
			pattern: "GET /api/v1/titles/{id}/next", access: signedIn,
			summary: "The episode to play next: after an episode, or where the profile is in a show or season",
			status:  http.StatusOK,
		}, a.next),
		a.handles(route{
			pattern: "GET /api/v1/titles/{id}/playable", access: signedIn,
			summary: "The films and episodes a title stands for, in the order they play",
			status:  http.StatusOK,
		}, a.playable),
		a.handles(route{
			pattern: "GET /api/v1/titles/{id}/lyrics", access: signedIn,
			summary: "A track's lyrics, each line where it is sung where they are timed",
			status:  http.StatusOK,
		}, a.lyrics),
		a.handles(route{
			pattern: "GET /api/v1/titles/{id}/similar", access: signedIn, summary: "The titles most like one",
			status: http.StatusOK,
		}, a.similar),
		{
			pattern: "GET /api/v1/titles/{id}", access: signedIn, summary: "A title's page, or one a search found for the profile to ask for",
			status: http.StatusOK, reply: titlePageJSON{}, handle: a.title,
		},
		a.handles(route{
			pattern: "GET /api/v1/search", access: signedIn, summary: "Page the titles, episodes among them, and the people a search finds",
			query: append([]param{
				{"q", "", "What to search for; required."},
				{"library", uuid.UUID{}, "Only this library's titles."},
				{"kind", []domain.SearchKind{}, "Only these kinds of title, or people; repeated or comma-separated, everything by default."},
			}, pageParams...),
			status: http.StatusOK,
		}, a.search),
	}
}
