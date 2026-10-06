package httpapi

import (
	"net/http"
	"strconv"
	"uuid"
)

func (a *API) pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return uuid.UUID{}, false
	}
	return id, true
}

// pathNumber reads a count or id from 0 that a path names; any other is a path to nothing.
func (a *API) pathNumber(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil || n < 0 {
		writeProblem(w, a.logger, codeNotFound, "")
		return 0, false
	}
	return n, true
}
