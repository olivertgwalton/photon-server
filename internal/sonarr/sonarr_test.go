package sonarr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakeSonarr finds Breaking Bad, with its specials and five seasons, and The Wire, which it has
// already with its second season unmonitored; it keeps the show it was sent.
type fakeSonarr struct {
	added *series
	key   string
}

func (f *fakeSonarr) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.key = r.Header.Get("X-Api-Key")
		answer := func(v any) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Error(err)
			}
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/series/lookup":
			switch r.URL.Query().Get("term") {
			case "tvdb:81189":
				seasons := []season{}
				for n := range 6 {
					seasons = append(seasons, season{SeasonNumber: n})
				}
				answer([]found{{Title: "Breaking Bad", TitleSlug: "breaking-bad", Year: 2008, Seasons: seasons}})
			case "tvdb:79126":
				answer([]found{{ID: 7, Title: "The Wire", Seasons: []season{{1, true}, {2, false}}}})
			default:
				answer([]found{})
			}
		case "GET /api/v3/qualityprofile":
			answer([]map[string]any{{"id": 6, "name": "HD-1080p"}})
		case "GET /api/v3/rootfolder":
			answer([]map[string]any{{"path": "/tv/"}})
		case "POST /api/v3/series":
			f.added = &series{}
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

func client(srv *httptest.Server) *Client {
	return New(func(context.Context) (map[string]string, error) {
		return map[string]string{"address": srv.URL, "api_key": "k3y"}, nil
	})
}

func monitored(s *series) []int {
	var out []int
	for _, x := range s.Seasons {
		if x.Monitored {
			out = append(out, x.SeasonNumber)
		}
	}
	return out
}

// Sonarr is sent a show by its TheTVDB id with the seasons asked for monitored, every one but its
// specials where none are named, and searched for. A show it has already is taken on where it
// monitors what was asked for, and refused where it does not; it says where it knows no such show,
// or none of the seasons asked for.
func TestSonarrIsAskedForAShowALibraryLacks(t *testing.T) {
	f := &fakeSonarr{}
	srv := f.serve(t)
	breakingBad := domain.Wanted{ID: uuid.NewV7(), Kind: domain.ItemShow, Title: "Breaking Bad", IDs: map[domain.Provider]string{domain.ProviderTVDB: "81189"}}
	if err := client(srv).Request(t.Context(), breakingBad); err != nil {
		t.Fatal(err)
	}
	if f.key != "k3y" || f.added == nil || f.added.TVDBID != 81189 || f.added.TitleSlug != "breaking-bad" || f.added.QualityProfileID != 6 ||
		f.added.RootFolderPath != "/tv/" || !f.added.Monitored || !f.added.AddOptions.SearchForMissingEpisodes || f.added.SeriesType != "standard" {
		t.Errorf("added %+v with key %q", f.added, f.key)
	}
	if got := monitored(f.added); len(got) != 5 || got[0] != 1 {
		t.Errorf("monitored seasons %v, want 1 to 5 and no specials", got)
	}
	twoAndSpecials := breakingBad
	twoAndSpecials.Seasons = []int{0, 2}
	if err := client(srv).Request(t.Context(), twoAndSpecials); err != nil {
		t.Fatal(err)
	}
	if got := monitored(f.added); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("monitored seasons %v, want the specials and season 2", got)
	}

	f.added = nil
	wire := domain.Wanted{Kind: domain.ItemShow, Title: "The Wire", Seasons: []int{1}, IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}
	if err := client(srv).Request(t.Context(), wire); err != nil || f.added != nil {
		t.Errorf("a season Sonarr monitors already: %v, added %+v; want it taken on", err, f.added)
	}
	for name, tc := range map[string]struct {
		wanted domain.Wanted
		want   string
	}{
		"unmonitored":  {domain.Wanted{Kind: domain.ItemShow, IDs: wire.IDs}, "does not monitor season 2"},
		"no season":    {domain.Wanted{Kind: domain.ItemShow, Seasons: []int{9}, IDs: breakingBad.IDs}, "none of seasons [9]"},
		"no such show": {domain.Wanted{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTVDB: "1"}}, "no show with TheTVDB id 1"},
		"no TVDB id":   {domain.Wanted{Kind: domain.ItemShow, IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396"}}, "TheTVDB id"},
		"not a show":   {domain.Wanted{Kind: domain.ItemMovie, IDs: breakingBad.IDs}, "not a movie"},
	} {
		err := client(srv).Request(t.Context(), tc.wanted)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want it refused saying %q", name, err, tc.want)
		}
	}
}
