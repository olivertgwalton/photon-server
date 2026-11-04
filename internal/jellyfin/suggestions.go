package jellyfin

import (
	"net/http"
	"strings"
)

// suggestions answers Jellyfin's /Items/Suggestions, which Discrete opens its home with, as
// Jellyfin does: the items of the kinds an app names in type, across every library the profile
// sees, at random.
func (a *API) suggestions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	types := values(r, "type")
	// The query's names are any case, so each is set alone in the spelling /Items reads.
	for k := range q {
		for _, name := range []string{"type", "includeItemTypes", "recursive", "sortBy", "parentId"} {
			if strings.EqualFold(k, name) {
				q.Del(k)
			}
		}
	}
	q.Set("includeItemTypes", strings.Join(types, ","))
	q.Set("recursive", "true")
	q.Set("sortBy", "Random")
	asked := r.Clone(r.Context())
	asked.URL.RawQuery = q.Encode()
	a.items(w, asked)
}
