package lrclib

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// answers are LRCLIB's, by path and query, as it answered them in November 2026.
var answers = map[string]string{
	"/get album_name=Hurry+Up+Tomorrow&artist_name=The+Weeknd&duration=309&track_name=Wake+Me+Up": `{"id":1,"duration":308.6,"instrumental":false,"plainLyrics":"Wake me up","syncedLyrics":"[00:12.00]Wake me up"}`,
	"/search artist_name=The+Weeknd&track_name=Cry+For+Me":                                        `[{"duration":180,"plainLyrics":"Another","syncedLyrics":""},{"duration":224.5,"plainLyrics":"Cry for me","syncedLyrics":""}]`,
	"/get album_name=Hurry+Up+Tomorrow&artist_name=The+Weeknd&duration=60&track_name=Interlude":   `{"duration":60,"instrumental":true,"plainLyrics":"","syncedLyrics":""}`,
	"/search artist_name=The+Weeknd&track_name=Untold":                                            `[]`,
	"/get album_name=Mixes&artist_name=The+Weeknd&track_name=Two+Hours":                           `{"duration":7200,"plainLyrics":"Long","syncedLyrics":""}`,
}

func client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "photon-server 1.0 (https://") {
			http.Error(w, "say who you are", http.StatusForbidden)
			return
		}
		a, ok := answers[r.URL.Path+" "+r.URL.RawQuery]
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
	c.api.Base, c.api.HTTP, c.gap = srv.URL+"/", srv.Client(), 0
	return c
}

// A track's lyrics are its recording's, timed where LRCLIB has them timed; else the first a search
// finds of about its length; and none for an instrumental or a track LRCLIB lacks.
func TestATracksLyricsAreFoundOnLRCLIB(t *testing.T) {
	c := client(t)
	for _, tc := range []struct {
		q    domain.LyricsQuery
		want string
	}{
		{domain.LyricsQuery{Artist: "The Weeknd", Title: "Wake Me Up", Album: "Hurry Up Tomorrow", Duration: 308573 * time.Millisecond}, "[00:12.00]Wake me up"},
		{domain.LyricsQuery{Artist: "The Weeknd", Title: "Cry For Me", Album: "Hurry Up Tomorrow", Duration: 224 * time.Second}, "Cry for me"},
		{domain.LyricsQuery{Artist: "The Weeknd", Title: "Interlude", Album: "Hurry Up Tomorrow", Duration: time.Minute}, ""},
		{domain.LyricsQuery{Artist: "The Weeknd", Title: "Untold", Album: "Hurry Up Tomorrow", Duration: time.Minute}, ""},
	} {
		got, err := c.Lyrics(t.Context(), tc.q)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.q.Title, got, err, tc.want)
		}
	}
}

// A track longer than LRCLIB takes, an hour, is looked up by its name alone, as LRCLIB refuses a
// lookup by a longer duration.
func TestATrackLongerThanAnHourIsLookedUpWithoutItsLength(t *testing.T) {
	c := client(t)
	got, err := c.Lyrics(t.Context(), domain.LyricsQuery{Artist: "The Weeknd", Title: "Two Hours", Album: "Mixes", Duration: 2 * time.Hour})
	if err != nil || got != "Long" {
		t.Errorf("%q, %v; want its lyrics", got, err)
	}
}

// Lookups asked at once go to LRCLIB one at a time, each half a second after the last was
// answered, as it asks of a client fetching a library's.
func TestLyricsAreAskedOneAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var at, most atomic.Int32
		var sent []time.Time
		var mu sync.Mutex
		c := New("1.0", nil)
		c.api.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			n := at.Add(1)
			defer at.Add(-1)
			if n > most.Load() {
				most.Store(n)
			}
			mu.Lock()
			sent = append(sent, time.Now())
			mu.Unlock()
			<-time.After(100 * time.Millisecond)
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
		})}
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				if _, err := c.Lyrics(t.Context(), domain.LyricsQuery{Artist: "A", Title: "B", Duration: time.Minute}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		if most.Load() != 1 {
			t.Errorf("%d requests at once, want one at a time", most.Load())
		}
		for i := 1; i < len(sent); i++ {
			if d := sent[i].Sub(sent[i-1]); d < 100*time.Millisecond+gap {
				t.Errorf("request %d sent %v after the one before, want its answer and %v between", i, d, gap)
			}
		}
	})
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
