package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// idWildcards are rt's path wildcards that are ids: every one its path parameters do not name, as
// the description has them.
func idWildcards(rt route) []string {
	var ids []string
	for _, m := range wildcard.FindAllStringSubmatch(rt.pattern, -1) {
		if !slices.ContainsFunc(rt.path, func(p param) bool { return p.name == m[1] }) {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// requireIDs answers 404 Not Found for a path whose id wildcards are not all ids, so that a
// handler reads each with pathUUID.
func (a *API) requireIDs(ids []string, next http.Handler) http.Handler {
	if len(ids) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, name := range ids {
			if _, err := uuid.Parse(r.PathValue(name)); err != nil {
				writeProblem(w, a.logger, codeNotFound, "")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// pathUUID is the id wildcard name, which requireIDs has made sure is one.
func pathUUID(r *http.Request, name string) uuid.UUID { return uuid.MustParse(r.PathValue(name)) }

// queryID reads an id, or the zero id where the query leaves name out.
func queryID(r *http.Request, name string) (uuid.UUID, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return uuid.UUID{}, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, refuse(codeInvalidParameter, name+" is not an id")
	}
	return id, nil
}

// pathNumber reads a count or id from 0 that a path names; any other is a path to nothing.
func pathNumber(r *http.Request, name string) (int, error) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil || n < 0 {
		return 0, refuse(codeNotFound, "")
	}
	return n, nil
}

// queryNumber reads a whole number from least to most, or def where the query leaves name out.
func queryNumber(r *http.Request, name string, def, least, most int) (int, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= least && n <= most {
		return n, nil
	}
	detail := fmt.Sprintf("%s is a number from %d", name, least)
	if most < math.MaxInt {
		detail += fmt.Sprintf(" to %d", most)
	}
	return 0, refuse(codeInvalidParameter, detail)
}

// queryEnum reads one of all, or def where the query leaves name out.
func queryEnum[T ~string](r *http.Request, name string, def T, all []T) (T, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	v, err := domain.Parse(name, s, all)
	if err != nil {
		return "", refuse(codeInvalidParameter, err.Error())
	}
	return v, nil
}

// paging reads a page's offset and limit, as walls page, and whether it asks how many there are.
func paging(r *http.Request, defaultLimit int) (page store.Page, err error) {
	if page.Offset, err = queryNumber(r, "offset", 0, 0, store.MaxOffset); err != nil {
		return page, err
	}
	if page.Limit, err = queryNumber(r, "limit", defaultLimit, 1, maxWallLimit); err != nil {
		return page, err
	}
	page.Count, err = queryEnum(r, "total", domain.CountNone, domain.Countings())
	return page, err
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
