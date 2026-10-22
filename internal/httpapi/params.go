package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

func (a *API) pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return uuid.UUID{}, false
	}
	return id, true
}

// queryID reads an id, or the zero id where the query leaves name out.
func (a *API) queryID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return uuid.UUID{}, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, name+" is not an id")
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

// paging reads a page's offset and limit, as walls page, and whether it asks how many there are.
func (a *API) paging(w http.ResponseWriter, r *http.Request, defaultLimit int) (page store.Page, ok bool) {
	if page.Offset, ok = a.queryNumber(w, r, "offset", 0, 0, math.MaxInt); !ok {
		return page, false
	}
	if page.Limit, ok = a.queryNumber(w, r, "limit", defaultLimit, 1, maxWallLimit); !ok {
		return page, false
	}
	page.Count, ok = queryEnum(a, w, r, "total", domain.CountNone, domain.Countings())
	return page, ok
}

// list is the values of the list parameter name, repeated or comma-separated.
func list(q url.Values, name string) []string {
	var out []string
	for _, v := range q[name] {
		out = append(out, strings.Split(v, ",")...)
	}
	return out
}

// enum parses one of all for parseAll.
func enum[T ~string](what string, all []T) func(string) (T, error) {
	return func(s string) (T, error) { return domain.Parse(what, s, all) }
}

func parseAll[T any](values []string, parse func(string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(values))
	for _, v := range values {
		p, err := parse(v)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
