package fanart

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type unlimited struct{}

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

// Answers as fanart.tv gives them, by path.
var answers = map[string]string{
	"/movies/578": `{"name":"Jaws","tmdb_id":"578","imdb_id":"tt0073195",
		"movieposter":[{"id":"1","url":"https://assets.fanart.tv/fanart/movies/578/movieposter/jaws-fr.jpg","lang":"fr","likes":"9"},
			{"id":"2","url":"https://assets.fanart.tv/fanart/movies/578/movieposter/jaws-en.jpg","lang":"en","likes":"3"}],
		"moviebackground":[{"id":"3","url":"https://assets.fanart.tv/fanart/movies/578/moviebackground/jaws-en.jpg","lang":"en","likes":"8"},
			{"id":"4","url":"https://assets.fanart.tv/fanart/movies/578/moviebackground/jaws.jpg","lang":"00","likes":"1"}],
		"movielogo":[{"id":"5","url":"https://assets.fanart.tv/fanart/movies/578/movielogo/jaws-old.png","lang":"en","likes":"7"}],
		"hdmovielogo":[{"id":"6","url":"https://assets.fanart.tv/fanart/movies/578/hdmovielogo/jaws-hd.png","lang":"en","likes":"7"}],
		"moviethumb":[{"id":"7","url":"https://assets.fanart.tv/fanart/movies/578/moviethumb/jaws.jpg","lang":"en","likes":"2","width":"1000","height":"562"}],
		"hdmovieclearart":[{"id":"8","url":"https://assets.fanart.tv/fanart/movies/578/hdmovieclearart/jaws.png","lang":"en","likes":"5"}]}`,
	"/tv/81189": `{"name":"Breaking Bad","thetvdb_id":"81189",
		"tvthumb":[{"id":"9","url":"https://assets.fanart.tv/fanart/tv/81189/tvthumb/bb.jpg","lang":"en","likes":"4"}],
		"seasonposter":[{"id":"10","url":"https://assets.fanart.tv/fanart/tv/81189/seasonposter/bb-s1.jpg","lang":"en","likes":"4","season":"1"}]}`,
}

// client asks a fanart.tv served here, which refuses every key where refuse is set, with an
// admin's personal key where personal is not empty.
func client(t *testing.T, personal string, refuse bool) (*Client, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		if refuse || r.URL.Query().Get("api_key") != projectKey {
			http.Error(w, `{"status":"error","error message":"Not authorized"}`, http.StatusUnauthorized)
			return
		}
		a, ok := answers[r.URL.Path]
		if !ok {
			http.Error(w, `{"status":"error","error message":"Not found"}`, http.StatusNotFound)
			return
		}
		if _, err := io.WriteString(w, a); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(func(context.Context) (map[string]string, error) {
		return map[string]string{"client_key": personal}, nil
	}, unlimited{})
	c.api.Base = srv.URL + "/"
	return c, &asked
}

func TestAFilmIsPicturedByItsTMDBID(t *testing.T) {
	c, asked := client(t, "mine", false)
	id, err := c.Match(t.Context(), domain.Locale{}, domain.ItemMovie, provider.Hints{
		IDs: map[domain.Provider]string{domain.ProviderTMDB: "578", domain.ProviderIMDb: "tt0073195"},
	})
	if err != nil || id != "578" {
		t.Fatalf("match = %q, %v", id, err)
	}
	m, seasons, err := c.Describe(t.Context(), domain.Locale{Language: "fr-FR"}, domain.ItemMovie, id, domain.SeasonRequest{})
	if err != nil || seasons != nil {
		t.Fatalf("describe: %v, %v", seasons, err)
	}
	// A poster lettered in the reader's language first; a backdrop with none first; an HD logo
	// before an older one liked as much; the lettered landscape picture as a wide card's. Clear art
	// is no kind a title is drawn by.
	art := "https://assets.fanart.tv/fanart/movies/578/"
	want := []domain.Artwork{
		{Kind: domain.ArtworkPoster, URL: art + "movieposter/jaws-fr.jpg", Language: "fr"},
		{Kind: domain.ArtworkPoster, URL: art + "movieposter/jaws-en.jpg", Language: "en"},
		{Kind: domain.ArtworkBackdrop, URL: art + "moviebackground/jaws.jpg"},
		{Kind: domain.ArtworkBackdrop, URL: art + "moviebackground/jaws-en.jpg", Language: "en"},
		{Kind: domain.ArtworkLogo, URL: art + "hdmovielogo/jaws-hd.png", Language: "en"},
		{Kind: domain.ArtworkLogo, URL: art + "movielogo/jaws-old.png", Language: "en"},
		{Kind: domain.ArtworkThumb, URL: art + "moviethumb/jaws.jpg", Language: "en", Width: 1000, Height: 562},
	}
	if diff := cmp.Diff(want, m.Artwork); diff != "" {
		t.Errorf("artwork (-want +got):\n%s", diff)
	}
	if m.Title != "" || m.Overview != "" {
		t.Errorf("fanart.tv says nothing but pictures: %+v", m)
	}
	if got := (*asked)[0]; got != "/movies/578?api_key="+projectKey+"&client_key=mine" {
		t.Errorf("asked %s", got)
	}
}

func TestAFilmWithNoTMDBIDIsAskedForByItsIMDbID(t *testing.T) {
	c, _ := client(t, "", false)
	id, err := c.Match(t.Context(), domain.Locale{}, domain.ItemMovie, provider.Hints{IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0073195"}})
	if err != nil || id != "tt0073195" {
		t.Errorf("match = %q, %v", id, err)
	}
}

func TestAShowIsPicturedByItsTheTVDBID(t *testing.T) {
	c, _ := client(t, "", false)
	hints := provider.Hints{IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396", domain.ProviderTVDB: "81189"}}
	id, err := c.Match(t.Context(), domain.Locale{}, domain.ItemShow, hints)
	if err != nil || id != "81189" {
		t.Fatalf("match = %q, %v", id, err)
	}
	m, _, err := c.Describe(t.Context(), domain.Locale{Language: "en"}, domain.ItemShow, id, domain.SeasonRequest{Numbers: []int{1}})
	want := []domain.Artwork{{Kind: domain.ArtworkThumb, URL: "https://assets.fanart.tv/fanart/tv/81189/tvthumb/bb.jpg", Language: "en"}}
	if diff := cmp.Diff(want, m.Artwork); err != nil || diff != "" {
		t.Errorf("artwork, %v (-want +got):\n%s", err, diff)
	}
	if id, err := c.Match(t.Context(), domain.Locale{}, domain.ItemShow, provider.Hints{IDs: map[domain.Provider]string{domain.ProviderTMDB: "1396"}}); err != nil || id != "" {
		t.Errorf("a show with no TheTVDB id matched as %q, %v", id, err)
	}
}

func TestATitleFanartHasNothingOfIsDescribedWithNothing(t *testing.T) {
	c, _ := client(t, "", false)
	m, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemMovie, "999999", domain.SeasonRequest{})
	if err != nil || len(m.Artwork) != 0 {
		t.Errorf("describe = %+v, %v", m, err)
	}
}

// With no personal key, fanart.tv is asked with photon's project key alone.
func TestAServerNeedsNoKeyOfItsOwn(t *testing.T) {
	c, asked := client(t, "", false)
	m, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemMovie, "578", domain.SeasonRequest{})
	if err != nil || len(m.Artwork) == 0 {
		t.Fatalf("describe = %d pictures, %v", len(m.Artwork), err)
	}
	if got := (*asked)[0]; got != "/movies/578?api_key="+projectKey {
		t.Errorf("asked %s", got)
	}
	for _, s := range c.Info().Settings {
		if s.Required {
			t.Errorf("an admin must set %s", s.Key)
		}
	}
}

func TestAKeyFanartRefusesPassesItOver(t *testing.T) {
	c, _ := client(t, "", true)
	if _, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemMovie, "578", domain.SeasonRequest{}); !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}
