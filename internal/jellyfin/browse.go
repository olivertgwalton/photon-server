package jellyfin

import (
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

type catalogue interface {
	LibrariesSeen(ctx context.Context, profile uuid.UUID) ([]*store.SeenLibrary, error)
	Wall(ctx context.Context, libs []uuid.UUID, p store.WallPage) ([]store.Card, int64, error)
	Search(ctx context.Context, q store.SearchQuery) ([]store.Card, int64, error)
	Cards(ctx context.Context, profile uuid.UUID, titles []uuid.UUID) ([]store.Card, error)
	Collections(ctx context.Context, lib, profile uuid.UUID, offset, limit int) ([]store.Card, int64, error)
	HasCollections(ctx context.Context, profile uuid.UUID) (bool, error)
	Members(ctx context.Context, profile, collection uuid.UUID) ([]store.Card, error)
	Named(ctx context.Context, profile, id uuid.UUID) (store.Named, error)
	SearchPeople(ctx context.Context, text string, offset, limit int) ([]store.PersonRef, int64, error)
	Person(ctx context.Context, id uuid.UUID) (store.PersonPage, error)
	Facets(ctx context.Context, lib, profile uuid.UUID) (store.Facets, error)
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
	return split(query(r, name), ",")
}

// piped are a list parameter's names, which Jellyfin splits on "|" alone, as a name may hold a
// comma.
func piped(r *http.Request, name string) []string {
	return split(query(r, name), "|")
}

func split(s, sep string) []string {
	var out []string
	for v := range strings.SplitSeq(s, sep) {
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
				out[n].sources(v, l.words)
				// Jellyfin writes the first copy's tracks beside it only when asked: they double
				// what is written of the item.
				if l.fields["mediastreams"] {
					out[n].MediaStreams = out[n].MediaSources[0].MediaStreams
				}
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
	a.writeJSON(w, queryResult{Items: items, TotalRecordCount: total, StartIndex: start})
}

// seenLibraries are the libraries the profile sees, by id.
func (a *API) seenLibraries(r *http.Request) ([]*store.SeenLibrary, map[uuid.UUID]*store.SeenLibrary, error) {
	libs, err := a.svc.Catalogue.LibrariesSeen(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	byID := map[uuid.UUID]*store.SeenLibrary{}
	for _, l := range libs {
		byID[l.ID] = l
	}
	return libs, byID, err
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
	p.Filter.Genres, p.Filter.Studios = piped(r, "genres"), piped(r, "studios")
	// An id that is none names no one, so narrows to nothing rather than letting everything through.
	for _, v := range values(r, "personIds") {
		id, _ := parseID(v)
		p.Filter.People = append(p.Filter.People, id)
	}
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

// items answers Jellyfin's /Items: the items an app names, a library's films or shows, a show's
// seasons or episodes, a season's episodes, every collection, or what matches a search; every
// library at once where an app names none, and the profile's playlists where it asks for those
// alone or opens their view.
func (a *API) items(w http.ResponseWriter, r *http.Request) {
	profile, l := auth.SessionOf(r.Context()).Profile.ID, listedOf(w, r)
	types := values(r, "includeItemTypes")
	libs, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if ids := values(r, "ids"); len(ids) > 0 {
		a.byID(w, r, ids, seen, l)
		return
	}
	parent, ok := optionalID(query(r, "parentId"))
	if !ok {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	if text := query(r, "searchTerm"); text != "" {
		a.search(w, r, text, parent, types, l)
		return
	}
	switch lib, ok := seen[parent]; {
	case ok:
		a.wall([]*store.SeenLibrary{lib}, w, r, types, l)
	case parent == a.viewID(collectionsView):
		a.collections(libs, w, r, l)
	case parent == a.viewID(playlistsView), parent == uuid.UUID{} && len(types) == 1 && strings.EqualFold(types[0], "Playlist"):
		a.playlists(w, r, l)
	case parent == uuid.UUID{} && len(types) == 0 && !strings.EqualFold(query(r, "recursive"), "true"):
		a.views(w, r)
	case parent == uuid.UUID{}:
		a.wall(libs, w, r, types, l)
	default:
		a.children(w, r, profile, parent, types, l)
	}
}

// byID answers the items an app names, in its order, each once: the libraries and titles the
// profile may see, and nothing for the rest.
func (a *API) byID(w http.ResponseWriter, r *http.Request, names []string, seen map[uuid.UUID]*store.SeenLibrary, l listed) {
	ids := make([]uuid.UUID, len(names))
	for n, name := range names {
		id, ok := parseID(name)
		if !ok {
			a.refuse(w, http.StatusBadRequest)
			return
		}
		ids[n] = id
	}
	cards, err := a.svc.Catalogue.Cards(r.Context(), auth.SessionOf(r.Context()).Profile.ID, ids)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	titles, err := a.list(r.Context(), cards, l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	found := map[uuid.UUID]item{}
	for n, c := range cards {
		found[c.ID] = titles[n]
	}
	for _, id := range ids {
		if lib, ok := seen[id]; ok {
			found[id] = a.library(lib)
		}
	}
	out := []item{}
	for _, id := range ids {
		if it, ok := found[id]; ok {
			out = append(out, it)
			delete(found, id)
		}
	}
	a.writeJSON(w, queryResult{Items: out[min(l.start, len(out)):min(l.start+l.limit, len(out))], TotalRecordCount: len(out), StartIndex: l.start})
}

// wall answers the titles of libraries, those of several sorted together, of the kinds asked for;
// or their collections, where an app asks for box sets alone.
func (a *API) wall(libs []*store.SeenLibrary, w http.ResponseWriter, r *http.Request, types []string, l listed) {
	if len(types) == 1 && strings.EqualFold(types[0], "BoxSet") {
		a.collections(libs, w, r, l)
		return
	}
	var ids []uuid.UUID
	var shown []*store.SeenLibrary
	for _, lib := range libs {
		if len(types) == 0 || has(types, libraryKinds[lib.Kind]) {
			ids, shown = append(ids, lib.ID), append(shown, lib)
		}
	}
	if len(ids) == 0 {
		a.writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
		return
	}
	p := wallPage(r, auth.SessionOf(r.Context()).Profile.ID, l)
	if err := a.narrow(r, shown, &p.Filter); err != nil {
		a.internal(w, r, err)
		return
	}
	cards, total, err := a.svc.Catalogue.Wall(r.Context(), ids, p)
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
	q := store.SearchQuery{Profile: auth.SessionOf(r.Context()).Profile.ID, Text: text, Library: library, Offset: l.start, Limit: l.limit}
	for _, t := range types {
		if k, ok := searchKinds[strings.ToLower(t)]; ok {
			q.Kinds = append(q.Kinds, k)
		}
	}
	if len(q.Kinds) == 0 && len(types) > 0 {
		a.writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
		return
	}
	cards, total, err := a.svc.Catalogue.Search(r.Context(), q)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeList(w, r, cards, int(total), l.start, l)
}

// children answers what is in a collection, a show or a season: a collection's titles; a show's
// seasons, or its episodes where an app asks for them; a season's episodes. Anything else holds
// nothing an app can ask for here.
func (a *API) children(w http.ResponseWriter, r *http.Request, profile, parent uuid.UUID, types []string, l listed) {
	none := queryResult{Items: []item{}, StartIndex: l.start}
	named, err := a.svc.Catalogue.Named(r.Context(), profile, parent)
	if errors.Is(err, store.ErrNotFound) {
		a.writeJSON(w, none)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	switch named.Kind {
	case store.NamedTitle:
		a.titleChildren(w, r, profile, parent, named.Title, types, l)
	case store.NamedAnnounced, store.NamedPerson, store.NamedPlaylist:
		a.writeJSON(w, none)
	}
}

func (a *API) titleChildren(w http.ResponseWriter, r *http.Request, profile, parent uuid.UUID, kind domain.ItemKind, types []string, l listed) {
	switch kind {
	case domain.ItemShow:
		seasons, err := a.svc.Catalogue.Seasons(r.Context(), profile, parent)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		if len(seasons) > 0 && !has(types, "Episode") {
			a.writeSeasons(w, r, parent, seasons, l)
			return
		}
		fallthrough
	case domain.ItemSeason:
		cards, err := a.svc.Catalogue.Episodes(r.Context(), profile, parent)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		a.writeList(w, r, cards[min(l.start, len(cards)):min(l.start+l.limit, len(cards))], len(cards), l.start, l)
	case domain.ItemCollection:
		members, err := a.svc.Catalogue.Members(r.Context(), profile, parent)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		a.writeList(w, r, members[min(l.start, len(members)):min(l.start+l.limit, len(members))], len(members), l.start, l)
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		a.writeJSON(w, queryResult{Items: []item{}, StartIndex: l.start})
	}
}

func (a *API) writeSeasons(w http.ResponseWriter, r *http.Request, show uuid.UUID, seasons []store.SeasonCard, l listed) {
	page := seasons[min(l.start, len(seasons)):min(l.start+l.limit, len(seasons))]
	out := queryResult{Items: make([]item, len(page)), TotalRecordCount: len(seasons), StartIndex: l.start}
	name := ""
	if len(page) > 0 {
		if p, err := a.svc.Catalogue.Title(r.Context(), auth.SessionOf(r.Context()).Profile.ID, show); err == nil {
			name = p.Title
		}
	}
	for n, s := range page {
		out.Items[n] = a.fromSeason(store.TitleRef{ID: show, Title: name}, s)
		out.Items[n].Etag = etag(out.Items[n])
	}
	a.writeJSON(w, out)
}

// item answers one item: a library or the view of collections or playlists, a title with all
// photon knows of it, an episode announced, someone credited on a title, or one of the profile's
// playlists.
func (a *API) item(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	_, seen, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if lib, ok := seen[id]; ok {
		a.writeJSON(w, a.library(lib))
		return
	}
	if id == a.viewID(collectionsView) {
		a.writeJSON(w, a.collectionsFolder())
		return
	}
	if id == a.viewID(playlistsView) {
		a.writeJSON(w, a.playlistsFolder())
		return
	}
	named, err := a.svc.Catalogue.Named(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	switch named.Kind {
	case store.NamedTitle:
		a.titleItem(w, r, id)
	case store.NamedAnnounced:
		a.announcedItem(w, r, id)
	case store.NamedPerson:
		a.person(w, r, id)
	case store.NamedPlaylist:
		a.playlistItem(w, r, id)
	}
}

func (a *API) titleItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	p, err := a.svc.Catalogue.Title(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	it := a.fromTitle(p, words.Negotiate(w, r))
	if p.Kind == domain.ItemCollection {
		members, err := a.svc.Catalogue.Members(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
		if err != nil {
			a.internal(w, r, err)
			return
		}
		n := len(members)
		it.ChildCount = &n
	}
	it.Etag = etag(it)
	a.writeJSON(w, it)
}

func (a *API) seasons(w http.ResponseWriter, r *http.Request) {
	show, err := uuid.Parse(r.PathValue("seriesId"))
	if err != nil {
		a.refuse(w, http.StatusNotFound)
		return
	}
	seasons, err := a.svc.Catalogue.Seasons(r.Context(), auth.SessionOf(r.Context()).Profile.ID, show)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
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
		a.refuse(w, http.StatusNotFound)
		return
	}
	if season, err := uuid.Parse(query(r, "seasonId")); err == nil {
		of = season
	}
	cards, err := a.svc.Catalogue.Episodes(r.Context(), auth.SessionOf(r.Context()).Profile.ID, of)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
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

// none answers a list photon has nothing for yet, as an empty one: Infuse asks for every title's
// trailers and features, and takes a missing route for a failure.
func (a *API) none(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	a.write(w, []byte(`[]`))
}
