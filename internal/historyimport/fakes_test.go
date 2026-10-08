package historyimport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type object = map[string]any

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}

func queryInt(t *testing.T, q url.Values, key string) int {
	t.Helper()
	n, err := strconv.Atoi(q.Get(key))
	if err != nil {
		t.Errorf("%s: %v", key, err)
	}
	return n
}

// page answers the part of items a Plex or Jellyfin request asks for.
func page(items []object, start, size int) []object {
	start = min(start, len(items))
	return items[start:min(start+size, len(items))]
}

// fakePlex is a Plex server with one film section and one show section, answering only token.
type fakePlex struct {
	token                  string
	films, shows, episodes []object
}

func (f *fakePlex) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != f.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		var items []object
		switch r.URL.Path + "?" + q.Get("type") {
		case "/library/sections?":
			writeJSON(t, w, object{"MediaContainer": object{"size": 2, "Directory": []object{
				{"key": "1", "type": "movie", "title": "Films"},
				{"key": "2", "type": "show", "title": "TV"},
			}}})
			return
		case "/library/sections/1/all?1":
			items = f.films
		case "/library/sections/2/all?2":
			items = f.shows
		case "/library/sections/2/all?4":
			items = f.episodes
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if q.Get("includeGuids") != "1" {
			t.Errorf("%s asked without includeGuids", r.URL)
		}
		start, size := queryInt(t, q, "X-Plex-Container-Start"), queryInt(t, q, "X-Plex-Container-Size")
		writeJSON(t, w, object{"MediaContainer": object{"totalSize": len(items), "Metadata": page(items, start, size)}})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// fakeJellyfin is a Jellyfin or Emby server with one user, who signs in by name and password.
type fakeJellyfin struct {
	emby               bool
	user, password     string
	movies, episodes   []object
	series             []object
	signedIn, signOuts int
}

const fakeJellyfinToken = "jf-token"

func (f *fakeJellyfin) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := "Authorization"
		items := "/Items"
		if f.emby {
			header, items = "X-Emby-Authorization", "/Users/user-1/Items"
		}
		auth := r.Header.Get(header)
		if !strings.HasPrefix(auth, "MediaBrowser ") || !strings.Contains(auth, `Client="`) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/Users/AuthenticateByName" && r.Method == http.MethodPost {
			var body struct{ Username, Pw string }
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Username != f.user || body.Pw != f.password {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f.signedIn++
			writeJSON(t, w, object{"AccessToken": fakeJellyfinToken, "User": object{"Id": "user-1", "Name": f.user}})
			return
		}
		if !strings.Contains(auth, `Token="`+fakeJellyfinToken+`"`) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/Sessions/Logout" && r.Method == http.MethodPost:
			f.signOuts++
			w.WriteHeader(http.StatusNoContent)
			return
		case r.URL.Path != items, !f.emby && q.Get("userId") != "user-1":
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var found []object
		switch q.Get("IncludeItemTypes") {
		case "Series":
			found = f.series
		case "Movie,Episode":
			for _, it := range append(append([]object{}, f.movies...), f.episodes...) {
				data, _ := it["UserData"].(object)
				played, _ := data["Played"].(bool)
				ticks, _ := data["PlaybackPositionTicks"].(int64)
				if q.Get("Filters") == "IsPlayed" && played || q.Get("Filters") == "IsResumable" && ticks > 0 {
					found = append(found, it)
				}
			}
		}
		start, limit := queryInt(t, q, "StartIndex"), queryInt(t, q, "Limit")
		writeJSON(t, w, object{"Items": page(found, start, limit), "TotalRecordCount": len(found)})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
