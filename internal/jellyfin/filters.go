package jellyfin

import (
	"cmp"
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
	return slices.DeleteFunc(slices.Clone(libs), func(l *store.SeenLibrary) bool {
		return len(types) > 0 && !has(types, libraryKinds[l.Kind])
	})
}

// facets are the values the titles of libraries have, each once: names in order, years the
// newest first.
func (a *API) facets(ctx context.Context, profile uuid.UUID, libs []*store.SeenLibrary) (store.Facets, error) {
	var all store.Facets
	for _, l := range libs {
		f, err := a.svc.Catalogue.Facets(ctx, l.ID, profile)
		if err != nil {
			return store.Facets{}, err
		}
		all.Genres = append(all.Genres, f.Genres...)
		all.Studios = append(all.Studios, f.Studios...)
		all.Certificates = append(all.Certificates, f.Certificates...)
		all.Years = append(all.Years, f.Years...)
	}
	for _, names := range []*[]string{&all.Genres, &all.Studios, &all.Certificates} {
		slices.Sort(*names)
		*names = slices.Compact(*names)
	}
	slices.SortFunc(all.Years, func(a, b int) int { return cmp.Compare(b, a) })
	all.Years = slices.Compact(all.Years)
	return all, nil
}

// facetsAsked are the facets of the libraries a request names.
func (a *API) facetsAsked(r *http.Request) (store.Facets, error) {
	libs, seen, err := a.seenLibraries(r)
	if err != nil {
		return store.Facets{}, err
	}
	return a.facets(r.Context(), auth.SessionOf(r.Context()).Profile.ID, asked(r, libs, seen))
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
		it := item{
			Name: name, SortName: name, ServerID: a.id, ID: guid(nameID(kind, name)), Type: kind, IsFolder: true,
			LocationType: "FileSystem", MediaType: "Unknown",
		}
		it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
		it.Etag = etag(it)
		out.Items[n] = it
	}
	a.writeJSON(w, out)
}

// filters answers Jellyfin's QueryFiltersLegacy: what the libraries asked for can be narrowed to.
// photon has no tags.
func (a *API) filters(w http.ResponseWriter, r *http.Request) {
	f, err := a.facetsAsked(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	ratings := []string{}
	for _, c := range f.Certificates {
		ratings = append(ratings, domain.Bare(c))
	}
	slices.Sort(ratings)
	a.writeJSON(w, struct {
		Genres          []string `json:"Genres"`
		Tags            []string `json:"Tags"`
		OfficialRatings []string `json:"OfficialRatings"`
		Years           []int    `json:"Years"`
	}{nonNil(f.Genres), []string{}, slices.Compact(ratings), nonNil(f.Years)})
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
	}{genres, []struct{}{}})
}

// narrow adds to a filter the names of the genres and studios an app names by id, and the
// certificates it names bare, of those the libraries have.
func (a *API) narrow(r *http.Request, libs []*store.SeenLibrary, f *store.WallFilter) error {
	genres, studios, ratings := values(r, "genreIds"), values(r, "studioIds"), piped(r, "officialRatings")
	if len(genres)+len(studios)+len(ratings) == 0 {
		return nil
	}
	have, err := a.facets(r.Context(), auth.SessionOf(r.Context()).Profile.ID, libs)
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

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
