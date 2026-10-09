package httpapi

import (
	"net/http"
	"slices"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

var (
	artworkKindParam = param{"kind", domain.ArtworkKind(""), "The kind of picture."}
	artworkPathParam = param{"artwork", domain.ArtworkKind(""), "The kind of picture."}
)

// artworkCandidateJSON is a picture a provider has, served at /api/v1/artwork/{id} like a title's
// own, so a browser shows it sized and from this server.
type artworkCandidateJSON struct {
	ID       uuid.UUID          `json:"id"`
	Source   domain.FieldSource `json:"source"`
	Language string             `json:"language,omitzero"`
	Width    int                `json:"width,omitzero"`
	Height   int                `json:"height,omitzero"`
	Chosen   bool               `json:"chosen"`
}

// artworkCandidates lists the pictures of a kind each provider has for a title, as Jellyfin's Edit
// Images and Plex's poster chooser do.
func (a *API) artworkCandidates(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	kind := domain.ArtworkKind(r.URL.Query().Get("kind"))
	if !slices.Contains(domain.ArtworkKinds(), kind) {
		writeProblem(w, a.logger, codeInvalidParameter, "kind is a kind of picture")
		return
	}
	offered, err := a.svc.Editing.ArtworkCandidates(r.Context(), id, kind)
	if a.answered(w, r, err) {
		return
	}
	out := make([]artworkCandidateJSON, len(offered))
	for i, c := range offered {
		out[i] = artworkCandidateJSON{ID: c.ID, Source: c.Source, Language: c.Language, Width: c.Width, Height: c.Height, Chosen: c.Chosen}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[artworkCandidateJSON]{Items: out})
}

// pathArtworkKind answers the kind of picture a path names, or writes why not.
func (a *API) pathArtworkKind(w http.ResponseWriter, r *http.Request) (domain.ArtworkKind, bool) {
	kind := domain.ArtworkKind(r.PathValue("artwork"))
	if !slices.Contains(domain.ArtworkKinds(), kind) {
		writeProblem(w, a.logger, codeNotFound, "")
		return "", false
	}
	return kind, true
}

type chooseArtworkJSON struct {
	ID uuid.UUID `json:"id"`
}

// chooseArtwork makes a provider's picture a title's own of its kind, over every source and
// through every refresh.
func (a *API) chooseArtwork(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	kind, ok := a.pathArtworkKind(w, r)
	if !ok {
		return
	}
	var req chooseArtworkJSON
	if !a.decode(w, r, &req) {
		return
	}
	if a.answered(w, r, a.svc.Editing.ChooseArtwork(r.Context(), id, kind, req.ID)) {
		return
	}
	a.titleUpdated(r, id)
	w.WriteHeader(http.StatusNoContent)
}

// forgetArtwork gives a title's picture of a kind back to its sources.
func (a *API) forgetArtwork(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	kind, ok := a.pathArtworkKind(w, r)
	if !ok {
		return
	}
	if a.answered(w, r, a.svc.Editing.ForgetArtworkChoice(r.Context(), id, kind)) {
		return
	}
	a.titleUpdated(r, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) artworkEditRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/titles/{id}/artwork/candidates", access: admin,
			summary: "List the pictures of a kind each provider has for a title, to choose from",
			query:   []param{artworkKindParam},
			status:  http.StatusOK, reply: listJSON[artworkCandidateJSON]{}, handle: a.artworkCandidates,
		},
		{
			pattern: "PUT /api/v1/admin/titles/{id}/artwork/{artwork}", access: admin,
			summary: "Choose a title's picture of a kind from its candidates, over every source",
			path:    []param{artworkPathParam}, body: chooseArtworkJSON{}, status: http.StatusNoContent, handle: a.chooseArtwork,
		},
		{
			pattern: "DELETE /api/v1/admin/titles/{id}/artwork/{artwork}", access: admin,
			summary: "Give a title's picture of a kind back to its sources",
			path:    []param{artworkPathParam}, status: http.StatusNoContent, handle: a.forgetArtwork,
		},
	}
}
