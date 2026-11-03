package lrclib

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// answers are LRCLIB's, by path and query, as it answered them in November 2026.
var answers = map[string]string{
	"/get album_name=Hurry+Up+Tomorrow&artist_name=The+Weeknd&duration=309&track_name=Wake+Me+Up": `{"id":1,"duration":308.6,"instrumental":false,"plainLyrics":"Wake me up","syncedLyrics":"[00:12.00]Wake me up"}`,
	"/search artist_name=The+Weeknd&track_name=Cry+For+Me":                                        `[{"duration":180,"plainLyrics":"Another","syncedLyrics":""},{"duration":224.5,"plainLyrics":"Cry for me","syncedLyrics":""}]`,
	"/get album_name=Hurry+Up+Tomorrow&artist_name=The+Weeknd&duration=60&track_name=Interlude":   `{"duration":60,"instrumental":true,"plainLyrics":"","syncedLyrics":""}`,
	"/search artist_name=The+Weeknd&track_name=Untold":                                            `[]`,
}

func client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "photon-server/") {
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
	c.api.Base, c.api.HTTP = srv.URL+"/", srv.Client()
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
