package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type playlists interface {
	Playlists(ctx context.Context, profile uuid.UUID) ([]store.PlaylistSummary, error)
	AddPlaylist(ctx context.Context, profile uuid.UUID, name string, items []uuid.UUID) (uuid.UUID, error)
	AddSmartPlaylist(ctx context.Context, profile, lib uuid.UUID, name string, rule store.SmartRule) (uuid.UUID, error)
	SetPlaylistRule(ctx context.Context, profile, playlist uuid.UUID, rule store.SmartRule) error
	PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, offset, limit int) ([]store.PlaylistEntry, int64, error)
	AddToPlaylist(ctx context.Context, profile, playlist uuid.UUID, items []uuid.UUID) error
	RemoveFromPlaylist(ctx context.Context, profile, playlist, entry uuid.UUID) error
	MovePlaylistEntry(ctx context.Context, profile, playlist, entry uuid.UUID, position int) error
	RenamePlaylist(ctx context.Context, profile, playlist uuid.UUID, name string) error
	RemovePlaylist(ctx context.Context, profile, playlist uuid.UUID) error
}

type playlistJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Library and Rule are a smart playlist's: the library whose wall its rule reads, and the rule
	// its entries are what of. A playlist made by hand has neither.
	Library    uuid.UUID        `json:"library_id,omitzero"`
	Rule       *store.SmartRule `json:"rule,omitzero"`
	Entries    int              `json:"entries"`
	DurationMS int64            `json:"duration_ms"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// addPlaylistJSON is a playlist of the titles given, if any; given a rule and the library it
// reads, a smart playlist of what the rule finds there, as Plex's smart playlists.
type addPlaylistJSON struct {
	Name      string           `json:"name"`
	ItemIDs   []uuid.UUID      `json:"item_ids,omitzero"`
	LibraryID uuid.UUID        `json:"library_id,omitzero"`
	Rule      *store.SmartRule `json:"rule,omitzero"`
}

type nameJSON struct {
	Name string `json:"name"`
}

// entryJSON is one entry of a playlist. A smart playlist's entries are its rule's findings rather
// than rows of its own, so each is sent without an id of its own, and none is moved or taken out.
type entryJSON struct {
	EntryID uuid.UUID `json:"entry_id,omitzero"`
	cardJSON
}

// moveJSON is a position counted from zero.
type moveJSON struct {
	Position int `json:"position"`
}

// playlistsOf answers the profile's playlists, by name.
func (a *API) playlistsOf(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.Playlists.Playlists(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
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

// addPlaylist makes one of the profile's playlists, of the titles given if any, or a smart one of
// what its rule finds.
func (a *API) addPlaylist(w http.ResponseWriter, r *http.Request) {
	var req addPlaylistJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeProblem(w, a.logger, codeInvalidBody, "name is set")
		return
	}
	if (req.Rule != nil) != (req.LibraryID != uuid.UUID{}) {
		writeProblem(w, a.logger, codeInvalidBody, "library_id names the library a rule reads, and a playlist made by hand neither")
		return
	}
	if req.Rule != nil && len(req.ItemIDs) > 0 {
		writeProblem(w, a.logger, codeInvalidBody, "a smart playlist's entries are what its rule finds: item_ids is left out")
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	var id uuid.UUID
	var err error
	if req.Rule != nil {
		if !a.checkedRule(w, req.Rule.CheckPlayable()) {
			return
		}
		id, err = a.svc.Playlists.AddSmartPlaylist(r.Context(), profile, req.LibraryID, req.Name, *req.Rule)
		if a.answeredAs(w, r, err, "library_id names a library the profile sees") {
			return
		}
	} else {
		id, err = a.svc.Playlists.AddPlaylist(r.Context(), profile, req.Name, req.ItemIDs)
		if a.answered(w, r, err) {
			return
		}
	}
	a.playlistChanged(r, id)
	writeJSON(w, a.logger, "application/json", http.StatusCreated, createdJSON{ID: id})
}

// setPlaylistRule replaces a smart playlist's rule, and so its entries with what it finds.
func (a *API) setPlaylistRule(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req store.SmartRule
	if !a.decode(w, r, &req) || !a.checkedRule(w, req.CheckPlayable()) {
		return
	}
	if a.answered(w, r, a.svc.Playlists.SetPlaylistRule(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, req)) {
		return
	}
	a.playlistChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

// playlistEntries answers a page of a playlist, in its order: each entry's own id and the title it
// plays.
func (a *API) playlistEntries(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	offset, limit, ok := a.paging(w, r, maxWallLimit)
	if !ok {
		return
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	entries, total, err := a.svc.Playlists.PlaylistEntries(r.Context(), profile, id, offset, limit)
	if a.answered(w, r, err) {
		return
	}
	titles := make([]uuid.UUID, len(entries))
	for i, e := range entries {
		titles[i] = e.Title
	}
	cards, err := a.svc.Catalogue.Cards(r.Context(), profile, titles)
	if a.answered(w, r, err) {
		return
	}
	drawn := map[uuid.UUID]store.Card{}
	for _, c := range cards {
		drawn[c.ID] = c
	}
	// A title a playlist holds twice is drawn once and answered for both its entries.
	out := []entryJSON{}
	for _, e := range entries {
		if c, ok := drawn[e.Title]; ok {
			out = append(out, entryJSON{EntryID: e.ID, cardJSON: cardOf(c)})
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, pageJSON[entryJSON]{out, offset, total})
}

// addToPlaylist puts titles at the end of a playlist: a show or season as its episodes, a
// collection as its titles.
func (a *API) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req itemIDsJSON
	if !a.decode(w, r, &req) {
		return
	}
	if a.answered(w, r, a.svc.Playlists.AddToPlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, req.ItemIDs)) {
		return
	}
	a.playlistChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

// setPlaylist renames a playlist.
func (a *API) setPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
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
	if a.answered(w, r, a.svc.Playlists.RenamePlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, req.Name)) {
		return
	}
	a.playlistChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Playlists.RemovePlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)) {
		return
	}
	a.playlistChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

// moveEntry moves one entry of a playlist to a position, counted from zero.
func (a *API) moveEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	entry, ok := a.pathID(w, r, "entry")
	if !ok {
		return
	}
	var req moveJSON
	if !a.decode(w, r, &req) {
		return
	}
	if a.answered(w, r, a.svc.Playlists.MovePlaylistEntry(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, entry, req.Position)) {
		return
	}
	a.playlistChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removeEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	entry, ok := a.pathID(w, r, "entry")
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Playlists.RemoveFromPlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, entry)) {
		return
	}
	a.playlistChanged(r, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) playlistsRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/playlists", access: signedIn, summary: "List the profile's playlists, by name",
			status: http.StatusOK, reply: listJSON[playlistJSON]{}, handle: a.playlistsOf,
		},
		{
			pattern: "POST /api/v1/playlists", access: signedIn, summary: "Make a playlist, by hand or by a rule",
			body: addPlaylistJSON{}, status: http.StatusCreated, reply: createdJSON{}, handle: a.addPlaylist,
		},
		{
			pattern: "PUT /api/v1/playlists/{id}/rule", access: signedIn,
			summary: "Replace a smart playlist's rule, and so its entries with what it finds",
			body:    store.SmartRule{}, status: http.StatusNoContent, handle: a.setPlaylistRule,
		},
		{
			pattern: "PATCH /api/v1/playlists/{id}", access: signedIn, summary: "Rename a playlist",
			body: nameJSON{}, status: http.StatusNoContent, handle: a.setPlaylist,
		},
		{
			pattern: "DELETE /api/v1/playlists/{id}", access: signedIn, summary: "Remove a playlist",
			status: http.StatusNoContent, handle: a.removePlaylist,
		},
		{
			pattern: "GET /api/v1/playlists/{id}/entries", access: signedIn, summary: "Page a playlist, in its order",
			query: pageParams, status: http.StatusOK, reply: pageJSON[entryJSON]{}, handle: a.playlistEntries,
		},
		{
			pattern: "POST /api/v1/playlists/{id}/entries", access: signedIn,
			summary: "Put titles at the end of a playlist: a show or season as its episodes",
			body:    itemIDsJSON{}, status: http.StatusNoContent, handle: a.addToPlaylist,
		},
		{
			pattern: "PUT /api/v1/playlists/{id}/entries/{entry}/position", access: signedIn,
			summary: "Move a playlist's entry", body: moveJSON{}, status: http.StatusNoContent, handle: a.moveEntry,
		},
		{
			pattern: "DELETE /api/v1/playlists/{id}/entries/{entry}", access: signedIn,
			summary: "Take an entry out of a playlist", status: http.StatusNoContent, handle: a.removeEntry,
		},
	}
}
