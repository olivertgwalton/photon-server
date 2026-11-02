package httpapi

import (
	"net/http"
)

// forgottenJSON is how many versions an ask to forget took out of a library.
type forgottenJSON struct {
	Versions int64 `json:"versions"`
}

// forgetMissingVersions takes a library's versions on no disk out of it, and the titles left with
// nothing with them, as Plex's Empty Trash does for a library whose trash its scans do not empty.
func (a *API) forgetMissingVersions(r *http.Request, _ none) (forgottenJSON, error) {
	id := pathUUID(r, "id")
	forgotten, changed, err := a.svc.Libraries.ForgetMissingVersions(r.Context(), id)
	if err != nil {
		return forgottenJSON{}, err
	}
	a.svc.Events.Changed(r.Context(), id, changed)
	return forgottenJSON{Versions: forgotten}, nil
}

func (a *API) missingRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "DELETE /api/v1/admin/libraries/{id}/missing", access: admin,
			summary: "Forget a library's versions on no disk, and the titles left with nothing",
			status:  http.StatusOK,
		}, a.forgetMissingVersions),
	}
}
