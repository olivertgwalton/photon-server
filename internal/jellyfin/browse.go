package jellyfin

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

type catalogue interface {
	LibrariesSeen(ctx context.Context, profile uuid.UUID) ([]*store.SeenLibrary, error)
	Wall(ctx context.Context, libs []uuid.UUID, p store.WallPage) ([]store.Card, int64, error)
	Search(ctx context.Context, q store.SearchQuery) ([]store.Card, int64, error)
	Title(ctx context.Context, profile, id uuid.UUID) (store.TitlePage, error)
	Seasons(ctx context.Context, profile, show uuid.UUID) ([]store.SeasonCard, error)
	Episodes(ctx context.Context, profile, of uuid.UUID) ([]store.Card, error)
	Next(ctx context.Context, profile, id uuid.UUID) (store.Card, error)
	Calendar(ctx context.Context, q store.CalendarQuery) ([]store.CalendarDay, error)
	AnnouncedEpisode(ctx context.Context, profile, id uuid.UUID) (store.Card, error)
	RowPage(ctx context.Context, profile uuid.UUID, row domain.HomeRow, offset, limit int) ([]store.Card, int64, error)
	LibraryRow(ctx context.Context, profile uuid.UUID, row domain.HomeRow, library uuid.UUID, limit int) ([]store.Card, error)
	Versions(ctx context.Context, items []uuid.UUID) (map[uuid.UUID][]store.VersionPage, error)
	ExternalIDs(ctx context.Context, items []uuid.UUID) (map[uuid.UUID]map[domain.Provider]string, error)
	Picture(ctx context.Context, id uuid.UUID) (domain.Picture, error)
}

type pictureFiles interface {
	Open(ctx context.Context, id uuid.UUID, p domain.Picture, width, height int) (blob.Object, string, error)
}

// queryResult is Jellyfin's BaseItemDtoQueryResult. TotalRecordCount is of every match, not the
// page, as apps page by it.
type queryResult struct {
	Items            []item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
	StartIndex       int    `json:"StartIndex"`
}

// listed is a page of a list as asked for: where it starts, how many at most (all, where an app
// says none, as Jellyfin answers), and the optional fields it wants.
type listed struct {
	start, limit int
	fields       map[string]bool
	// words names its copies and tracks in the reader's language.
	words words.Words
}

func listedOf(w http.ResponseWriter, r *http.Request) listed {
	l := listed{limit: math.MaxInt32, fields: map[string]bool{}, words: words.Negotiate(w, r)}
	if n, err := strconv.Atoi(query(r, "startIndex")); err == nil && n > 0 {
		l.start = n
	}
	if n, err := strconv.Atoi(query(r, "limit")); err == nil && n >= 0 {
		l.limit = n
	}
	for _, f := range values(r, "fields") {
		l.fields[strings.ToLower(f)] = true
	}
	return l
}

// values are a list parameter's values, sent comma-joined as Jellyfin's apps send them.
func values(r *http.Request, name string) []string {
	var out []string
	for v := range strings.SplitSeq(query(r, name), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func has(vs []string, v string) bool {
	return slices.ContainsFunc(vs, func(s string) bool { return strings.EqualFold(s, v) })
}

// list writes cards as items, with their copies and providers' ids where the app asked for them:
// read for the whole page at once.
func (a *API) list(ctx context.Context, cards []store.Card, l listed) ([]item, error) {
	out := make([]item, len(cards))
	ids := make([]uuid.UUID, len(cards))
	for n, c := range cards {
		out[n], ids[n] = a.fromCard(c), c.ID
	}
	if l.fields["mediasources"] || l.fields["mediastreams"] {
		versions, err := a.svc.Catalogue.Versions(ctx, ids)
		if err != nil {
			return nil, err
		}
		for n, id := range ids {
			if v := versions[id]; len(v) > 0 {
				out[n].sources(v, l.fields["mediastreams"], l.words)
			}
		}
	}
	if l.fields["providerids"] {
		external, err := a.svc.Catalogue.ExternalIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for n, id := range ids {
			out[n].ProviderIDs = providerIDs(external[id])
		}
	}
	for n := range out {
		out[n].Etag = etag(out[n])
	}
	return out, nil
}

func (a *API) writeList(w http.ResponseWriter, r *http.Request, cards []store.Card, total, start int, l listed) {
	items, err := a.list(r.Context(), cards, l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, queryResult{Items: items, TotalRecordCount: total, StartIndex: start})
}

// seenLibraries are the libraries the profile sees, by id.
func (a *API) seenLibraries(r *http.Request) ([]*store.SeenLibrary, map[uuid.UUID]*store.SeenLibrary, error) {
	libs, err := a.svc.Catalogue.LibrariesSeen(r.Context(), sessionOf(r).Profile.ID)
	byID := map[uuid.UUID]*store.SeenLibrary{}
	for _, l := range libs {
		byID[l.ID] = l
	}
	return libs, byID, err
}

// views are the libraries as Jellyfin's apps open them, in the profile's order.
func (a *API) views(w http.ResponseWriter, r *http.Request) {
	libs, _, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := queryResult{Items: make([]item, len(libs)), TotalRecordCount: len(libs)}
	for n, l := range libs {
		out.Items[n] = a.library(l)
	}
	writeJSON(w, out)
}

// groupingOptions are the libraries a view may be grouped by, as Jellyfin's SpecialViewOptionDto.
func (a *API) groupingOptions(w http.ResponseWriter, r *http.Request) {
	libs, _, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type option struct {
		Name string `json:"Name"`
		ID   string `json:"Id"`
	}
	out := make([]option, len(libs))
	for n, l := range libs {
		out[n] = option{Name: l.Name, ID: guid(l.ID)}
	}
	writeJSON(w, out)
}

// virtualFolders are the libraries and what each holds, which Infuse reads to know which is which;
// where their files are is the server's own business.
func (a *API) virtualFolders(w http.ResponseWriter, r *http.Request) {
	libs, _, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type folder struct {
		Name           string   `json:"Name"`
		Locations      []string `json:"Locations"`
		CollectionType string   `json:"CollectionType"`
		ItemID         string   `json:"ItemId"`
	}
	out := make([]folder, len(libs))
	for n, l := range libs {
		out[n] = folder{Name: l.Name, Locations: []string{}, CollectionType: a.library(l).CollectionType, ItemID: guid(l.ID)}
	}
	writeJSON(w, out)
}

// sorts are photon's sorts for Jellyfin's ItemSortBy, which an app sends most important first.
var sorts = map[string]domain.WallSort{
	"sortname": domain.SortTitle, "name": domain.SortTitle, "datecreated": domain.SortAdded,
	"premieredate": domain.SortReleased, "productionyear": domain.SortReleased, "communityrating": domain.SortRating,
	"criticrating": domain.SortRating, "runtime": domain.SortRuntime, "dateplayed": domain.SortPlayed,
}

// marks are photon's marks for Jellyfin's ItemFilter.
var marks = map[string]domain.Mark{
	"isplayed": domain.MarkWatched, "isunplayed": domain.MarkUnwatched, "isresumable": domain.MarkInProgress,
	"isfavorite": domain.MarkFavourite,
}

// wallPage is a library page as an app asks for one: its sort, order and filters, those photon
// has no like of left out rather than refused, as Jellyfin narrows by what it knows.
func wallPage(r *http.Request, profile uuid.UUID, l listed) store.WallPage {
	p := store.WallPage{Profile: profile, Sort: domain.SortTitle, Order: domain.Ascending, Offset: l.start, Limit: l.limit, RatingSite: domain.SiteTMDB}
	for _, s := range values(r, "sortBy") {
		if sort, ok := sorts[strings.ToLower(s)]; ok {
			p.Sort = sort
			if s == "CriticRating" {
				p.RatingSite = domain.SiteRottenTomatoes
			}
			break
		}
	}
	if has(values(r, "sortOrder"), "Descending") {
		p.Order = domain.Descending
	}
	for _, f := range values(r, "filters") {
		if m, ok := marks[strings.ToLower(f)]; ok {
			p.Filter.Marks = append(p.Filter.Marks, m)
		}
	}
	for _, flag := range [][2]string{{"isPlayed", "isplayed"}, {"isFavorite", "isfavorite"}} {
		if strings.EqualFold(query(r, flag[0]), "true") {
			p.Filter.Marks = append(p.Filter.Marks, marks[flag[1]])
		}
	}
	p.Filter.Genres = values(r, "genres")
	for _, y := range values(r, "years") {
		if n, err := strconv.Atoi(y); err == nil {
			p.Filter.Years = append(p.Filter.Years, n)
		}
	}
	if s := query(r, "nameStartsWith"); s != "" {
		p.Filter.StartsWith = strings.ToUpper(s[:1])
	}
	return p
}

// libraryKinds are the kinds of title each kind of library holds, as Jellyfin's apps ask for them.
var libraryKinds = map[domain.LibraryKind]string{domain.LibraryMovies: "Movie", domain.LibraryShows: "Series"}

// items answers Jellyfin's /Items: a library's films or shows, a show's seasons or episodes, a
// season's episodes, or what matches a search; every library at once where an app names none.
func (a *API) items(w http.ResponseWriter, r *http.Request) {
	profile, l := sessionOf(r).Profile.ID, listedOf(w, r)
	types := values(r, "includeItemTypes")
	libs, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	parent, _ := uuid.Parse(query(r, "parentId"))
	if text := query(r, "searchTerm"); text != "" {
		a.search(w, r, text, parent, types, l)
		return
	}
	switch lib, ok := seen[parent]; {
	case ok:
		a.wall([]*store.SeenLibrary{lib}, w, r, types, l)
	case parent == uuid.UUID{} && len(types) == 0 && !strings.EqualFold(query(r, "recursive"), "true"):
		a.views(w, r)
	case parent == uuid.UUID{}:
		a.wall(libs, w, r, types, l)
	default:
		a.children(w, r, profile, parent, types, l)
	}
}

// wall answers the titles of libraries, those of several sorted together, of the kinds asked for.
func (a *API) wall(libs []*store.SeenLibrary, w http.ResponseWriter, r *http.Request, types []string, l listed) {
	var ids []uuid.UUID
	for _, lib := range libs {
		if len(types) == 0 || has(types, libraryKinds[lib.Kind]) {
			ids = append(ids, lib.ID)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
		return
	}
	cards, total, err := a.svc.Catalogue.Wall(r.Context(), ids, wallPage(r, sessionOf(r).Profile.ID, l))
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards, int(total), l.start, l)
}

// searchKinds are photon's kinds for Jellyfin's, of those a search finds.
var searchKinds = map[string]domain.ItemKind{
	"movie": domain.ItemMovie, "series": domain.ItemShow, "episode": domain.ItemEpisode, "boxset": domain.ItemCollection,
}

func (a *API) search(w http.ResponseWriter, r *http.Request, text string, library uuid.UUID, types []string, l listed) {
	q := store.SearchQuery{Profile: sessionOf(r).Profile.ID, Text: text, Library: library, Offset: l.start, Limit: l.limit}
	for _, t := range types {
		if k, ok := searchKinds[strings.ToLower(t)]; ok {
			q.Kinds = append(q.Kinds, k)
		}
	}
	if len(q.Kinds) == 0 && len(types) > 0 {
		writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
		return
	}
	cards, total, err := a.svc.Catalogue.Search(r.Context(), q)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards, int(total), l.start, l)
}

// children answers what is in a show or a season: a show's seasons, or its episodes where an app
// asks for them; a season's episodes. Anything else holds nothing an app can ask for here.
func (a *API) children(w http.ResponseWriter, r *http.Request, profile, parent uuid.UUID, types []string, l listed) {
	seasons, err := a.svc.Catalogue.Seasons(r.Context(), profile, parent)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if len(seasons) > 0 && !has(types, "Episode") {
		a.writeSeasons(w, r, parent, seasons, l)
		return
	}
	cards, err := a.svc.Catalogue.Episodes(r.Context(), profile, parent)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards[min(l.start, len(cards)):min(l.start+l.limit, len(cards))], len(cards), l.start, l)
}

func (a *API) writeSeasons(w http.ResponseWriter, r *http.Request, show uuid.UUID, seasons []store.SeasonCard, l listed) {
	page := seasons[min(l.start, len(seasons)):min(l.start+l.limit, len(seasons))]
	out := queryResult{Items: make([]item, len(page)), TotalRecordCount: len(seasons), StartIndex: l.start}
	name := ""
	if len(page) > 0 {
		if p, err := a.svc.Catalogue.Title(r.Context(), sessionOf(r).Profile.ID, show); err == nil {
			name = p.Title
		}
	}
	for n, s := range page {
		out.Items[n] = a.fromSeason(store.TitleRef{ID: show, Title: name}, s)
		out.Items[n].Etag = etag(out.Items[n])
	}
	writeJSON(w, out)
}

// item answers one item: a library, or a title with all photon knows of it.
func (a *API) item(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil {
		refuse(w, http.StatusNotFound)
		return
	}
	_, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if lib, ok := seen[id]; ok {
		writeJSON(w, a.library(lib))
		return
	}
	p, err := a.svc.Catalogue.Title(r.Context(), sessionOf(r).Profile.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		a.announcedItem(w, r, id)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	it := a.fromTitle(p, words.Negotiate(w, r))
	it.Etag = etag(it)
	writeJSON(w, it)
}

func (a *API) seasons(w http.ResponseWriter, r *http.Request) {
	show, err := uuid.Parse(r.PathValue("seriesId"))
	if err != nil {
		refuse(w, http.StatusNotFound)
		return
	}
	seasons, err := a.svc.Catalogue.Seasons(r.Context(), sessionOf(r).Profile.ID, show)
	if errors.Is(err, store.ErrNotFound) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeSeasons(w, r, show, seasons, listedOf(w, r))
}

// episodes answers a show's episodes, every season's unless an app names one: Infuse asks for all
// of them at once and groups them by season itself.
func (a *API) episodes(w http.ResponseWriter, r *http.Request) {
	of, err := uuid.Parse(r.PathValue("seriesId"))
	if err != nil {
		refuse(w, http.StatusNotFound)
		return
	}
	if season, err := uuid.Parse(query(r, "seasonId")); err == nil {
		of = season
	}
	cards, err := a.svc.Catalogue.Episodes(r.Context(), sessionOf(r).Profile.ID, of)
	if errors.Is(err, store.ErrNotFound) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if n, err := strconv.Atoi(query(r, "season")); err == nil {
		cards = slices.DeleteFunc(cards, func(c store.Card) bool { return c.SeasonNumber == nil || *c.SeasonNumber != n })
	}
	l := listedOf(w, r)
	a.writeList(w, r, cards[min(l.start, len(cards)):min(l.start+l.limit, len(cards))], len(cards), l.start, l)
}

// rowLimit is the most of a row an app is given at once, and latestLimit how many of what was added last
// where it asks for no limit, as Jellyfin gives.
const (
	rowLimit    = 50
	latestLimit = 20
)

// row answers a page of one of the profile's rows, such as Continue Watching, as a list.
func (a *API) row(kind domain.HomeRow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := listedOf(w, r)
		cards, total, err := a.svc.Catalogue.RowPage(r.Context(), sessionOf(r).Profile.ID, kind, l.start, min(l.limit, rowLimit))
		if err != nil {
			a.internal(w, r, err)
			return
		}
		a.writeList(w, r, cards, int(total), l.start, l)
	}
}

// nextUp answers the episodes to play next: of every show, or of the one an app names.
func (a *API) nextUp(w http.ResponseWriter, r *http.Request) {
	show, err := uuid.Parse(query(r, "seriesId"))
	if err != nil {
		a.row(domain.RowNextUp)(w, r)
		return
	}
	var cards []store.Card
	switch c, err := a.svc.Catalogue.Next(r.Context(), sessionOf(r).Profile.ID, show); {
	case err == nil:
		cards = []store.Card{c}
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrNoNext):
	default:
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards, len(cards), 0, listedOf(w, r))
}

// latestRows are the rows of what was added last to each kind of library.
var latestRows = map[domain.LibraryKind]domain.HomeRow{domain.LibraryMovies: domain.RowRecentFilms, domain.LibraryShows: domain.RowRecentShows}

// latest answers what was added last to a library, or to every library: a bare list, as Jellyfin's
// is, of films, and of shows by their newest episode.
func (a *API) latest(w http.ResponseWriter, r *http.Request) {
	libs, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if parent, err := uuid.Parse(query(r, "parentId")); err == nil {
		libs = nil
		if lib, ok := seen[parent]; ok {
			libs = []*store.SeenLibrary{lib}
		}
	}
	l := listedOf(w, r)
	limit := latestLimit
	if query(r, "limit") != "" {
		limit = min(l.limit, rowLimit)
	}
	var cards []store.Card
	for _, lib := range libs {
		got, err := a.svc.Catalogue.LibraryRow(r.Context(), sessionOf(r).Profile.ID, latestRows[lib.Kind], lib.ID, limit)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		cards = append(cards, got...)
	}
	items, err := a.list(r.Context(), cards, l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, items)
}

// none answers a list photon has nothing for yet, as an empty one: Infuse asks for every title's
// trailers and features, and takes a missing route for a failure.
func none(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`[]`))
}

// image answers a picture by its tag, sized to fit what an app asks for. A tag is a picture's id,
// which changes whenever the picture does, so it is kept for good. It is public, as Jellyfin's
// images are, so a page can show one without a token; ids are random.
func (a *API) image(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(query(r, "tag"))
	if err != nil {
		refuse(w, http.StatusNotFound)
		return
	}
	pic, err := a.svc.Catalogue.Picture(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	bound := func(names ...string) int {
		n := 0
		for _, name := range names {
			if v, err := strconv.Atoi(query(r, name)); err == nil && v > 0 {
				n = max(n, v)
			}
		}
		return n
	}
	o, name, err := a.svc.Pictures.Open(r.Context(), id, pic, bound("maxWidth", "fillWidth", "width"), bound("maxHeight", "fillHeight", "height"))
	if errors.Is(err, os.ErrNotExist) {
		refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	err = blob.Serve(w, r, o, name, http.Header{
		"Cache-Control": {"public, max-age=31536000, immutable"},
		// A provider's logo may be SVG, which a browser opening it directly would run script in.
		"Content-Security-Policy": {"default-src 'none'; style-src 'unsafe-inline'; sandbox"},
		"X-Content-Type-Options":  {"nosniff"},
	})
	if err != nil {
		a.internal(w, r, err)
	}
}
