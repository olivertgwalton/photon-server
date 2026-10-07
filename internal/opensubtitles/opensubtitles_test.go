package opensubtitles

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type unlimited struct{}

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

// server is OpenSubtitles: it knows Alien in English, and an episode of Lost; it signs Ada in,
// takes back the first token it gave once, and lets her fetch until quota runs out.
type server struct {
	signIns, downloads, quota int
	asked                     string
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The file a download links to is served apart, with no key.
	if r.URL.Path == "/file/12.srt" {
		_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nIn space\n"))
		return
	}
	if r.Header.Get("Api-Key") != "consumer" || r.Header.Get("User-Agent") == "" {
		http.Error(w, `{"message":"bad key"}`, http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/subtitles":
		s.asked = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[
			{"attributes":{"language":"en","release":"Alien.1979.Directors.Cut","download_count":900,"files":[{"file_id":11}]}},
			{"attributes":{"language":"en","release":"Alien.1979.1080p","download_count":20,"moviehash_match":true,"hearing_impaired":true,"files":[{"file_id":12}]}},
			{"attributes":{"language":"en","release":"Alien.1979.2CD","files":[{"file_id":13},{"file_id":14}]}}
		]}`))
	case "/login":
		s.signIns++
		_, _ = w.Write([]byte(`{"token":"t` + string(rune('0'+s.signIns)) + `"}`))
	case "/download":
		switch {
		case r.Header.Get("Authorization") == "Bearer t1":
			http.Error(w, `{"message":"token expired"}`, http.StatusUnauthorized)
		case s.downloads >= s.quota:
			http.Error(w, `{"remaining":0}`, http.StatusNotAcceptable)
		default:
			s.downloads++
			_, _ = w.Write([]byte(`{"link":"http://` + r.Host + `/file/12.srt","remaining":4}`))
		}
	default:
		http.NotFound(w, r)
	}
}

func client(t *testing.T, s *server) *Client {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	c := New(func(context.Context) (map[string]string, error) {
		return map[string]string{"api_key": "consumer", "username": "ada", "password": "secret"}, nil
	}, unlimited{})
	c.base = srv.URL
	return c
}

// A film is searched for by its bare IMDb id, its hash and language, in the order OpenSubtitles
// keeps answers for; one in several files is left out.
func TestSubtitlesAreSearchedAsOpenSubtitlesAsks(t *testing.T) {
	s := &server{}
	got, err := client(t, s).SearchSubtitles(t.Context(), domain.SubtitleQuery{
		Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0078748"},
		Hash: "8e245d9679d31e12", Language: language.MustParse("en-GB"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.asked != "imdb_id=78748&languages=en&moviehash=8e245d9679d31e12&type=movie" {
		t.Errorf("asked %q", s.asked)
	}
	want := []domain.FoundSubtitle{
		{Source: domain.SourceOpenSubtitles, ID: "11", Language: language.English, Release: "Alien.1979.Directors.Cut", Downloads: 900},
		{Source: domain.SourceOpenSubtitles, ID: "12", Language: language.English, Release: "Alien.1979.1080p", HearingImpaired: true, ForRelease: true, Downloads: 20},
	}
	if diff := cmp.Diff(want, got, cmp.Comparer(func(a, b language.Tag) bool { return a == b })); diff != "" {
		t.Errorf("found (-want +got):\n%s", diff)
	}
}

// An episode is searched for by its show's id and its numbers.
func TestAnEpisodeIsSearchedByItsShow(t *testing.T) {
	s := &server{}
	if _, err := client(t, s).SearchSubtitles(t.Context(), domain.SubtitleQuery{
		Kind: domain.ItemEpisode, IDs: map[domain.Provider]string{domain.ProviderTMDB: "4607"},
		Season: 1, Episode: 2, Language: language.BrazilianPortuguese,
	}); err != nil {
		t.Fatal(err)
	}
	if s.asked != "episode_number=2&languages=pt-br&parent_tmdb_id=4607&season_number=1&type=episode" {
		t.Errorf("asked %q", s.asked)
	}
}

// A subtitle is fetched signed in, again where the token is refused, as SubRip; a spent quota says
// so.
func TestASubtitleIsFetchedSignedIn(t *testing.T) {
	s := &server{quota: 1}
	c := client(t, s)
	got, err := c.FetchSubtitle(t.Context(), "12")
	if err != nil || !strings.Contains(string(got), "In space") || s.signIns != 2 {
		t.Fatalf("fetched %q, %v after %d sign-ins; want the SubRip, signed in again once", got, err, s.signIns)
	}
	if _, err := c.FetchSubtitle(t.Context(), "12"); !errors.Is(err, ErrQuota) {
		t.Errorf("past the quota: %v, want ErrQuota", err)
	}
	if s.signIns != 2 {
		t.Errorf("signed in %d times, want the token kept", s.signIns)
	}
	if _, err := New(func(context.Context) (map[string]string, error) { return map[string]string{}, nil }, unlimited{}).
		SearchSubtitles(t.Context(), domain.SubtitleQuery{}); !errors.Is(err, provider.ErrNotConfigured) {
		t.Errorf("with no key: %v, want ErrNotConfigured", err)
	}
}
