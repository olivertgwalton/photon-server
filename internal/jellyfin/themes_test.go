//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// aShowWithATheme is a household's show, The Wire, with a theme tune in its folder and one episode;
// and Ada, who may see it.
func aShowWithATheme(t *testing.T) (st *store.Store, ada domain.Profile, show, episode uuid.UUID) {
	t.Helper()
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
	t.Cleanup(st.Close)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Wire"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Wire", "theme.mp3"), []byte("way down in the hole"), 0o600); err != nil {
		t.Fatal(err)
	}
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, root)
	if err != nil {
		t.Fatal(err)
	}
	ep := store.Episode{Season: 1, Episodes: []int{1}, Title: "S01E01.mkv", Folder: "Wire", ByNumber: true, Copies: []store.Copy{{
		ContentKey: []byte("e1"), Parts: []store.Part{{RelPath: "Wire/S01E01.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Wire", []byte("v"), store.Show{Title: "The Wire", Folder: "Wire", Themes: []string{"Wire/theme.mp3"}}, []store.Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	if ada, err = st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil); err != nil {
		t.Fatal(err)
	}
	cards, _, err := st.Wall(ctx, []uuid.UUID{tv.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	episodes, err := st.Episodes(ctx, ada.ID, cards[0].ID)
	if err != nil || len(episodes) != 1 {
		t.Fatal(episodes, err)
	}
	return st, ada, cards[0].ID, episodes[0].ID
}

// themeResult is Jellyfin's ThemeMediaResult as an app reads it.
type themeResult struct {
	Items   []map[string]any
	OwnerID string `json:"OwnerId"`
}

// An app plays a show's theme tune under its pages, as Jellyfin's web app does: asks for its theme
// media, the show's own and, on a season's or episode's page, its parent's, and keeps playing it
// while the owner stays the same. photon has no theme videos.
func TestAnAppListsAShowsThemeSongs(t *testing.T) {
	st, ada, show, episode := aShowWithATheme(t)
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st,
	})
	get := func(target string, v any) {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
		if err := json.Unmarshal(w.Body.Bytes(), v); w.Code != http.StatusOK || err != nil {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
	}
	var songs themeResult
	get("/Items/"+guid(show)+"/ThemeSongs", &songs)
	if len(songs.Items) != 1 || songs.OwnerID != guid(show) || songs.Items[0]["Type"] != "Audio" || songs.Items[0]["MediaType"] != "Audio" {
		t.Fatalf("the show's theme songs: %+v", songs)
	}
	requireKeys(t, "a theme song", songs.Items[0], "Id", "Name", "ServerId", "ImageTags", "BackdropImageTags")

	var all struct{ ThemeVideosResult, ThemeSongsResult, SoundtrackSongsResult themeResult }
	get("/Items/"+guid(episode)+"/ThemeMedia?inheritFromParent=true&sortBy=Random", &all)
	if len(all.ThemeSongsResult.Items) != 1 || all.ThemeSongsResult.OwnerID != guid(show) || all.ThemeSongsResult.Items[0]["Id"] != songs.Items[0]["Id"] {
		t.Errorf("an episode's theme songs, its show's: %+v", all.ThemeSongsResult)
	}
	if all.ThemeVideosResult.Items == nil || len(all.ThemeVideosResult.Items) != 0 || all.SoundtrackSongsResult.Items == nil {
		t.Errorf("theme videos and soundtracks: %+v, %+v, want none", all.ThemeVideosResult, all.SoundtrackSongsResult)
	}
	get("/Items/"+guid(episode)+"/ThemeSongs", &songs)
	if len(songs.Items) != 0 || songs.OwnerID != guid(episode) {
		t.Errorf("an episode's own theme songs: %+v, want none", songs)
	}
	get("/Items/"+guid(show)+"/ThemeVideos", &songs)
	if len(songs.Items) != 0 || songs.OwnerID != guid(show) {
		t.Errorf("theme videos: %+v, want none", songs)
	}
}

// An app plays a theme song where Jellyfin's apps play audio: Jellyfin's web app at its universal
// address with the token in it, others at its stream, in ranges.
func TestAnAppPlaysAThemeSong(t *testing.T) {
	st, ada, show, _ := aShowWithATheme(t)
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, Themes: st,
	})
	page, err := st.Title(t.Context(), ada.ID, show)
	if err != nil || len(page.Themes) != 1 {
		t.Fatal(page.Themes, err)
	}
	theme := guid(page.Themes[0])
	w := serve(api, http.MethodGet, "/Audio/"+theme+"/universal?UserId="+guid(ada.ID)+"&DeviceId=TW96&MaxStreamingBitrate=140000000"+
		"&Container=opus,webm|opus,mp3,aac,m4a|aac,m4b|aac,flac,webma,webm|webma,wav,ogg&TranscodingContainer=mp4&TranscodingProtocol=hls"+
		"&AudioCodec=aac&ApiKey=pst_ada&PlaySessionId=1&StartTimeTicks=0&EnableRedirection=true&EnableRemoteMedia=false", "", "")
	if w.Code != http.StatusOK || w.Body.String() != "way down in the hole" || w.Header().Get("Content-Type") != "audio/mpeg" {
		t.Errorf("the universal address: %d %s %q", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/audio/"+theme+"/stream.mp3?static=true", nil)
	r.Header.Set("Authorization", `MediaBrowser Token="pst_ada"`)
	r.Header.Set("Range", "bytes=4-7")
	rw := httptest.NewRecorder()
	api.ServeHTTP(rw, r)
	if rw.Code != http.StatusPartialContent || rw.Body.String() != "down" {
		t.Errorf("a range of the stream: %d %q", rw.Code, rw.Body)
	}
	for _, target := range []string{"/Audio/" + theme + "/master.m3u8", "/Audio/" + guid(show) + "/stream", "/Audio/" + guid(uuid.NewV7()) + "/universal"} {
		if w := serve(api, http.MethodGet, target, `MediaBrowser Token="pst_ada"`, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, w.Code)
		}
	}
	if w := serve(api, http.MethodGet, "/Audio/"+theme+"/universal", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("without a token: %d, want 401", w.Code)
	}
}
