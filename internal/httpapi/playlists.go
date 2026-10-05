package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

type playlists interface {
	Playlists(ctx context.Context, profile uuid.UUID) ([]store.PlaylistSummary, error)
	AddPlaylist(ctx context.Context, profile uuid.UUID, name string, items []uuid.UUID) (uuid.UUID, error)
	PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]store.PlaylistEntry, int64, error)
	AddToPlaylist(ctx context.Context, profile, playlist uuid.UUID, items []uuid.UUID) error
	RemoveFromPlaylist(ctx context.Context, profile, playlist, entry uuid.UUID) error
	MovePlaylistEntry(ctx context.Context, profile, playlist, entry uuid.UUID, position int) error
	RenamePlaylist(ctx context.Context, profile, playlist uuid.UUID, name string) error
	RemovePlaylist(ctx context.Context, profile, playlist uuid.UUID) error
}

type playlistJSON struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Entries    int       `json:"entries"`
	DurationMS int64     `json:"duration_ms"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type addPlaylistJSON struct {
	Name    string      `json:"name"`
	ItemIDs []uuid.UUID `json:"item_ids,omitzero"`
}

type nameJSON struct {
	Name string `json:"name"`
}

type entryJSON struct {
	EntryID uuid.UUID `json:"entry_id"`
	cardJSON
}

// moveJSON is a position counted from zero.
type moveJSON struct {
	Position int `json:"position"`
}

// playlistsOf answers the profile's playlists, by name.
func (a *API) playlistsOf(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.Playlists.Playlists(r.Context(), sessionOf(r).Profile.ID)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]playlistJSON, len(all))
	for i, p := range all {
		out[i] = playlistJSON(p)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[playlistJSON]{Items: out})
}

// addPlaylist makes one of the profile's playlists, of the titles given if any.
func (a *API) addPlaylist(w http.ResponseWriter, r *http.Request) {
	var req addPlaylistJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is set")
		return
	}
	id, err := a.svc.Playlists.AddPlaylist(r.Context(), sessionOf(r).Profile.ID, req.Name, req.ItemIDs)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, createdJSON{ID: id})
}

// playlistEntries answers a page of a playlist, in its order: each entry's own id and the title it
// plays.
func (a *API) playlistEntries(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	offset, limit := 0, maxWallLimit
	var err error
	if s := q.Get("offset"); s != "" {
		if offset, err = strconv.Atoi(s); err != nil || offset < 0 {
			writeProblem(w, a.logger, codeInvalidParameter, "offset is a number from 0")
			return
		}
	}
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > maxWallLimit {
			writeProblem(w, a.logger, codeInvalidParameter, "limit is a number from 1 to "+strconv.Itoa(maxWallLimit))
			return
		}
	}
	entries, total, err := a.svc.Playlists.PlaylistEntries(r.Context(), sessionOf(r).Profile.ID, id, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	out := make([]entryJSON, len(entries))
	for i, e := range entries {
		out[i] = entryJSON{EntryID: e.ID, cardJSON: cardsJSON([]store.Card{e.Card})[0]}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[entryJSON]{out, offset, total})
}

// addToPlaylist puts titles at the end of a playlist: a show or season as its episodes, a
// collection as its titles.
func (a *API) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req itemIDsJSON
	if !a.decode(w, r, &req) {
		return
	}
	if a.answered(w, r, a.svc.Playlists.AddToPlaylist(r.Context(), sessionOf(r).Profile.ID, id, req.ItemIDs)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setPlaylist renames a playlist.
func (a *API) setPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	var req nameJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is set")
		return
	}
	if a.answered(w, r, a.svc.Playlists.RenamePlaylist(r.Context(), sessionOf(r).Profile.ID, id, req.Name)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Playlists.RemovePlaylist(r.Context(), sessionOf(r).Profile.ID, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// moveEntry moves one entry of a playlist to a position, counted from zero.
func (a *API) moveEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	entry, err := uuid.Parse(r.PathValue("entry"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	var req moveJSON
	if !a.decode(w, r, &req) {
		return
	}
	if a.answered(w, r, a.svc.Playlists.MovePlaylistEntry(r.Context(), sessionOf(r).Profile.ID, id, entry, req.Position)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removeEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r)
	if !ok {
		return
	}
	entry, err := uuid.Parse(r.PathValue("entry"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if a.answered(w, r, a.svc.Playlists.RemoveFromPlaylist(r.Context(), sessionOf(r).Profile.ID, id, entry)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
