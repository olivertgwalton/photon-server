//go:build integration

package jellyfin

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// An app downloads a film as it is, to keep on the device: the file under its own name, in ranges
// so a download broken off goes on, counted as media the node sent. A profile downloads only what
// it may play.
func TestAnAppDownloadsAFilm(t *testing.T) {
	st, ada, heat, copyID := aFilm(t)
	tv, err := st.AddLibrary(t.Context(), "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.AddProfile(t.Context(), "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccess(t.Context(), kid.ID, store.ProfileAccess{Libraries: []uuid.UUID{tv.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	sent := playback.NewSent()
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada, "pst_kid": kid}, Catalogue: st, Playing: st, Sent: sent,
	})
	w := serve(api, http.MethodGet, "/Items/"+guid(heat)+"/Download?ApiKey=pst_ada", "", "")
	if w.Code != http.StatusOK || w.Body.String() != "0123456789" || w.Header().Get("Content-Type") != "video/x-matroska" ||
		w.Header().Get("Content-Disposition") != "attachment; filename=Heat.mkv" {
		t.Errorf("the download: %d %v %q", w.Code, w.Header(), w.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/items/"+guid(heat)+"/download?mediaSourceId="+guid(copyID), nil)
	r.Header.Set("Authorization", `MediaBrowser Token="pst_ada"`)
	r.Header.Set("Range", "bytes=6-")
	rw := httptest.NewRecorder()
	api.ServeHTTP(rw, r)
	if rw.Code != http.StatusPartialContent || rw.Body.String() != "6789" {
		t.Errorf("the rest of a download broken off: %d %q", rw.Code, rw.Body)
	}
	if err := testutil.CollectAndCompare(sent, strings.NewReader(`
# HELP photon_sent_bytes_total The bytes of media this node sent its players, by how each was delivered.
# TYPE photon_sent_bytes_total counter
photon_sent_bytes_total{delivery="file"} 14
photon_sent_bytes_total{delivery="segment"} 0
`)); err != nil {
		t.Errorf("counted: %v", err)
	}
	for token, want := range map[string]int{"pst_kid": http.StatusNotFound, "": http.StatusUnauthorized} {
		w := serve(api, http.MethodGet, "/Items/"+guid(heat)+"/Download", `MediaBrowser Token="`+token+`"`, "")
		if w.Code != want || w.Header().Get("Content-Disposition") != "" {
			t.Errorf("downloading with %q: %d %v, want %d and nothing to save", token, w.Code, w.Header(), want)
		}
	}
}
