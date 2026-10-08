package jellyfin

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type playlists interface {
	Playlists(ctx context.Context, profile uuid.UUID) ([]store.PlaylistSummary, error)
	PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]store.PlaylistEntry, int64, error)
	AddPlaylist(ctx context.Context, profile uuid.UUID, name string, items []uuid.UUID) (uuid.UUID, error)
	AddToPlaylist(ctx context.Context, profile, playlist uuid.UUID, items []uuid.UUID) error
	RemoveFromPlaylist(ctx context.Context, profile, playlist, entry uuid.UUID) error
	MovePlaylistEntry(ctx context.Context, profile, playlist, entry uuid.UUID, position int) error
	RenamePlaylist(ctx context.Context, profile, playlist uuid.UUID, name string) error
}

// playlistsView is the name of the view of the profile's playlists, which Jellyfin's apps find
// them by.
const playlistsView = "Playlists"

func (a *API) playlistsFolder() item {
	return a.view(playlistsView, "ManualPlaylistsFolder", "playlists")
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

// sharing is whom a playlist is shared with, as Jellyfin's dtos to make or change one say. Photon's
// playlists are their profile's alone, so one asked to be shared with anyone else is refused.
type sharing struct {
	Users []struct {
		UserID string `json:"UserId"`
	} `json:"Users"`
	IsPublic bool `json:"IsPublic"`
}

func (s sharing) shared(profile uuid.UUID) bool {
	for _, u := range s.Users {
		if id, ok := parseID(u.UserID); !ok || id != profile {
			return true
		}
	}
	return s.IsPublic
}

// ids reads Guids, and says whether every one was one.
func ids(vs []string) ([]uuid.UUID, bool) {
	out := make([]uuid.UUID, len(vs))
	for n, v := range vs {
		id, ok := parseID(v)
		if !ok {
			return nil, false
		}
		out[n] = id
	}
	return out, true
}

// createPlaylist makes one of the profile's playlists, of the titles named if any: a show or season
// as its episodes, a collection as its titles. A playlist for another profile is refused as
// Jellyfin refuses one to a user who is not an administrator.
func (a *API) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string   `json:"Name"`
		IDs    []string `json:"Ids"`
		UserID string   `json:"UserId"`
		sharing
	}
	if !a.readJSON(w, r, &req) {
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	if owner, ok := parseID(req.UserID); req.UserID != "" && (!ok || owner != profile) {
		a.refuse(w, http.StatusForbidden)
		return
	}
	items, ok := ids(req.IDs)
	if !ok || req.Name == "" || req.shared(profile) {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	id, err := a.svc.Playlists.AddPlaylist(r.Context(), profile, req.Name, items)
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.playlistChanged(r, id)
	a.writeJSON(w, struct {
		ID string `json:"Id"`
	}{guid(id)})
}

// addToPlaylist puts titles at the end of the profile's playlist, as making one does.
func (a *API) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.playlistID(w, r)
	if !ok {
		return
	}
	items, ok := ids(values(r, "ids"))
	if !ok {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	a.changed(w, r, id, a.svc.Playlists.AddToPlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, items))
}

// removeFromPlaylist takes entries out of the profile's playlist, by their own ids.
func (a *API) removeFromPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.playlistID(w, r)
	if !ok {
		return
	}
	entries, ok := ids(values(r, "entryIds"))
	if !ok {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	var err error
	for _, entry := range entries {
		if err = a.svc.Playlists.RemoveFromPlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, entry); err != nil {
			break
		}
	}
	a.changed(w, r, id, err)
}

// moveInPlaylist moves an entry of the profile's playlist to an index counted from zero. The entry
// is named by its own id, though Jellyfin's route calls it the item's.
func (a *API) moveInPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.playlistID(w, r)
	if !ok {
		return
	}
	entry, ok := parseID(r.PathValue("itemId"))
	index, err := strconv.Atoi(r.PathValue("newIndex"))
	if !ok || err != nil {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	a.changed(w, r, id, a.svc.Playlists.MovePlaylistEntry(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, entry, index))
}

// playlist answers the profile's playlist as Jellyfin's PlaylistDto: shared with no one, and the
// titles it plays in order.
func (a *API) playlist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.playlistID(w, r)
	if !ok {
		return
	}
	entries, _, err := a.svc.Playlists.PlaylistEntries(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, 0, math.MaxInt32)
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := struct {
		OpenAccess bool       `json:"OpenAccess"`
		Shares     []struct{} `json:"Shares"`
		ItemIDs    []string   `json:"ItemIds"`
	}{Shares: []struct{}{}, ItemIDs: make([]string, len(entries))}
	for n, e := range entries {
		out.ItemIDs[n] = guid(e.Card.ID)
	}
	a.writeJSON(w, out)
}

// updatePlaylist renames the profile's playlist. Its titles are changed an entry at a time, so a
// whole new list of them is refused, as is sharing it.
func (a *API) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.playlistID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string   `json:"Name"`
		IDs  []string `json:"Ids"`
		sharing
	}
	if !a.readJSON(w, r, &req) {
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	if req.Name == "" || req.IDs != nil || req.shared(profile) {
		a.refuse(w, http.StatusBadRequest)
		return
	}
	a.changed(w, r, id, a.svc.Playlists.RenamePlaylist(r.Context(), profile, id, req.Name))
}

// changed answers a change to the profile's playlist: 404 where it has no such playlist, or the
// change names a title or entry that is not there; else 204, once its apps are told.
func (a *API) changed(w http.ResponseWriter, r *http.Request, id uuid.UUID, err error) {
	switch {
	case isNotFound(err):
		a.refuse(w, http.StatusNotFound)
	case err != nil:
		a.internal(w, r, err)
	default:
		a.playlistChanged(r, id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// playlistChanged tells the profile's apps, photon's own among them, that its playlist changed.
func (a *API) playlistChanged(r *http.Request, id uuid.UUID) {
	profile := auth.SessionOf(r.Context()).Profile.ID
	a.svc.Raise(r.Context(), domain.Event{
		Kind: domain.EventUserDataChanged, Profile: profile, Details: domain.UserDataDetails{PlaylistID: id},
	})
}
