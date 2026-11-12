package jellyfin

import (
	"context"
	"crypto/sha256"
	"net/http"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// nameID is the id of a genre or studio, which photon keeps as names alone: made from the name,
// so it is the same whenever an app asks, and an app filtering by it is narrowed to the name.
func nameID(kind, name string) uuid.UUID {
	var id uuid.UUID
	sum := sha256.Sum256([]byte(kind + "\x00" + name))
	copy(id[:], sum[:])
	return id
}

// asked are the libraries an app's parentId and includeItemTypes name: the one it names, or every
// one, of those holding the kinds asked for.
func asked(r *http.Request, libs []*store.SeenLibrary, seen map[uuid.UUID]*store.SeenLibrary) []*store.SeenLibrary {
	if parent, ok := optionalID(query(r, "parentId")); !ok {
		return nil
	} else if parent != (uuid.UUID{}) {
		libs = nil
		if lib, ok := seen[parent]; ok {
			libs = []*store.SeenLibrary{lib}
		}
	}
	types := values(r, "includeItemTypes")
	return slices.DeleteFunc(slices.Clone(libs), func(l *store.SeenLibrary) bool { return !holds(l, types) })
}

// wallKind is what an app's includeItemTypes asks a library for: its episodes, albums or tracks
// where it asks for those alone, as Jellyfin's Episodes, Albums and Songs tabs do, its films or its shows where
// it asks for one and not the other, else its titles.
func wallKind(types []string) domain.WallKind {
	films, shows := has(types, itemKinds[domain.ItemMovie]), has(types, itemKinds[domain.ItemShow])
	switch {
	case len(types) == 1 && has(types, itemKinds[domain.ItemEpisode]):
		return domain.WallEpisodes
	case len(types) == 1 && has(types, itemKinds[domain.ItemAlbum]):
		return domain.WallAlbums
	case len(types) == 1 && has(types, itemKinds[domain.ItemTrack]):
		return domain.WallTracks
	case has(types, itemKinds[domain.ItemBook]) || has(types, audioBook):
		return domain.WallBooks
	case films && !shows:
		return domain.WallFilms
	case shows && !films:
		return domain.WallShows
	}
	return domain.WallTitles
}

// holds is whether a library holds what an app's includeItemTypes asks for.
func holds(l *store.SeenLibrary, types []string) bool {
	return len(types) == 0 || slices.ContainsFunc(wallKind(types).Of(l.Kind), func(k domain.ItemKind) bool {
		return has(types, itemKinds[k])
	})
}

// facets are the values the titles of libraries, or their episodes, have.
func (a *API) facets(ctx context.Context, profile uuid.UUID, libs []*store.SeenLibrary, wall domain.WallKind) (store.Facets, error) {
	ids := make([]uuid.UUID, len(libs))
	for n, l := range libs {
		ids[n] = l.ID
	}
	return a.svc.Catalogue.Facets(ctx, ids, profile, wall)
}

// facetsAsked are the facets of the libraries a request names.
func (a *API) facetsAsked(r *http.Request) (store.Facets, error) {
	libs, seen, err := a.seenLibraries(r)
	if err != nil {
		return store.Facets{}, err
	}
	return a.facets(r.Context(), auth.SessionOf(r.Context()).Profile.ID, asked(r, libs, seen), wallKind(values(r, "includeItemTypes")))
}

func (a *API) genres(w http.ResponseWriter, r *http.Request) {
	a.named(w, r, "Genre", func(f store.Facets) []string { return f.Genres })
}

func (a *API) studios(w http.ResponseWriter, r *http.Request) {
	a.named(w, r, "Studio", func(f store.Facets) []string { return f.Studios })
}

// named answers the genres or studios of the libraries asked for as Jellyfin's Genre or Studio
// items, a page of them.
func (a *API) named(w http.ResponseWriter, r *http.Request, kind string, of func(store.Facets) []string) {
	f, err := a.facetsAsked(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	names, l := of(f), listedOf(w, r)
	page := names[min(l.start, len(names)):min(l.start+l.limit, len(names))]
	out := queryResult{Items: make([]item, len(page)), TotalRecordCount: len(names), StartIndex: l.start}
	for n, name := range page {
		out.Items[n] = a.nameItem(kind, name)
	}
	a.writeJSON(w, out)
}

// nameItem is a genre or studio as Jellyfin's Genre or Studio item.
func (a *API) nameItem(kind, name string) item {
	it := item{
		Name: name, SortName: name, ServerID: a.id, ID: guid(nameID(kind, name)), Type: kind, IsFolder: true,
		LocationType: "FileSystem", MediaType: "Unknown",
	}
	it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
	it.Etag = etag(it)
	return it
}

// genre answers the genre an app names by its id, as Finamp opens a title's genre: the one of the
// libraries' genres whose name makes it. Anything else is not found.
func (a *API) genre(w http.ResponseWriter, r *http.Request, libs []*store.SeenLibrary, id uuid.UUID) {
	ids := make([]uuid.UUID, len(libs))
	for n, l := range libs {
		ids[n] = l.ID
	}
	genres, err := a.svc.Catalogue.Genres(r.Context(), ids, auth.SessionOf(r.Context()).Profile.ID)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	for _, name := range genres {
		if nameID("Genre", name) == id {
			a.writeJSON(w, a.nameItem("Genre", name))
			return
		}
	}
	a.refuse(w, http.StatusNotFound)
}

// filters answers Jellyfin's QueryFiltersLegacy: what the libraries asked for can be narrowed to.
// photon has no tags.
func (a *API) filters(w http.ResponseWriter, r *http.Request) {
	f, err := a.facetsAsked(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	var ratings []string
	for _, c := range f.Certificates {
		ratings = append(ratings, domain.Bare(c))
	}
	slices.Sort(ratings)
	a.writeJSON(w, struct {
		Genres          []string `json:"Genres"`
		Tags            []string `json:"Tags"`
		OfficialRatings []string `json:"OfficialRatings"`
		Years           []int    `json:"Years"`
	}{f.Genres, nil, slices.Compact(ratings), f.Years})
}

// filters2 answers Jellyfin's QueryFilters: the genres, by name and id, and no tags.
func (a *API) filters2(w http.ResponseWriter, r *http.Request) {
	f, err := a.facetsAsked(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	type pair struct {
		Name string `json:"Name"`
		ID   string `json:"Id"`
	}
	genres := make([]pair, len(f.Genres))
	for n, g := range f.Genres {
		genres[n] = pair{g, guid(nameID("Genre", g))}
	}
	a.writeJSON(w, struct {
		Genres []pair     `json:"Genres"`
		Tags   []struct{} `json:"Tags"`
	}{Genres: genres})
}

// narrow adds to a filter the names of the genres and studios an app names by id, and the
// certificates it names bare, of those the libraries have.
func (a *API) narrow(r *http.Request, libs []*store.SeenLibrary, wall domain.WallKind, f *store.WallFilter) error {
	genres, studios, ratings := values(r, "genreIds"), values(r, "studioIds"), piped(r, "officialRatings")
	if len(genres)+len(studios)+len(ratings) == 0 {
		return nil
	}
	have, err := a.facets(r.Context(), auth.SessionOf(r.Context()).Profile.ID, libs, wall)
	if err != nil {
		return err
	}
	// Each starts from a name no title has, so what none of the libraries has narrows to nothing
	// rather than letting everything through.
	matching := func(names []string, match func(string) bool) []string {
		out := []string{""}
		for _, name := range names {
			if match(name) {
				out = append(out, name)
			}
		}
		return out
	}
	byID := func(kind string, ids []string) func(string) bool {
		want := map[uuid.UUID]bool{}
		for _, s := range ids {
			id, _ := parseID(s)
			want[id] = true
		}
		return func(name string) bool { return want[nameID(kind, name)] }
	}
	if len(genres) > 0 {
		f.Genres = append(f.Genres, matching(have.Genres, byID("Genre", genres))...)
	}
	if len(studios) > 0 {
		f.Studios = append(f.Studios, matching(have.Studios, byID("Studio", studios))...)
	}
	if len(ratings) > 0 {
		f.Certificates = append(f.Certificates, matching(have.Certificates, func(c string) bool { return has(ratings, domain.Bare(c)) })...)
	}
	return nil
}
