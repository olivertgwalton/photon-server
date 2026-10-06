package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
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

// queryNumber reads a whole number from least to most, or def where the query leaves name out.
func (a *API) queryNumber(w http.ResponseWriter, r *http.Request, name string, def, least, most int) (int, bool) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, true
	}
	if n, err := strconv.Atoi(s); err == nil && n >= least && n <= most {
		return n, true
	}
	detail := fmt.Sprintf("%s is a number from %d", name, least)
	if most < math.MaxInt {
		detail += fmt.Sprintf(" to %d", most)
	}
	writeProblem(w, a.logger, codeInvalidParameter, detail)
	return 0, false
}

// queryEnum reads one of all, or def where the query leaves name out.
func queryEnum[T ~string](a *API, w http.ResponseWriter, r *http.Request, name string, def T, all []T) (T, bool) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, true
	}
	v, err := domain.Parse(name, s, all)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, err.Error())
		return "", false
	}
	return v, true
}
