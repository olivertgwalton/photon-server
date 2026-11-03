package musicbrainz

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

const hurryUp = "14f83b04-2b9e-4974-9169-476f26475be3"

// answers are MusicBrainz's and the Cover Art Archive's, by path and the search asked.
var answers = map[string]string{
	"/ws/2/release-group/ releasegroup:\"Hurry Up Tomorrow\" AND artist:\"The Weeknd\"": `{"release-groups":[
		{"id":"` + hurryUp + `","score":100,"title":"Hurry Up Tomorrow","first-release-date":"2025-01-31","artist-credit":[{"name":"The Weeknd"}]},
		{"id":"c135c45c-9cf1-4dfe-9945-98ca9712555c","score":87,"title":"Hurry Up Tomorrow: Bonus Tracks","first-release-date":"2025-02-07"}]}`,
	"/ws/2/release-group/" + hurryUp + " ": `{"id":"` + hurryUp + `","title":"Hurry Up Tomorrow","first-release-date":"2025-01-31",
		"genres":[{"name":"pop","count":2},{"name":"contemporary r&b","count":5}]}`,
	"/ws/2/artist/ artist:\"The Weeknd\"": `{"artists":[
		{"id":"c8b03190-306c-4120-bb0b-6f2ebfc06ea9","score":100,"name":"The Weeknd","sort-name":"Weeknd, The"},
		{"id":"0b0b0b0b-0000-4000-8000-000000000000","score":64,"name":"Weekend"}]}`,
	"/ws/2/artist/ artist:\"Nirvana\"": `{"artists":[
		{"id":"5b11f4ce-a62d-471e-81fc-a69a8278c7da","score":100,"name":"Nirvana","disambiguation":"90s US grunge band"},
		{"id":"9282c8b4-ca0b-4c6b-b7e3-4f7762dfc4d6","score":100,"name":"Nirvana","disambiguation":"60s band from the UK"}]}`,
	"/ws/2/artist/c8b03190-306c-4120-bb0b-6f2ebfc06ea9 ": `{"id":"c8b03190-306c-4120-bb0b-6f2ebfc06ea9","name":"The Weeknd","sort-name":"Weeknd, The",
		"genres":[{"name":"r&b","count":9}]}`,
	"/release-group/" + hurryUp + " ": `{"images":[{"front":false,"image":"https://coverartarchive.org/release/c807/back.jpg"},
		{"front":true,"image":"https://coverartarchive.org/release/c807/45194765731.jpg","thumbnails":{"1200":"https://coverartarchive.org/release/c807/45194765731-1200.jpg"}}]}`,
}

// client asks a MusicBrainz and a Cover Art Archive served here, which refuse a request that does
// not name the server, and answers when each request came.
func client(t *testing.T) (*Client, func() []time.Time) {
	t.Helper()
	var mu sync.Mutex
	var at []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		at = append(at, time.Now())
		mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "photon-server/") {
			http.Error(w, "say who you are", http.StatusForbidden)
			return
		}
		a, ok := answers[r.URL.Path+" "+r.URL.Query().Get("query")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, a); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("1.0", nil)
	c.api.Base, c.covers.Base, c.api.HTTP, c.covers.HTTP = srv.URL+"/ws/2/", srv.URL+"/", srv.Client(), srv.Client()
	return c, func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		return at
	}
}

// An album is matched by its name and artist, the bonus tracks MusicBrainz is less sure of passed
// over, and an artist by its name, unless MusicBrainz is as sure of two namesakes.
func TestMusicIsMatchedByNameWhereItCarriesNoID(t *testing.T) {
	c, _ := client(t)
	for _, tc := range []struct {
		kind domain.ItemKind
		h    provider.Hints
		want string
	}{
		{domain.ItemAlbum, provider.Hints{Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Year: 2025}, hurryUp},
		{domain.ItemAlbum, provider.Hints{Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Year: 2019}, ""},
		{domain.ItemArtist, provider.Hints{Title: "The Weeknd"}, "c8b03190-306c-4120-bb0b-6f2ebfc06ea9"},
		{domain.ItemArtist, provider.Hints{Title: "Nirvana"}, ""},
		{domain.ItemAlbum, provider.Hints{Title: "Anything", IDs: map[domain.Provider]string{domain.ProviderMusicBrainz: hurryUp}}, hurryUp},
	} {
		got, err := c.Match(t.Context(), domain.Locale{}, tc.kind, tc.h)
		if err != nil || got != tc.want {
			t.Errorf("%s %+v: %q, %v; want %q", tc.kind, tc.h, got, err, tc.want)
		}
	}
}

// An album is described by its first release, its genres most voted first, and its front cover at
// the archive's large size; an artist by its sort name.
func TestMusicIsDescribedWithItsCover(t *testing.T) {
	c, _ := client(t)
	album, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemAlbum, hurryUp, domain.SeasonRequest{})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.Metadata{
		Title: "Hurry Up Tomorrow", Year: 2025, ReleaseDate: time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC),
		Genres: []string{"contemporary r&b", "pop"}, IDs: map[domain.Provider]string{domain.ProviderMusicBrainz: hurryUp},
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: "https://coverartarchive.org/release/c807/45194765731-1200.jpg"}},
	}
	if diff := cmp.Diff(want, album); diff != "" {
		t.Errorf("the album (-want +got):\n%s", diff)
	}
	artist, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemArtist, "c8b03190-306c-4120-bb0b-6f2ebfc06ea9", domain.SeasonRequest{})
	if err != nil || artist.SortTitle != "Weeknd, The" || !cmp.Equal(artist.Genres, []string{"r&b"}) {
		t.Errorf("the artist: %+v, %v; want sorted under W", artist, err)
	}
	none, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemAlbum, "c135c45c-9cf1-4dfe-9945-98ca9712555c", domain.SeasonRequest{})
	if err == nil {
		t.Errorf("an album MusicBrainz has not: %+v, want its refusal", none)
	}
}

// MusicBrainz is asked no more than once a second, as it asks of an address.
func TestMusicBrainzIsAskedOnceASecond(t *testing.T) {
	c, at := client(t)
	for range 3 {
		if _, err := c.Match(t.Context(), domain.Locale{}, domain.ItemArtist, provider.Hints{Title: "The Weeknd"}); err != nil {
			t.Fatal(err)
		}
	}
	times := at()
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < every-10*time.Millisecond {
			t.Errorf("request %d came %v after the last, want at least %v", i, gap, every)
		}
	}
}
