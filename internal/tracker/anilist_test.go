package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/animelist"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type unpaused struct{}

func (unpaused) Pause(context.Context, string, time.Duration) error { return nil }

func (unpaused) Paused(context.Context, string) (time.Duration, error) { return 0, nil }

// animeLists place Attack on Titan's season 3 in two cours, and Princess Mononoke.
type animeLists struct{}

var titan = []animelist.Entry{
	{AniDB: 13241, AniList: 99147, TVDB: animelist.Placing{Show: 267440, Season: 3}},
	{AniDB: 14444, AniList: 104578, TVDB: animelist.Placing{Show: 267440, Season: 3, Offset: 12}},
}

func (animeLists) AnimeShow(_ context.Context, p domain.Provider, show int) ([]animelist.Entry, error) {
	if p == domain.ProviderTVDB && show == 267440 {
		return titan, nil
	}
	return nil, nil
}

func (animeLists) AnimeEntry(_ context.Context, p domain.Provider, id string) (animelist.Entry, bool, error) {
	if p == domain.ProviderTMDB && id == "128" {
		return animelist.Entry{AniDB: 7, AniList: 164}, true, nil
	}
	return animelist.Entry{}, false, nil
}

// fakeAniList is an account's list on AniList: how far into each entry, and the entries' lengths.
type fakeAniList struct {
	mu       sync.Mutex
	list     map[int][2]any
	episodes map[int]int
	saved    []map[string]any
}

func (f *fakeAniList) serve(t *testing.T) aniList {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a.year.of.anime" {
			http.Error(w, `{"errors":[{"message":"Invalid token"}]}`, http.StatusBadRequest)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var data any
		switch {
		case strings.Contains(req.Query, "SaveMediaListEntry"):
			f.saved = append(f.saved, req.Variables)
			data = map[string]any{"SaveMediaListEntry": map[string]any{"id": 1}}
		case strings.Contains(req.Query, "mediaList"):
			var lists, media []map[string]any
			for _, id := range req.Variables["ids"].([]any) {
				n := int(id.(float64))
				if l, ok := f.list[n]; ok {
					lists = append(lists, map[string]any{"mediaId": n, "progress": l[0], "status": l[1]})
				}
				media = append(media, map[string]any{"id": n, "episodes": f.episodes[n]})
			}
			data = map[string]any{"Page": map[string]any{"mediaList": lists, "media": media}}
		default:
			data = map[string]any{"Viewer": map[string]any{"id": 42, "name": "Levi"}}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := newAniList(animeLists{}, unpaused{})
	c.api.Base = srv.URL
	return c
}

func watchedAt(season int, episodes ...int) watchedSeason {
	s := watchedSeason{Number: season}
	for _, e := range episodes {
		s.Episodes = append(s.Episodes, watchedEpisode{Number: e})
	}
	return s
}

func TestWhatIsWatchedMovesEachAniListEntryOn(t *testing.T) {
	f := &fakeAniList{list: map[int][2]any{99147: {12, "COMPLETED"}}, episodes: map[int]int{99147: 12, 104578: 10, 164: 1}}
	c := f.serve(t)
	h := history{
		Movies: []watchedTitle{{IDs: map[string]any{"tmdb": 128}}},
		Shows: []watchedShow{{
			IDs: map[string]any{"tvdb": 267440, "tmdb": 1429}, order: domain.OrderAired, numbering: domain.ProviderTVDB,
			Seasons: []watchedSeason{watchedAt(0, 9), watchedAt(3, 12, 13, 15)},
		}},
	}
	if err := c.history(t.Context(), "app", "a.year.of.anime", historyAdd, h); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(f.saved, func(a, b map[string]any) int { return int(a["id"].(float64) - b["id"].(float64)) })
	want := []map[string]any{
		{"id": 164.0, "progress": 1.0, "status": "COMPLETED"},
		{"id": 104578.0, "progress": 3.0, "status": "CURRENT"},
	}
	if len(f.saved) != 2 || !maps.Equal(f.saved[0], want[0]) || !maps.Equal(f.saved[1], want[1]) {
		t.Errorf("saved %v; want the film completed, the second cour at its 3rd, the first cour, completed already, left", f.saved)
	}
}

func TestAnAniListEntryIsNeverMovedBackAndCompletesAtItsLast(t *testing.T) {
	f := &fakeAniList{list: map[int][2]any{104578: {5, "CURRENT"}}, episodes: map[int]int{104578: 10}}
	c := f.serve(t)
	show := func(eps ...int) history {
		return history{Shows: []watchedShow{{
			IDs: map[string]any{"tvdb": 267440}, order: domain.OrderAired, numbering: domain.ProviderTVDB,
			Seasons: []watchedSeason{watchedAt(3, eps...)},
		}}}
	}
	if err := c.history(t.Context(), "app", "a.year.of.anime", historyAdd, show(14)); err != nil || len(f.saved) != 0 {
		t.Errorf("an episode behind the account's progress: saved %v, %v; want nothing", f.saved, err)
	}
	if err := c.history(t.Context(), "app", "a.year.of.anime", historyAdd, show(22)); err != nil || len(f.saved) != 1 ||
		!maps.Equal(f.saved[0], map[string]any{"id": 104578.0, "progress": 10.0, "status": "COMPLETED"}) {
		t.Errorf("its last: saved %v, %v; want the entry completed", f.saved, err)
	}
	if err := c.history(t.Context(), "app", "a.year.of.anime", historyRemove, show(13)); err != nil || len(f.saved) != 1 {
		t.Errorf("unwatched: saved %v, %v; want AniList left as it is", f.saved, err)
	}
}

func TestAniListIsLinkedByTheTokenItShows(t *testing.T) {
	c := (&fakeAniList{}).serve(t)
	link, err := c.code(t.Context(), "app")
	if err != nil || link.VerificationURIComplete != "https://anilist.co/api/v2/oauth/authorize?client_id=app&response_type=token" || link.Interval < aniListPaste {
		t.Errorf("link %+v, %v; want AniList's sign-in page, never asked after", link, err)
	}
	if name, err := c.username(t.Context(), "app", "a.year.of.anime"); err != nil || name != "Levi" {
		t.Errorf("the account: %q, %v", name, err)
	}
	if _, err := c.username(t.Context(), "app", "stale"); err == nil {
		t.Error("a token AniList refuses was taken")
	}
	if err := c.scrobble(t.Context(), "app", "a.year.of.anime", actionStop, play{Progress: 100}); !errors.Is(err, errNoPlays) {
		t.Errorf("a play told: %v; want it left to the history push", err)
	}
}
