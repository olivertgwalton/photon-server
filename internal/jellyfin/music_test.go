//go:build integration

package jellyfin

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// musicItem is as much of a BaseItemDto as a music app browses by.
type musicItem struct {
	Name              string `json:"Name"`
	ID                string `json:"Id"`
	Type              string `json:"Type"`
	MediaType         string `json:"MediaType"`
	IsFolder          bool   `json:"IsFolder"`
	CollectionType    string `json:"CollectionType"`
	Album             string `json:"Album"`
	AlbumArtist       string `json:"AlbumArtist"`
	Artists           []string
	IndexNumber       *int `json:"IndexNumber"`
	ParentIndexNumber *int `json:"ParentIndexNumber"`
	HasLyrics         bool `json:"HasLyrics"`
}

// A music app browses a music library as Jellyfin's: its artists, its albums, an artist's albums
// and an album's tracks, each track on its disc under its album and album artist.
func TestAMusicAppBrowsesAMusicLibrary(t *testing.T) {
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
	music, err := st.AddLibrary(ctx, "Music", domain.LibraryMusic, "/srv/music", "")
	if err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	track := func(title string, disc, number int, artists ...string) store.Track {
		rel := "Hurry Up Tomorrow/" + title + ".flac"
		return store.Track{Title: title, Disc: disc, Number: number, Artists: artists, Folder: "Hurry Up Tomorrow", Version: store.Version{
			ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
				Duration: 4 * time.Minute, Streams: []domain.Stream{{Kind: domain.StreamAudio, Codec: "flac", Channels: 2}},
			}}},
		}}
	}
	sung := track("Wake Me Up", 1, 1, "The Weeknd")
	sung.Version.Lyrics = domain.TimedByLine
	if _, err := st.SaveMusicFolder(ctx, music.ID, "Hurry Up Tomorrow", []byte("v"), []store.Album{{
		Title: "Hurry Up Tomorrow", Artist: "The Weeknd", Year: 2025, Folder: "Hurry Up Tomorrow",
		Tracks: []store.Track{track("Timeless", 2, 2, "The Weeknd", "Playboi Carti"), sung},
	}}); err != nil {
		t.Fatal(err)
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, Playlists: st})
	items := func(target string) []musicItem {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Finamp", Token="pst_ada"`, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", target, w.Code)
		}
		var got struct{ Items []musicItem }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Items
	}
	views := items("/UserViews")
	if len(views) != 1 || views[0].CollectionType != "music" {
		t.Fatalf("views: %+v, want the music library as Jellyfin's music", views)
	}
	lib := views[0].ID
	n := func(i int) *int { return &i }
	for _, tc := range []struct {
		target string
		want   []musicItem
	}{
		{"/Artists/AlbumArtists?ParentId=" + lib, []musicItem{{Name: "The Weeknd", Type: "MusicArtist", IsFolder: true, MediaType: "Unknown"}}},
		{"/Items?ParentId=" + lib + "&IncludeItemTypes=MusicAlbum&Recursive=true", []musicItem{
			{Name: "Hurry Up Tomorrow", Type: "MusicAlbum", IsFolder: true, MediaType: "Unknown", AlbumArtist: "The Weeknd"},
		}},
	} {
		got := items(tc.target)
		if diff := cmp.Diff(tc.want, got, cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".ID" }, cmp.Ignore())); diff != "" {
			t.Errorf("%s (-want +got):\n%s", tc.target, diff)
		}
	}
	artist := items("/Artists/AlbumArtists?ParentId=" + lib)[0].ID
	albums := items("/Items?ParentId=" + artist)
	if len(albums) != 1 || albums[0].Name != "Hurry Up Tomorrow" {
		t.Fatalf("the artist's albums: %+v, want Hurry Up Tomorrow", albums)
	}
	tracks := items("/Items?ParentId=" + albums[0].ID + "&SortBy=ParentIndexNumber,IndexNumber,SortName")
	want := []musicItem{
		{Name: "Wake Me Up", Type: "Audio", MediaType: "Audio", Album: "Hurry Up Tomorrow", AlbumArtist: "The Weeknd", Artists: []string{"The Weeknd"}, IndexNumber: n(1), ParentIndexNumber: n(1), HasLyrics: true},
		{Name: "Timeless", Type: "Audio", MediaType: "Audio", Album: "Hurry Up Tomorrow", AlbumArtist: "The Weeknd", Artists: []string{"The Weeknd", "Playboi Carti"}, IndexNumber: n(2), ParentIndexNumber: n(2)},
	}
	if diff := cmp.Diff(want, tracks, cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".ID" }, cmp.Ignore())); diff != "" {
		t.Errorf("the album's tracks (-want +got):\n%s", diff)
	}
}

// aTrack is a library holding one FLAC track, on disk, and a profile to play it.
func aTrack(t *testing.T) (*store.Store, domain.Profile, uuid.UUID) {
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
	if err := os.MkdirAll(filepath.Join(root, "Album"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Album", "01.flac"), []byte("fLaC and its frames"), 0o600); err != nil {
		t.Fatal(err)
	}
	lib, err := st.AddLibrary(ctx, "Music", domain.LibraryMusic, root, "")
	if err != nil {
		t.Fatal(err)
	}
	// Four minutes at 900 kbps, as the scan read it; its bytes here are fewer.
	part := store.Part{RelPath: "Album/01.flac", Size: 27_000_000, ModTime: time.Unix(0, 0), Facts: &domain.Facts{
		Size: 27_000_000, Duration: 4 * time.Minute, Container: "flac",
		Streams: []domain.Stream{{Index: 0, Kind: domain.StreamAudio, Codec: "flac", Channels: 2}},
	}}
	if _, err := st.SaveMusicFolder(ctx, lib.ID, "Album", []byte("v"), []store.Album{{
		Title: "Album", Artist: "Artist", Folder: "Album",
		Tracks: []store.Track{{Title: "One", Number: 1, Folder: "Album", Version: store.Version{ContentKey: []byte("one"), Parts: []store.Part{part}}}},
	}}); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	artists, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(artists) != 1 {
		t.Fatal(artists, err)
	}
	albums, err := st.Albums(ctx, ada.ID, artists[0])
	if err != nil || len(albums) != 1 {
		t.Fatal(albums, err)
	}
	tracks, err := st.Tracks(ctx, ada.ID, albums[0].ID)
	if err != nil || len(tracks) != 1 {
		t.Fatal(tracks, err)
	}
	return st, ada, tracks[0].ID
}

// Finamp plays a track at /Audio/{id}/universal, naming what it plays: the FLAC as it is where it
// plays FLAC, and, under a limit the FLAC is over, the HLS of AAC photon makes of it, which it is
// sent to.
func TestAMusicAppPlaysATrackAtItsUniversalAddress(t *testing.T) {
	st, ada, track := aTrack(t)
	plays, remuxes := newFakePlaybacks(), newFakeRemuxes()
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Sent: playback.NewSent(), Network: st,
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Parts: library.Parts{Places: st}, Playbacks: plays, Watching: st, Preferences: st,
		HLS: remuxes, Placer: alone(remuxes), Owners: noOwners{}, Plans: newPlans(), Signer: playback.NewSigner([]byte("key")),
		Themes: st,
	})
	const finamp = `MediaBrowser Client="Finamp", Device="iPhone", DeviceId="1", Version="0.9", Token="pst_ada"`
	universal := "/Audio/" + guid(track) + "/universal?UserId=" + guid(ada.ID) + "&Container=opus,mp3|mp3,aac,m4a|aac,flac&TranscodingContainer=ts&TranscodingProtocol=hls&AudioCodec=aac"
	w := serve(api, http.MethodGet, universal+"&MaxStreamingBitrate=999999999", finamp, "")
	if w.Code != http.StatusOK || w.Body.String() != "fLaC and its frames" {
		t.Fatalf("FLAC it plays: %d %q, want the file as it is", w.Code, w.Body)
	}
	w = serve(api, http.MethodGet, universal+"&MaxStreamingBitrate=320000", finamp, "")
	to := w.Header().Get("Location")
	if w.Code != http.StatusFound || !strings.HasPrefix(to, "/videos/"+guid(track)+"/master.m3u8?") {
		t.Fatalf("FLAC over its limit: %d to %q, want sent to HLS", w.Code, to)
	}
	planned := plannedFor(t, api, to)
	if planned.Video != nil || planned.Audio == nil || planned.Audio.Encode == nil || planned.Audio.Encode.Codec != "aac" ||
		planned.Audio.Encode.BitrateKbps > 320 || planned.Segments != domain.SegmentsFMP4 {
		t.Errorf("the HLS planned: video %+v, audio %+v, in %s; want AAC alone within 320 kbps in fragmented MP4", planned.Video, planned.Audio, planned.Segments)
	}
}
