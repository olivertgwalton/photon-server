package httpapi

import (
	"net/http"
)

// forgottenJSON is how many copies an ask to forget took out of a library.
type forgottenJSON struct {
	Copies int64 `json:"copies"`
}

// forgetMissingCopies takes a library's copies on no disk out of it, and the titles left with
// nothing with them, as Plex's Empty Trash does for a library whose trash its scans do not empty.
func (a *API) forgetMissingCopies(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	forgotten, changed, err := a.svc.Libraries.ForgetMissingCopies(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	a.svc.Events.Changed(r.Context(), id, changed)
	writeJSON(w, a.logger, "application/json", http.StatusOK, forgottenJSON{Copies: forgotten})
}

func (a *API) missingRoutes() []route {
	return []route{
		{
			pattern: "DELETE /api/v1/admin/libraries/{id}/missing", access: admin,
			summary: "Forget a library's copies on no disk, and the titles left with nothing",
			status:  http.StatusOK, reply: forgottenJSON{}, handle: a.forgetMissingCopies,
		},
	}
}
