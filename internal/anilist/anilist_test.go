package anilist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

type unlimited struct{}

func (unlimited) Allow(context.Context, string, kv.Limit) (time.Duration, error) { return 0, nil }

// titan is Attack on Titan as the anime lists place it: its first season, and season 3's two cours.
var titan = []animelist.Entry{
	{AniDB: 9541, AniList: 16498, TVDB: animelist.Placing{Show: 267440, Season: 1}, TMDB: animelist.Placing{Show: 1429, Season: 1}},
	{AniDB: 13241, AniList: 99147, TVDB: animelist.Placing{Show: 267440, Season: 3}, TMDB: animelist.Placing{Show: 1429, Season: 3}},
	{AniDB: 14444, AniList: 104578, TVDB: animelist.Placing{Show: 267440, Season: 3, Offset: 12}, TMDB: animelist.Placing{Show: 1429, Season: 3, Offset: 12}},
}

var mononoke = animelist.Entry{AniDB: 7, AniList: 164, TMDBMovies: []int{128}, IMDb: []string{"tt0119698"}}

type fakeLists struct{}

func (fakeLists) AnimeShow(_ context.Context, p domain.Provider, show int) ([]animelist.Entry, error) {
	var out []animelist.Entry
	for _, e := range titan {
		if e.Placing(p).Show == show {
			out = append(out, e)
		}
	}
	return out, nil
}

func (fakeLists) AnimeEntry(_ context.Context, p domain.Provider, id string) (animelist.Entry, bool, error) {
	for _, e := range append(slices.Clone(titan), mononoke) {
		if p == domain.ProviderAniList && strconv.Itoa(e.AniList) == id || p == domain.ProviderAniDB && strconv.Itoa(e.AniDB) == id ||
			p == domain.ProviderIMDb && slices.Contains(e.IMDb, id) {
			return e, true, nil
		}
	}
	return animelist.Entry{}, false, nil
}

// entries are AniList's, as it answers them.
var entries = map[int]string{
	16498: `{"id":16498,"title":{"romaji":"Shingeki no Kyojin","english":"Attack on Titan","native":"進撃の巨人"},
		"description":"Centuries ago, mankind was slaughtered...<br><br>\n(Source: Kodansha)","startDate":{"year":2013,"month":4,"day":7},
		"genres":["Action","Drama"],"averageScore":85,"coverImage":{"extraLarge":"https://s4.anilist.co/cover/bx16498.jpg"},
		"bannerImage":"https://s4.anilist.co/banner/16498.jpg",
		"studios":{"nodes":[{"name":"Wit Studio","isAnimationStudio":true},{"name":"Pony Canyon","isAnimationStudio":false}]},
		"characters":{"edges":[{"node":{"name":{"full":"Eren Yeager"}},"voiceActors":[{"id":95991,"name":{"full":"Yuki Kaji"},"image":{"large":"https://s4.anilist.co/staff/n95991.jpg"}}]}]},
		"staff":{"edges":[{"role":"Director","node":{"id":96879,"name":{"full":"Tetsurou Araki"},"image":{"large":"https://s4.anilist.co/staff/default.jpg"}}},
			{"role":"Key Animation (ep 1)","node":{"id":1,"name":{"full":"Someone"}}}]}}`,
	99147: `{"id":99147,"title":{"romaji":"Shingeki no Kyojin 3","english":"Attack on Titan Season 3"},"startDate":{"year":2018}}`,
}

func serve(t *testing.T) (*Client, *[][]int) {
	t.Helper()
	var asked [][]int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				IDs []int `json:"ids"`
			} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		asked = append(asked, req.Variables.IDs)
		var found []json.RawMessage
		for _, id := range req.Variables.IDs {
			if e, ok := entries[id]; ok {
				found = append(found, json.RawMessage(e))
			}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Page": map[string]any{"media": found}}}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(fakeLists{}, unlimited{})
	c.api.Base = srv.URL
	return c, &asked
}

func TestATitleIsMatchedOnlyThroughTheAnimeLists(t *testing.T) {
	c, _ := serve(t)
	for _, tc := range []struct {
		name string
		kind domain.ItemKind
		ids  map[domain.Provider]string
		want string
	}{
		{"a show by its TheTVDB id, as it opens", domain.ItemShow, map[domain.Provider]string{domain.ProviderTVDB: "267440"}, "16498"},
		{"a show by its TMDB id", domain.ItemShow, map[domain.Provider]string{domain.ProviderTMDB: "1429"}, "16498"},
		{"a show by its own AniList id", domain.ItemShow, map[domain.Provider]string{domain.ProviderAniList: "104578"}, "104578"},
		{"a show by an AniDB id", domain.ItemShow, map[domain.Provider]string{domain.ProviderAniDB: "13241"}, "99147"},
		{"a film by its IMDb id", domain.ItemMovie, map[domain.Provider]string{domain.ProviderIMDb: "tt0119698"}, "164"},
		{"a show the lists do not name", domain.ItemShow, map[domain.Provider]string{domain.ProviderTVDB: "121361"}, ""},
	} {
		got, err := c.Match(t.Context(), domain.Locale{}, tc.kind, providerHints(tc.ids))
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestAShowIsDescribedByTheEntriesItsSeasonsOpenWith(t *testing.T) {
	c, asked := serve(t)
	m, seasons, err := c.Describe(t.Context(), domain.LocaleOf("en-GB"), domain.ItemShow, "16498",
		domain.SeasonRequest{Numbers: []int{0, 3}, Order: domain.OrderAired, Numbering: domain.ProviderTVDB})
	if err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 || !slices.Contains((*asked)[0], 99147) {
		t.Errorf("asked %v; want the show and season 3's first cour in one request", *asked)
	}
	if m.Title != "Attack on Titan" || m.OriginalTitle != "進撃の巨人" || m.Overview != "Centuries ago, mankind was slaughtered...\n\n(Source: Kodansha)" ||
		m.Year != 2013 || !slices.Equal(m.Studios, []string{"Wit Studio"}) || m.Ratings[0] != (domain.Rating{Site: domain.SiteAniList, Score: 85}) ||
		m.IDs[domain.ProviderAniDB] != "9541" {
		t.Errorf("the show: %+v", m)
	}
	if len(m.Credits) != 2 || m.Credits[0].Role != "Eren Yeager" || m.Credits[1].Kind != domain.CreditDirector || m.Credits[1].Photo != "" {
		t.Errorf("credits %+v; want Eren's voice and the director, who has no picture", m.Credits)
	}
	if seasons[3].Metadata.Title != "Attack on Titan Season 3" || len(seasons) != 1 {
		t.Errorf("seasons %+v; want season 3 alone, named as its first cour, and not the specials", seasons)
	}
	ja, _, err := c.Describe(t.Context(), domain.LocaleOf("ja-JP"), domain.ItemShow, "16498", domain.SeasonRequest{})
	if err != nil || ja.Title != "進撃の巨人" {
		t.Errorf("in Japanese: %q, %v", ja.Title, err)
	}
}

func TestAShowNumberedOtherwiseIsDescribedWithoutItsSeasons(t *testing.T) {
	c, _ := serve(t)
	for _, order := range []domain.EpisodeOrder{domain.OrderDVD, domain.OrderGroup} {
		_, seasons, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemShow, "16498",
			domain.SeasonRequest{Numbers: []int{3}, Order: order, Numbering: domain.ProviderTVDB})
		if err != nil || len(seasons) != 0 {
			t.Errorf("%s: %+v, %v; want no seasons", order, seasons, err)
		}
	}
}

func providerHints(ids map[domain.Provider]string) provider.Hints {
	return provider.Hints{IDs: ids}
}
