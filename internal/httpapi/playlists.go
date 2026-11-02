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
	PlaylistEntries(ctx context.Context, profile, playlist uuid.UUID, page store.Page) ([]store.PlaylistEntry, store.Extent, error)
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
	Library    uuid.UUID      `json:"library_id,omitzero"`
	Rule       *smartRuleJSON `json:"rule,omitzero"`
	Entries    int            `json:"entries"`
	DurationMS int64          `json:"duration_ms"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// addPlaylistJSON is a playlist of the titles given, if any; given a rule and the library it
// reads, a smart playlist of what the rule finds there, as Plex's smart playlists.
type addPlaylistJSON struct {
	Name      string         `json:"name"`
	ItemIDs   []uuid.UUID    `json:"item_ids,omitzero"`
	LibraryID uuid.UUID      `json:"library_id,omitzero"`
	Rule      *smartRuleJSON `json:"rule,omitzero"`
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
func (a *API) playlistsOf(r *http.Request, _ none) (listJSON[playlistJSON], error) {
	all, err := a.svc.Playlists.Playlists(r.Context(), auth.SessionOf(r.Context()).Profile.ID)
	out := make([]playlistJSON, len(all))
	for i, p := range all {
		out[i] = playlistJSON{
			ID: p.ID, Name: p.Name, Library: p.Library, Rule: ruleOf(p.Rule), Entries: p.Entries, DurationMS: p.DurationMS, UpdatedAt: p.UpdatedAt,
		}
	}
	return listJSON[playlistJSON]{Items: out}, err
}

// addPlaylist makes one of the profile's playlists, of the titles given if any, or a smart one of
// what its rule finds.
func (a *API) addPlaylist(r *http.Request, req addPlaylistJSON) (createdJSON, error) {
	switch {
	case req.Name == "":
		return createdJSON{}, refuse(codeInvalidBody, "name is set")
	case (req.Rule != nil) != (req.LibraryID != uuid.UUID{}):
		return createdJSON{}, refuse(codeInvalidBody, "library_id names the library a rule reads, and a playlist made by hand neither")
	case req.Rule != nil && len(req.ItemIDs) > 0:
		return createdJSON{}, refuse(codeInvalidBody, "a smart playlist's entries are what its rule finds: item_ids is left out")
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	var id uuid.UUID
	var err error
	if req.Rule != nil {
		if err := req.Rule.rule().CheckPlayable(); err != nil {
			return createdJSON{}, refuse(codeInvalidBody, err.Error())
		}
		id, err = a.svc.Playlists.AddSmartPlaylist(r.Context(), profile, req.LibraryID, req.Name, req.Rule.rule())
		err = notFoundAs(err, "library_id names a library the profile sees")
	} else {
		id, err = a.svc.Playlists.AddPlaylist(r.Context(), profile, req.Name, req.ItemIDs)
	}
	return createdJSON{ID: id}, a.playlistChangedBy(r, id, err)
}

// setPlaylistRule replaces a smart playlist's rule, and so its entries with what it finds.
func (a *API) setPlaylistRule(r *http.Request, req smartRuleJSON) (none, error) {
	if err := req.rule().CheckPlayable(); err != nil {
		return none{}, refuse(codeInvalidBody, err.Error())
	}
	id := pathUUID(r, "id")
	return none{}, a.playlistChangedBy(r, id, a.svc.Playlists.SetPlaylistRule(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, req.rule()))
}

// playlistEntries answers a page of a playlist, in its order: each entry's own id and the title it
// plays.
func (a *API) playlistEntries(r *http.Request, _ none) (pageJSON[entryJSON], error) {
	page, err := paging(r, maxWallLimit)
	if err != nil {
		return pageJSON[entryJSON]{}, err
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	entries, extent, err := a.svc.Playlists.PlaylistEntries(r.Context(), profile, pathUUID(r, "id"), page)
	if err != nil {
		return pageJSON[entryJSON]{}, err
	}
	titles := make([]uuid.UUID, len(entries))
	for i, e := range entries {
		titles[i] = e.Title
	}
	cards, err := a.svc.Catalogue.Cards(r.Context(), profile, titles)
	if err != nil {
		return pageJSON[entryJSON]{}, err
	}
	drawn := map[uuid.UUID]store.Card{}
	for _, c := range cards {
		drawn[c.ID] = c
	}
	// A title a playlist holds twice is drawn once and answered for both its entries.
	var out []entryJSON
	for _, e := range entries {
		if c, ok := drawn[e.Title]; ok {
			out = append(out, entryJSON{EntryID: e.ID, cardJSON: cardOf(c)})
		}
	}
	return pageAt(out, page.Offset, extent), nil
}

// addToPlaylist puts titles at the end of a playlist: a show or season as its episodes, a
// collection as its titles.
func (a *API) addToPlaylist(r *http.Request, req itemIDsJSON) (none, error) {
	id := pathUUID(r, "id")
	return none{}, a.playlistChangedBy(r, id, a.svc.Playlists.AddToPlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, req.ItemIDs))
}

// setPlaylist renames a playlist.
func (a *API) setPlaylist(r *http.Request, req nameJSON) (none, error) {
	if req.Name == "" {
		return none{}, refuse(codeInvalidBody, "name is set")
	}
	id := pathUUID(r, "id")
	return none{}, a.playlistChangedBy(r, id, a.svc.Playlists.RenamePlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, req.Name))
}

func (a *API) removePlaylist(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	return none{}, a.playlistChangedBy(r, id, a.svc.Playlists.RemovePlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id))
}

// moveEntry moves one entry of a playlist to a position, counted from zero.
func (a *API) moveEntry(r *http.Request, req moveJSON) (none, error) {
	id := pathUUID(r, "id")
	return none{}, a.playlistChangedBy(r, id, a.svc.Playlists.MovePlaylistEntry(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, pathUUID(r, "entry"), req.Position))
}

func (a *API) removeEntry(r *http.Request, _ none) (none, error) {
	id := pathUUID(r, "id")
	return none{}, a.playlistChangedBy(r, id, a.svc.Playlists.RemoveFromPlaylist(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, pathUUID(r, "entry")))
}

// playlistChangedBy tells the profile its playlist changed, where err says the change was made.
func (a *API) playlistChangedBy(r *http.Request, id uuid.UUID, err error) error {
	if err == nil {
		a.playlistChanged(r, id)
	}
	return err
}

func (a *API) playlistsRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/playlists", access: signedIn, summary: "List the profile's playlists, by name",
			status: http.StatusOK,
		}, a.playlistsOf),
		a.handles(route{
			pattern: "POST /api/v1/playlists", access: signedIn, summary: "Make a playlist, by hand or by a rule",
			status: http.StatusCreated,
		}, a.addPlaylist),
		a.handles(route{
			pattern: "PUT /api/v1/playlists/{id}/rule", access: signedIn,
			summary: "Replace a smart playlist's rule, and so its entries with what it finds",
			status:  http.StatusNoContent,
		}, a.setPlaylistRule),
		a.handles(route{
			pattern: "PATCH /api/v1/playlists/{id}", access: signedIn, summary: "Rename a playlist",
			status: http.StatusNoContent,
		}, a.setPlaylist),
		a.handles(route{
			pattern: "DELETE /api/v1/playlists/{id}", access: signedIn, summary: "Remove a playlist",
			status: http.StatusNoContent,
		}, a.removePlaylist),
		a.handles(route{
			pattern: "GET /api/v1/playlists/{id}/entries", access: signedIn, summary: "Page a playlist, in its order",
			query: pageParams, status: http.StatusOK,
		}, a.playlistEntries),
		a.handles(route{
			pattern: "POST /api/v1/playlists/{id}/entries", access: signedIn,
			summary: "Put titles at the end of a playlist: a show or season as its episodes",
			status:  http.StatusNoContent,
		}, a.addToPlaylist),
		a.handles(route{
			pattern: "PUT /api/v1/playlists/{id}/entries/{entry}/position", access: signedIn,
			summary: "Move a playlist's entry", status: http.StatusNoContent,
		}, a.moveEntry),
		a.handles(route{
			pattern: "DELETE /api/v1/playlists/{id}/entries/{entry}", access: signedIn,
			summary: "Take an entry out of a playlist", status: http.StatusNoContent,
		}, a.removeEntry),
	}
}
