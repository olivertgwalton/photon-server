package anidb

import (
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// titan is Attack on Titan's season 3 as the anime lists place it: two cours, the first's recap
// among the show's specials.
var titan = []animelist.Entry{
	{
		AniDB: 13241, AniList: 99147, TVDB: animelist.Placing{Show: 267440, Season: 3},
		Mappings: []animelist.Mapping{{Provider: domain.ProviderTVDB, Special: true, Season: 0, Episodes: map[int][]int{1: {9}}}},
	},
	{AniDB: 14444, AniList: 104578, TVDB: animelist.Placing{Show: 267440, Season: 3, Offset: 12}},
}

type fakeLists struct{}

func (fakeLists) AnimeShow(_ context.Context, p domain.Provider, show int) ([]animelist.Entry, error) {
	if p == domain.ProviderTVDB && show == 267440 {
		return titan, nil
	}
	return nil, nil
}

func (fakeLists) AnimeEntry(_ context.Context, p domain.Provider, id string) (animelist.Entry, bool, error) {
	for _, e := range titan {
		if p == domain.ProviderAniDB && strconv.Itoa(e.AniDB) == id {
			return e, true, nil
		}
	}
	return animelist.Entry{}, false, nil
}

// valkey keeps AniDB's answers as Valkey would.
type valkey struct {
	mu   sync.Mutex
	kept map[string][]byte
}

func (*valkey) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

func (k *valkey) KeepAniDB(_ context.Context, name string, answer []byte, _ time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.kept[name] = answer
	return nil
}

func (k *valkey) AniDB(_ context.Context, name string) ([]byte, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	answer, ok := k.kept[name]
	return answer, ok, nil
}

func registered(context.Context) (map[string]string, error) {
	return map[string]string{clientSetting: "photon", versionSetting: "1"}, nil
}

// serve answers as AniDB does, gzipped, its anime from testdata; banned answers that it has banned
// the client.
func serve(t *testing.T, settings provider.Settings, banned bool) (*Client, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/httpapi" || q.Get("request") != "anime" || q.Get("client") != "photon" || q.Get("clientver") != "1" || q.Get("protover") != "1" {
			t.Errorf("asked %s", r.URL)
		}
		asked = append(asked, q.Get("aid"))
		body := []byte(`<error code="500">banned</error>`)
		if !banned {
			var err error
			if body, err = os.ReadFile("testdata/" + q.Get("aid") + ".xml"); err != nil {
				body = []byte(`<error code="330">No such anime</error>`)
			}
		}
		z := gzip.NewWriter(w)
		if _, err := z.Write(body); err != nil {
			t.Error(err)
		}
		if err := z.Close(); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(settings, fakeLists{}, &valkey{kept: map[string][]byte{}})
	c.api.Base = srv.URL
	return c, &asked
}

func TestASeasonIsDescribedByTheEpisodesOfEachOfItsEntries(t *testing.T) {
	c, asked := serve(t, registered, false)
	m, seasons, err := c.Describe(t.Context(), domain.LocaleOf("en-GB"), domain.ItemShow, "13241",
		domain.SeasonRequest{Numbers: []int{0, 3}, Order: domain.OrderAired, Numbering: domain.ProviderTVDB})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Attack on Titan Season 3" || m.OriginalTitle != "進撃の巨人 Season 3" || m.Year != 2018 ||
		m.Overview != "The third season of Shingeki no Kyojin.\nSource: Kodansha" ||
		m.Ratings[0] != (domain.Rating{Site: domain.SiteAniDB, Score: 84.3, Votes: 4380}) ||
		m.Artwork[0].URL != "https://cdn.anidb.net/images/main/229587.jpg" || m.IDs[domain.ProviderAniList] != "99147" {
		t.Errorf("the show: %+v", m)
	}
	if len(m.Credits) != 3 || m.Credits[0].Role != "Eren Yeager" || m.Credits[1].Kind != domain.CreditDirector || m.Credits[2].Kind != domain.CreditCreator {
		t.Errorf("credits %+v; want Eren's voice, the director and the original work's author", m.Credits)
	}
	three := seasons[3]
	if three.Metadata.Title != "Attack on Titan Season 3" || three.Episodes[1].Title != "Smoke Signal" ||
		three.Episodes[1].Overview != "Eren and the Scouts make camp." || three.Episodes[12].Title != "Night of the Battle to Retake the Wall" ||
		three.Episodes[13].Title != "The Town Where Everything Began" {
		t.Errorf("season 3: %+v; want both cours' episodes, the second's first as its 13th", three)
	}
	if seasons[0].Episodes[9].Title != "Recap" || len(seasons[0].Episodes) != 1 || seasons[0].Metadata.Title != "" {
		t.Errorf("the specials: %+v; want the recap as special 9 and no name of one entry's", seasons[0])
	}
	if len(*asked) != 2 {
		t.Errorf("asked AniDB for %v; want each anime once", *asked)
	}
	if _, _, err := c.Describe(t.Context(), domain.LocaleOf("en-GB"), domain.ItemShow, "13241", domain.SeasonRequest{}); err != nil || len(*asked) != 2 {
		t.Errorf("asked again: %v, %v; want the day's answer kept", *asked, err)
	}
}

func TestAJapaneseLibraryTakesAniDBsJapaneseTitles(t *testing.T) {
	c, _ := serve(t, registered, false)
	m, seasons, err := c.Describe(t.Context(), domain.LocaleOf("ja-JP"), domain.ItemShow, "13241",
		domain.SeasonRequest{Numbers: []int{3}, Order: domain.OrderAired, Numbering: domain.ProviderTVDB})
	if err != nil || m.Title != "進撃の巨人 Season 3" || seasons[3].Episodes[1].Title != "狼煙" || seasons[3].Episodes[13].Title != "The Town Where Everything Began" {
		t.Errorf("%q, %+v, %v; want the Japanese titles where AniDB has them, else its own", m.Title, seasons[3].Episodes, err)
	}
}

func TestABanParksAniDBUntilItLapses(t *testing.T) {
	c, asked := serve(t, registered, true)
	for range 2 {
		if _, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemShow, "13241", domain.SeasonRequest{}); !errors.Is(err, provider.ErrUnavailable) {
			t.Fatalf("banned: %v; want the provider unavailable", err)
		}
	}
	if len(*asked) != 1 {
		t.Errorf("asked %v; want nothing asked while banned", *asked)
	}
}

func TestAnAnimeAniDBHasNotIsNotFound(t *testing.T) {
	c, _ := serve(t, registered, false)
	if _, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemShow, "1", domain.SeasonRequest{}); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("no such anime: %v", err)
	}
}

func TestAniDBIsNotAskedUntilAClientIsRegistered(t *testing.T) {
	c, asked := serve(t, func(context.Context) (map[string]string, error) { return nil, nil }, false)
	if _, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemShow, "13241", domain.SeasonRequest{}); !errors.Is(err, provider.ErrNotConfigured) || len(*asked) != 0 {
		t.Errorf("unregistered: %v, asked %v", err, *asked)
	}
}

func TestATitleIsMatchedByItsAniDBIDAloneWhereTheListsDoNotNameIt(t *testing.T) {
	c, _ := serve(t, registered, false)
	if got, err := c.Match(t.Context(), domain.Locale{}, domain.ItemShow, provider.Hints{IDs: map[domain.Provider]string{domain.ProviderAniDB: "14444"}}); err != nil || got != "14444" {
		t.Errorf("by its AniDB id: %q, %v", got, err)
	}
	if got, err := c.Match(t.Context(), domain.Locale{}, domain.ItemShow, provider.Hints{IDs: map[domain.Provider]string{domain.ProviderTVDB: "121361"}}); err != nil || got != "" {
		t.Errorf("a show the lists do not name: %q, %v; want no match", got, err)
	}
}

func TestAnAnswerThatUnzipsPastTheCapIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		z := gzip.NewWriter(w)
		if _, err := z.Write(make([]byte, provider.MaxAnswer+1)); err != nil {
			t.Error(err)
		}
		if err := z.Close(); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(registered, fakeLists{}, &valkey{kept: map[string][]byte{}})
	c.api.Base = srv.URL
	if _, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemShow, "13241", domain.SeasonRequest{}); err == nil || !strings.Contains(err.Error(), "unzips to more than") {
		t.Errorf("a gzip bomb: %v; want it refused", err)
	}
}
