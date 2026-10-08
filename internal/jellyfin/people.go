package jellyfin

import (
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// personItem is someone as Jellyfin's Person item, as a list shows them.
func (a *API) personItem(id uuid.UUID, name string, photo uuid.UUID, hashes store.Blurhashes) item {
	it := item{Name: name, SortName: name, ServerID: a.id, ID: guid(id), Type: "Person", LocationType: "FileSystem", MediaType: "Unknown"}
	it.pictures(photo, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, hashes)
	it.PrimaryImageAspectRatio = posterAspect
	return it
}

// person answers someone by their id, as an app opens a cast member: born as Jellyfin's
// PremiereDate, died as its EndDate, and their birthplace as its ProductionLocations.
func (a *API) person(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	p, err := a.svc.Catalogue.Person(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	it := a.personItem(p.ID, p.Name, p.Photo, p.Blurhashes)
	it.Overview, it.PremiereDate, it.EndDate = p.Biography, optionalTime(time.Time(p.Born)), optionalTime(time.Time(p.Died))
	if p.Birthplace != "" {
		it.ProductionLocations = []string{p.Birthplace}
	}
	it.ProviderIDs = providerIDs(p.IDs)
	it.Etag = etag(it)
	a.writeJSON(w, it)
}

// persons answers the people whose names have words starting with those searched for, as
// photon's own search finds them. No searchTerm finds no one: every app looks people up by name,
// and a list of everyone credited on anything is of no use to one.
func (a *API) persons(w http.ResponseWriter, r *http.Request) {
	l := listedOf(w, r)
	found, total, err := a.svc.Catalogue.SearchPeople(r.Context(), query(r, "searchTerm"), l.start, l.limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := queryResult{Items: make([]item, len(found)), TotalRecordCount: int(total), StartIndex: l.start}
	for n, p := range found {
		out.Items[n] = a.personItem(p.ID, p.Name, p.Photo, p.Blurhashes)
		out.Items[n].Etag = etag(out.Items[n])
	}
	a.writeJSON(w, out)
}
