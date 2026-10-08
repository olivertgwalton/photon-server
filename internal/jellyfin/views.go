package jellyfin

import (
	"crypto/sha256"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
)

// viewID is the id of a view of what is in no one library, as every collection is: made from the
// server's id and the view's name, so every node answers the same, and no title's id is one.
func (a *API) viewID(name string) uuid.UUID {
	var id uuid.UUID
	sum := sha256.Sum256([]byte(a.id + "\x00" + name))
	copy(id[:], sum[:])
	return id
}

// view is such a view as Jellyfin's apps open one: a folder of the kind Jellyfin's is, whose
// CollectionType says what it holds.
func (a *API) view(name, kind, collectionType string) item {
	id := guid(a.viewID(name))
	it := item{
		Name: name, SortName: name, ServerID: a.id, ID: id, Type: kind, IsFolder: true, CollectionType: collectionType,
		DisplayPreferencesID: id, LocationType: "FileSystem", MediaType: "Unknown",
	}
	it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
	it.Etag = etag(it)
	return it
}

// views are the libraries as Jellyfin's apps open them, in the profile's order, then the views of
// its collections and its playlists where it has any.
func (a *API) views(w http.ResponseWriter, r *http.Request) {
	libs, _, err := a.seenLibraries(r)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]item, len(libs))
	for n, l := range libs {
		out[n] = a.library(l)
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	collections, err := a.svc.Catalogue.HasCollections(r.Context(), profile)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if collections {
		out = append(out, a.collectionsFolder())
	}
	playlists, err := a.svc.Playlists.Playlists(r.Context(), profile)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if len(playlists) > 0 {
		out = append(out, a.playlistsFolder())
	}
	a.writeJSON(w, queryResult{Items: out, TotalRecordCount: len(out)})
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
	a.writeJSON(w, out)
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
	a.writeJSON(w, out)
}
