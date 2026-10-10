//go:build integration

package jellyfin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/remote"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// streaming gives the titles it streams a copy as they are opened or played, as a remote library's
// provider does, and has nothing for the rest.
type streaming struct {
	st      *store.Store
	lib     uuid.UUID
	streams map[uuid.UUID]bool
}

func (s streaming) Ensure(ctx context.Context, item uuid.UUID) error {
	if !s.streams[item] {
		return remote.ErrNoCopy
	}
	facts := domain.Facts{Container: "matroska,webm", Size: 1 << 30, Duration: 2 * time.Hour, Streams: []domain.Stream{
		{Index: 0, Kind: domain.StreamVideo, Codec: "h264", Width: 1920, Height: 1080, Range: domain.RangeSDR},
	}}
	return s.st.SaveCopy(ctx, s.lib, item, store.Copy{
		ContentKey: item[:], Parts: []store.Part{{RelPath: "k/Heat.mkv", Size: facts.Size, ModTime: time.Unix(0, 0), Facts: &facts}},
	})
}

// discovered is what a remote library's search finds, whatever is searched for.
type discovered []store.Discovery

func (d discovered) Find(context.Context, uuid.UUID, string, []domain.ItemKind) ([]store.Discovery, error) {
	return d, nil
}

// A remote film with no copy yet, or one a search found, is listed with one media source under its
// own id, as Infuse needs one to play an item, to show it among what it searched for, and not to
// fail the page it is on. Played by that id, it is given its copy and played; one its provider has
// nothing for is not found.
func TestARemoteFilmIsListedWithACopyToBeFetchedAsItIsPlayed(t *testing.T) {
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
	aio := domain.PluginSource("aio")
	lib, err := st.AddRemoteLibrary(ctx, "Streamed", domain.LibraryMovies, store.Remote{ListSource: aio, ListID: "movie/top", DiscoverSource: domain.SourceTMDB, StreamSource: aio})
	if err != nil {
		t.Fatal(err)
	}
	listed := []domain.Listed{
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}, Title: "Heat"},
		{Kind: domain.ItemMovie, IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0499549"}, Title: "Avatar"},
	}
	if _, err := st.SaveListed(ctx, lib.ID, domain.ItemMovie, listed); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	cards, _, err := st.Wall(ctx, []uuid.UUID{lib.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Order: domain.Ascending, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		ids[c.Title] = c.ID
	}
	found, err := st.SaveDiscoveries(ctx, lib.ID, domain.ItemMovie, domain.ProviderTMDB, []domain.Candidate{{ID: "27205", Title: "Inception", Year: 2010}})
	if err != nil || len(found) != 1 {
		t.Fatalf("found %+v, %v", found, err)
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{
		Copies: streaming{st, lib.ID, map[uuid.UUID]bool{ids["Heat"]: true}}, Discover: discovered(found),
		Sent: playback.NewSent(), Network: st, Auth: profiles{"pst_ada": ada}, Catalogue: st, Playing: st,
		Parts: library.Parts{Places: st}, Playbacks: newFakePlaybacks(), Watching: st, Preferences: st, Placer: alone(nil),
	})
	const infuse = `MediaBrowser Client="Infuse-Direct", Device="iPhone", DeviceId="E0BE", Version="8.5.6", Token="pst_ada"`
	type source struct{ ID string }
	var page struct {
		Items []struct {
			Name         string
			MediaSources []source
		}
	}
	w := serve(api, http.MethodGet, "/Items?ParentId="+guid(lib.ID)+"&Fields=MediaSources", infuse, "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 2 {
		t.Fatalf("the library: %d %s", w.Code, w.Body)
	}
	for _, it := range page.Items {
		if len(it.MediaSources) != 1 || it.MediaSources[0].ID != guid(ids[it.Name]) {
			t.Errorf("%s is listed with %+v, want one source under its own id", it.Name, it.MediaSources)
		}
	}
	w = serve(api, http.MethodGet, "/Items?searchTerm=Inception&Recursive=true&Fields=MediaSources", infuse, "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || len(page.Items[0].MediaSources) != 1 ||
		page.Items[0].MediaSources[0].ID != guid(found[0].ID) {
		t.Errorf("Inception, which a search found: %d %s, want it with its placeholder", w.Code, w.Body)
	}

	var info struct{ MediaSources []source }
	w = serve(api, http.MethodPost, "/Items/"+guid(ids["Heat"])+"/PlaybackInfo", infuse, `{"MediaSourceId": "`+guid(ids["Heat"])+`"}`)
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != http.StatusOK || len(info.MediaSources) != 1 || info.MediaSources[0].ID == guid(ids["Heat"]) {
		t.Errorf("Heat played by its placeholder: %d %s, want its copy, fetched", w.Code, w.Body)
	}
	if w := serve(api, http.MethodPost, "/Items/"+guid(ids["Avatar"])+"/PlaybackInfo", infuse, `{"MediaSourceId": "`+guid(ids["Avatar"])+`"}`); w.Code != http.StatusNotFound {
		t.Errorf("Avatar, which its provider has nothing of: %d, want 404", w.Code)
	}
	var avatar struct{ MediaSources []source }
	w = serve(api, http.MethodGet, "/Items/"+guid(ids["Avatar"]), infuse, "")
	if err := json.Unmarshal(w.Body.Bytes(), &avatar); err != nil || len(avatar.MediaSources) != 1 {
		t.Errorf("Avatar's page: %d %s, want it with its placeholder", w.Code, w.Body)
	}
}
