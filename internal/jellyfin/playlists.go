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
