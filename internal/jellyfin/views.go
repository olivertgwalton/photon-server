package jellyfin

import (
	"crypto/sha256"
	"uuid"
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
