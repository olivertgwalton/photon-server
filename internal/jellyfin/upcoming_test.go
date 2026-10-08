//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// An app's upcoming list holds the episodes airing from yesterday, those with no file as virtual
// episodes it can open but not play, as Swiftfin and Jellyfin's web app show missing ones.
func TestAnAppListsUpcomingEpisodes(t *testing.T) {
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
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	ep := store.Episode{Season: 1, Episodes: []int{1}, Title: "S01E01.mkv", Folder: "Severance", ByNumber: true, Copies: []store.Copy{{
		ContentKey: []byte("1"), Parts: []store.Part{{RelPath: "S01E01.mkv", Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
	}}}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Severance", []byte("v"), store.Show{Title: "Severance", Folder: "Severance"}, []store.Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	shows, _, err := st.Wall(ctx, []uuid.UUID{tv.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 1})
	if err != nil || len(shows) != 1 {
		t.Fatal(shows, err)
	}
	show := shows[0].ID
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if err := st.SaveIdentity(ctx, show, domain.SourceTMDB, domain.Metadata{}, map[int]domain.SeasonMetadata{1: {Episodes: map[int]domain.Metadata{
		1: {Title: "Good News About Hell", ReleaseDate: today},
		2: {Title: "Half Loop", ReleaseDate: today.AddDate(0, 0, 7)},
	}}}); err != nil {
		t.Fatal(err)
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{Auth: profiles{"pst_ada": ada}, Catalogue: st, Playlists: st})
	get := func(target string, into any) int {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code
	}
	type episode struct {
		ID, Name, LocationType, SeriesName string
		IndexNumber                        int
		MediaSources                       []any
	}
	var upcoming struct {
		Items            []episode
		TotalRecordCount int
	}
	get("/Shows/Upcoming?userId="+guid(ada.ID)+"&limit=25&fields=AirTime,MediaSources", &upcoming)
	if len(upcoming.Items) != 2 || upcoming.TotalRecordCount != 2 {
		t.Fatalf("upcoming = %+v, want both episodes", upcoming)
	}
	here, coming := upcoming.Items[0], upcoming.Items[1]
	if here.Name != "Good News About Hell" || here.LocationType != "FileSystem" || len(here.MediaSources) != 1 {
		t.Errorf("the episode here = %+v, want it with its copy", here)
	}
	if coming.Name != "Half Loop" || coming.LocationType != "Virtual" || coming.SeriesName != "Severance" || coming.IndexNumber != 2 || coming.MediaSources != nil {
		t.Errorf("the episode to come = %+v, want it virtual, of Severance, with no copy", coming)
	}
	var opened episode
	if code := get("/Users/"+guid(ada.ID)+"/Items/"+coming.ID, &opened); code != http.StatusOK || opened.Name != "Half Loop" || opened.LocationType != "Virtual" {
		t.Errorf("opening the episode to come: %d %+v, want it, virtual", code, opened)
	}
	for parent, want := range map[string]int{guid(tv.ID): 2, guid(films.ID): 0, guid(show): 0} {
		var narrowed struct{ Items []episode }
		get("/Shows/Upcoming?parentId="+parent, &narrowed)
		if len(narrowed.Items) != want {
			t.Errorf("upcoming under %s = %+v, want %d: a library's own alone", parent, narrowed.Items, want)
		}
	}
}
