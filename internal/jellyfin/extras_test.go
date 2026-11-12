//go:build integration

package jellyfin

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// A film's trailer is its local trailer to an app, and its featurette a special feature: the film
// says how many of each it has, as Jellyfin's web app shows its trailer button and features row
// by, and the trailer plays as the film does. A theme video is on neither list, but is the film's
// theme video. The trailer is pictured by its first chapter picture, as photon pictures it.
func TestAnAppListsAndPlaysAFilmsExtras(t *testing.T) {
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
	files := map[string]string{
		"Heat/Heat.mkv": "the film", "Heat/Heat-trailer.mkv": "the trailer",
		"Heat/Heat-featurette.mkv": "the featurette", "Heat/backdrops/theme.mkv": "the theme",
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, root, "")
	if err != nil {
		t.Fatal(err)
	}
	version := func(rel string) store.Version {
		facts := &domain.Facts{
			Size: int64(len(files[rel])), Duration: 2 * time.Minute, Container: "matroska,webm",
			Streams:  []domain.Stream{{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR}},
			Chapters: []domain.Chapter{{End: 2 * time.Minute}},
		}
		return store.Version{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: facts.Size, ModTime: time.Unix(0, 0), Facts: facts}}}
	}
	owner := store.Owner{Kind: domain.ItemMovie, Folder: "Heat", Title: "Heat"}
	extra := func(kind domain.ExtraKind, title, rel string) store.Extra {
		return store.Extra{Kind: kind, Title: title, Folder: "Heat", Owner: owner, Version: version(rel)}
	}
	film := store.Film{Title: "Heat", Folder: "Heat", Versions: []store.Version{version("Heat/Heat.mkv")}}
	if _, err := st.SaveFolder(ctx, films.ID, "Heat", []byte("v"), []store.Film{film}, []store.Extra{
		extra(domain.ExtraTrailer, "Trailer", "Heat/Heat-trailer.mkv"),
		extra(domain.ExtraFeaturette, "Featurette", "Heat/Heat-featurette.mkv"),
		extra(domain.ExtraThemeVideo, "Theme", "Heat/backdrops/theme.mkv"),
	}); err != nil {
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
	heat := guid(cards[0])
	page, err := st.Title(ctx, ada.ID, cards[0])
	if err != nil || len(page.Extras) != 3 {
		t.Fatal(page.Extras, err)
	}
	var part uuid.UUID
	for _, e := range page.Extras {
		if e.Kind == domain.ExtraTrailer {
			versions, err := st.Versions(ctx, []uuid.UUID{e.ID})
			if err != nil {
				t.Fatal(err)
			}
			part = versions[e.ID][0].Files[0].ID
		}
	}
	if err := st.SavePreviews(ctx, part, []int{0}, nil); err != nil {
		t.Fatal(err)
	}
	dir, err := blob.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	if err := dir.Put(ctx, path.Join(part.String(), "chapters", "0.jpg"), strings.NewReader("a still")); err != nil {
		t.Fatal(err)
	}

	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Sent: playback.NewSent(), Network: st,
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st, Parts: library.Parts{Places: st},
		Playbacks: newFakePlaybacks(), Watching: st, Preferences: st, Placer: alone(nil), PreviewFiles: analysis.NewPreviews(dir),
	})
	const infuse = `MediaBrowser Client="Infuse-Direct", Device="Apple TV", DeviceId="E0BE", Version="8.5.6", Token="pst_ada"`
	get := func(target string, into any) {
		t.Helper()
		w := serve(api, http.MethodGet, target, infuse, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}

	var heatItem map[string]any
	get("/Items/"+heat, &heatItem)
	if heatItem["LocalTrailerCount"] != 1.0 || heatItem["SpecialFeatureCount"] != 1.0 {
		t.Errorf("the film counts %v trailers and %v features, want 1 of each", heatItem["LocalTrailerCount"], heatItem["SpecialFeatureCount"])
	}

	type extraItem struct {
		ID           string            `json:"Id"`
		Name         string            `json:"Name"`
		Type         string            `json:"Type"`
		ExtraType    string            `json:"ExtraType"`
		ImageTags    map[string]string `json:"ImageTags"`
		MediaSources []struct {
			ID string `json:"Id"`
		} `json:"MediaSources"`
	}
	var trailers, features []extraItem
	get("/Users/"+guid(ada.ID)+"/Items/"+heat+"/LocalTrailers", &trailers)
	if len(trailers) != 1 || trailers[0].Name != "Trailer" || trailers[0].Type != "Video" || trailers[0].ExtraType != "Trailer" || len(trailers[0].MediaSources) != 1 {
		t.Fatalf("trailers: %+v", trailers)
	}
	get("/Items/"+heat+"/SpecialFeatures", &features)
	if len(features) != 1 || features[0].Name != "Featurette" || features[0].ExtraType != "Featurette" {
		t.Errorf("special features: %+v", features)
	}

	var themes themeResult
	get("/Items/"+heat+"/ThemeVideos", &themes)
	if len(themes.Items) != 1 || themes.OwnerID != heat || themes.Items[0]["Name"] != "Theme" || themes.Items[0]["ExtraType"] != "ThemeVideo" {
		t.Errorf("theme videos: %+v", themes)
	}
	var all struct {
		ThemeVideosResult themeResult `json:"ThemeVideosResult"`
	}
	get("/Items/"+heat+"/ThemeMedia", &all)
	if len(all.ThemeVideosResult.Items) != 1 || all.ThemeVideosResult.Items[0]["Id"] != themes.Items[0]["Id"] {
		t.Errorf("theme media's videos: %+v", all.ThemeVideosResult)
	}

	trailer := trailers[0]
	if tag := trailer.ImageTags["Primary"]; tag != chapterTag(part, 0) {
		t.Errorf("the trailer's picture: %q, want its chapter's", tag)
	}
	if w := serve(api, http.MethodGet, "/Items/"+trailer.ID+"/Images/Primary?tag="+trailer.ImageTags["Primary"]+"&maxWidth=400", "", ""); w.Code != http.StatusOK || w.Body.String() != "a still" {
		t.Errorf("the trailer's picture: %d %q", w.Code, w.Body)
	}
	if len(features[0].ImageTags) != 0 {
		t.Errorf("the featurette, not yet pictured: %v", features[0].ImageTags)
	}
	var info struct {
		MediaSources []struct {
			ID string `json:"Id"`
		} `json:"MediaSources"`
	}
	if w := serve(api, http.MethodPost, "/Items/"+trailer.ID+"/PlaybackInfo", infuse, `{"UserId":"`+guid(ada.ID)+`"}`); w.Code != http.StatusOK {
		t.Fatalf("the trailer's playback info: %d %s", w.Code, w.Body)
	} else if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || len(info.MediaSources) != 1 || info.MediaSources[0].ID != trailer.MediaSources[0].ID {
		t.Errorf("the trailer's sources: %+v, %v", info, err)
	}
	w := serve(api, http.MethodGet, "/Videos/"+trailer.ID+"/stream?MediaSourceId="+trailer.MediaSources[0].ID+"&Static=true", infuse, "")
	if w.Code != http.StatusOK || w.Body.String() != "the trailer" {
		t.Errorf("the trailer's stream: %d %q", w.Code, w.Body)
	}

	// What is not a title has no extras, and is not refused: Infuse asks of everything it shows.
	var none []extraItem
	get("/Items/"+guid(uuid.NewV7())+"/LocalTrailers", &none)
	if none == nil || len(none) != 0 {
		t.Errorf("an unknown item's trailers: %v, want an empty list", none)
	}
}
