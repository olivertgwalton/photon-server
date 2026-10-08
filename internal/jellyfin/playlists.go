package jellyfin

import (
	"context"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type playlists interface {
	Playlists(ctx context.Context, profile uuid.UUID) ([]store.PlaylistSummary, error)
	PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]store.PlaylistEntry, int64, error)
}

// fromPlaylist is one of the profile's playlists, as Jellyfin's apps list one: a folder of video.
func (a *API) fromPlaylist(p store.PlaylistSummary) item {
	it := item{
		Name: p.Name, SortName: p.Name, ServerID: a.id, ID: guid(p.ID), Type: "Playlist", IsFolder: true,
		ChildCount: &p.Entries, RunTimeTicks: p.DurationMS * ticksPerMS, LocationType: "FileSystem", MediaType: "Video",
	}
	it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
	it.Etag = etag(it)
	return it
}

// playlists answers the profile's playlists, by name. No profile sees another's.
func (a *API) playlists(w http.ResponseWriter, r *http.Request, l listed) {
	all, err := a.svc.Playlists.Playlists(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	page := all[min(l.start, len(all)):min(l.start+l.limit, len(all))]
	out := queryResult{Items: make([]item, len(page)), TotalRecordCount: len(all), StartIndex: l.start}
	for n, p := range page {
		out.Items[n] = a.fromPlaylist(p)
	}
	a.writeJSON(w, out)
}

// playlistItem answers the profile's playlist of an id.
func (a *API) playlistItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	all, err := a.svc.Playlists.Playlists(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	for _, p := range all {
		if p.ID == id {
			a.writeJSON(w, a.fromPlaylist(p))
			return
		}
	}
	a.refuse(w, http.StatusNotFound)
}

func (a *API) playlistID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := parseID(r.PathValue("playlistId"))
	if !ok {
		a.refuse(w, http.StatusNotFound)
	}
	return id, ok
}

// playlistItems answers a page of the profile's playlist, in its order, each entry with its own id,
// which an app moves or removes it by.
func (a *API) playlistItems(w http.ResponseWriter, r *http.Request) {
	id, ok := a.playlistID(w, r)
	if !ok {
		return
	}
	l := listedOf(w, r)
	entries, total, err := a.svc.Playlists.PlaylistEntries(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, l.start, l.limit)
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	cards := make([]store.Card, len(entries))
	for n, e := range entries {
		cards[n] = e.Card
	}
	items, err := a.list(r.Context(), cards, l)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	for n, e := range entries {
		items[n].PlaylistItemID = guid(e.ID)
	}
	a.writeJSON(w, queryResult{Items: items, TotalRecordCount: int(total), StartIndex: l.start})
}
