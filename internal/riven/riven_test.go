package riven

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// asked is what riven was sent.
type asked struct {
	key       string
	query     string
	input     input
	answer    string
	status    int
	unreached bool
}

func serve(t *testing.T, a *asked) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Errorf("asked at %s", r.URL.Path)
		}
		var q struct {
			Query     string           `json:"query"`
			Variables map[string]input `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		a.key, a.query, a.input = r.Header.Get("X-Api-Key"), q.Query, q.Variables["input"]
		w.WriteHeader(a.status)
		if _, err := w.Write([]byte(a.answer)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	address := srv.URL
	if a.unreached {
		srv.Close()
	}
	return New(func(context.Context) (map[string]string, error) {
		return map[string]string{addressSetting: address, keySetting: "k3y"}, nil
	})
}

// riven is asked for a film by its TMDB id and a show by its TheTVDB id and seasons, with the
// request's id and the key; one it has already is taken on. It says why where it will not take one,
// and a riven that is down is asked again later.
func TestRivenIsAskedForATitleALibraryLacks(t *testing.T) {
	id := uuid.NewV7()
	film := domain.Wanted{ID: id, Kind: domain.ItemMovie, Title: "Inception", IDs: map[domain.Provider]string{domain.ProviderTMDB: "27205", domain.ProviderIMDb: "tt1375666"}}
	show := domain.Wanted{ID: id, Kind: domain.ItemShow, Title: "Breaking Bad", Seasons: []int{1, 2}, IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396", domain.ProviderTVDB: "81189"}}
	ok := `{"data": {"%s": {"success": true, "message": "Requested", "statusText": "CREATED"}}}`

	a := &asked{status: http.StatusOK, answer: strings.ReplaceAll(ok, "%s", "requestMovie")}
	if err := serve(t, a).Request(t.Context(), film); err != nil {
		t.Fatal(err)
	}
	if a.key != "k3y" || !strings.Contains(a.query, "requestMovie(input: $input)") || a.input.TMDBID != "27205" || a.input.IMDbID != "tt1375666" ||
		a.input.ExternalRequestID != id.String() || a.input.RequestedBy != "photon" || a.input.Title != "Inception" {
		t.Errorf("a film asked as %+v by %q with key %q", a.input, a.query, a.key)
	}

	a = &asked{status: http.StatusOK, answer: strings.ReplaceAll(ok, "%s", "requestShow")}
	if err := serve(t, a).Request(t.Context(), show); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.query, "requestShow(input: $input)") || a.input.TVDBID != "81189" || a.input.TMDBID != "" || !slices.Equal(a.input.Seasons, []int{1, 2}) {
		t.Errorf("a show asked as %+v by %q", a.input, a.query)
	}

	a = &asked{status: http.StatusOK, answer: `{"data": {"requestMovie": {"success": true, "message": "Already requested", "statusText": "CONFLICT"}}}`}
	if err := serve(t, a).Request(t.Context(), film); err != nil {
		t.Errorf("asked again: %v, want it taken on", err)
	}
	a = &asked{status: http.StatusOK, answer: `{"data": {"requestMovie": {"success": false, "message": "No TMDB entry", "statusText": "NOT_FOUND"}}}`}
	if err := serve(t, a).Request(t.Context(), film); err == nil || !strings.Contains(err.Error(), "No TMDB entry") || errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("refused: %v, want riven's words", err)
	}
	a = &asked{status: http.StatusOK, answer: `{"data": null, "errors": [{"message": "Unauthorized"}]}`}
	if err := serve(t, a).Request(t.Context(), film); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("a GraphQL error: %v", err)
	}
	if err := serve(t, &asked{status: http.StatusBadGateway}).Request(t.Context(), film); !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("riven failing on its side: %v, want ErrUnavailable", err)
	}
	if err := serve(t, &asked{unreached: true}).Request(t.Context(), film); !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("riven not answering: %v, want ErrUnavailable", err)
	}

	noTVDB := show
	noTVDB.IDs = map[domain.Provider]string{domain.ProviderTMDB: "1396"}
	a = &asked{status: http.StatusOK}
	if err := serve(t, a).Request(t.Context(), noTVDB); err == nil || a.query != "" {
		t.Errorf("a show with no TheTVDB or IMDb id: %v, asked %q; want it refused unasked", err, a.query)
	}
}
