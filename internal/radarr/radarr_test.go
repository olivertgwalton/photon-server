package radarr

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakeRadarr has two quality profiles and two root folders, and Heat already; it keeps the film
// it was sent, and refuses what refuse says.
type fakeRadarr struct {
	added   *movie
	key     string
	refuse  string
	missing bool
}

func (f *fakeRadarr) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.key = r.Header.Get("X-Api-Key")
		answer := func(v any) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Error(err)
			}
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/movie":
			if r.URL.Query().Get("tmdbId") == "949" {
				answer([]map[string]any{{"title": "Heat", "tmdbId": 949}})
				return
			}
			answer([]any{})
		case "GET /api/v3/qualityprofile":
			answer([]qualityProfile{{ID: 4, Name: "HD-1080p"}, {ID: 5, Name: "Ultra-HD"}})
		case "GET /api/v3/rootfolder":
			if f.missing {
				answer([]any{})
				return
			}
			answer([]rootFolder{{Path: "/movies/"}, {Path: "/movies-4k/"}})
		case "POST /api/v3/movie":
			if f.refuse != "" {
				w.WriteHeader(http.StatusBadRequest)
				answer([]map[string]string{{"propertyName": "TmdbId", "errorMessage": f.refuse}})
				return
			}
			f.added = &movie{}
			if err := json.NewDecoder(r.Body).Decode(f.added); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
			answer(f.added)
		default:
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func client(srv *httptest.Server, set map[string]string) *Client {
	return New(func(context.Context) (map[string]string, error) {
		all := map[string]string{addressSetting: srv.URL, keySetting: "k3y"}
		maps.Copy(all, set)
		return all, nil
	})
}

// Radarr is sent a film by its TMDB id, monitored and searched for, with the quality profile and
// root folder an admin named or else its first; a film it has already is taken on as it is. It
// says why where it will not add one, or where what an admin named is not there; a show is no
// film of its.
func TestRadarrIsAskedForAFilmALibraryLacks(t *testing.T) {
	inception := domain.Wanted{ID: uuid.NewV7(), Kind: domain.ItemMovie, Title: "Inception", Year: 2010, IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205"}}
	f := &fakeRadarr{}
	srv := f.serve(t)
	if err := client(srv, nil).Request(t.Context(), inception); err != nil {
		t.Fatal(err)
	}
	if f.key != "k3y" || f.added == nil || f.added.TMDBID != 27205 || f.added.Title != "Inception" || f.added.Year != 2010 ||
		f.added.QualityProfileID != 4 || f.added.RootFolderPath != "/movies/" || !f.added.Monitored || !f.added.AddOptions.SearchForMovie {
		t.Errorf("added %+v with key %q", f.added, f.key)
	}

	f.added = nil
	if err := client(srv, map[string]string{profileSetting: "ultra-hd", folderSetting: "/movies-4k"}).Request(t.Context(), inception); err != nil {
		t.Fatal(err)
	}
	if f.added == nil || f.added.QualityProfileID != 5 || f.added.RootFolderPath != "/movies-4k/" {
		t.Errorf("added with the admin's profile and folder as %+v", f.added)
	}

	f.added = nil
	heat := inception
	heat.Title, heat.IDs = "Heat", map[domain.Provider]string{domain.ProviderTMDB: "949"}
	if err := client(srv, nil).Request(t.Context(), heat); err != nil || f.added != nil {
		t.Errorf("a film Radarr has: %v, added %+v; want it taken on, unsent", err, f.added)
	}

	for name, tc := range map[string]struct {
		set    map[string]string
		f      *fakeRadarr
		wanted domain.Wanted
		want   string
	}{
		"refused":    {f: &fakeRadarr{refuse: "This movie has already been added"}, wanted: inception, want: "This movie has already been added"},
		"no profile": {set: map[string]string{profileSetting: "SD"}, f: &fakeRadarr{}, wanted: inception, want: `no quality profile named "SD"`},
		"no folder":  {f: &fakeRadarr{missing: true}, wanted: inception, want: "no root folder"},
		"no TMDB id": {f: &fakeRadarr{}, wanted: domain.Wanted{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt1375666"}}, want: "TMDB id"},
		"not a film": {f: &fakeRadarr{}, wanted: domain.Wanted{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396"}}, want: "not a show"},
	} {
		err := client(tc.f.serve(t), tc.set).Request(t.Context(), tc.wanted)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want it refused saying %q", name, err, tc.want)
		}
	}
}
