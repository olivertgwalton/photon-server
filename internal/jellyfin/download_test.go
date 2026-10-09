//go:build integration

package jellyfin

import (
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
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
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Auth: profiles{"pst_ada": ada, "pst_kid": kid}, Catalogue: st, Preferences: st, Playing: st, Parts: library.Parts{Places: st}, Sent: sent,
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

// An app offers to download only a title whose download would be served: one whose copy played
// unasked is one file. A list says so where an app asks for it, as Jellyfin's does.
func TestAnAppOffersToDownloadOnlyWhatDownloads(t *testing.T) {
	st, ada, heat, _ := aFilm(t)
	long, err := st.AddLibrary(t.Context(), "Long", domain.LibraryMovies, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string) store.Part {
		return store.Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour, Container: "matroska,webm"}}
	}
	film := store.Film{Title: "Shoah", Folder: "Shoah", Copies: []store.Copy{{ContentKey: []byte("shoah"), Parts: []store.Part{part("Shoah/1.mkv"), part("Shoah/2.mkv")}}}}
	if _, err := st.SaveFolder(t.Context(), long.ID, "Shoah", []byte("v"), []store.Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(t.Context(), []uuid.UUID{long.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	shoah := cards[0].ID
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, Playing: st, Parts: library.Parts{Places: st}, Sent: playback.NewSent(),
	})
	const header = `MediaBrowser Token="pst_ada"`
	for id, want := range map[uuid.UUID]bool{heat: true, shoah: false} {
		if it := object(t, serve(api, http.MethodGet, "/Items/"+guid(id), header, "")); it["CanDownload"] != want {
			t.Errorf("%s: CanDownload %v, want %v", it["Name"], it["CanDownload"], want)
		}
	}
	if w := serve(api, http.MethodGet, "/Items/"+guid(shoah)+"/Download", header, ""); w.Code != http.StatusConflict {
		t.Errorf("downloading a film in two files: %d, want 409", w.Code)
	}
	for fields, want := range map[string]map[string]any{
		"CanDownload": {"Heat": true, "Shoah": false},
		"Overview":    {"Heat": nil, "Shoah": nil},
	} {
		items, _ := object(t, serve(api, http.MethodGet, "/Items?recursive=true&includeItemTypes=Movie&fields="+fields, header, ""))["Items"].([]any)
		got := map[string]any{}
		for _, it := range items {
			got[it.(map[string]any)["Name"].(string)] = it.(map[string]any)["CanDownload"]
		}
		if !maps.Equal(got, want) {
			t.Errorf("a list asking for %s: %v, want %v", fields, got, want)
		}
	}
}

// A .strm downloads as the media it names, under its name and its media's container, not as a
// line of text.
func TestAnAppDownloadsAStrmAsItsMedia(t *testing.T) {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "heat.mkv", time.Unix(0, 0), strings.NewReader("0123456789"))
	}))
	defer media.Close()
	ctx := t.Context()
	db := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, db, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Heat (1995).strm"), []byte(media.URL+"/heat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root)
	if err != nil {
		t.Fatal(err)
	}
	part := store.Part{RelPath: "Heat (1995).strm", Size: 40, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Container: "matroska,webm", Size: 10, Duration: time.Hour,
		Streams: []domain.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR}},
	}}
	film := store.Film{Title: "Heat", Copies: []store.Copy{{ContentKey: []byte("heat"), Parts: []store.Part{part}}}}
	if _, err := st.SaveFolder(ctx, films.ID, ".", []byte("v"), []store.Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{films.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: noCopies{}, Discover: noDiscoveries{},
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, Playing: st, Parts: library.Parts{Places: st}, Sent: playback.NewSent(),
	})
	w := serve(api, http.MethodGet, "/Items/"+guid(cards[0].ID)+"/Download?ApiKey=pst_ada", "", "")
	if w.Code != http.StatusOK || w.Body.String() != "0123456789" ||
		w.Header().Get("Content-Disposition") != `attachment; filename="Heat (1995).mkv"` {
		t.Errorf("the download: %d %v %q", w.Code, w.Header(), w.Body)
	}
}
