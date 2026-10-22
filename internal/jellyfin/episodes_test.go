//go:build integration

package jellyfin

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// Jellyfin's Episodes tab pages a show library's episodes across its shows, those that aired the
// same day in their shows' order.
func TestAnAppPagesALibrarysEpisodes(t *testing.T) {
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
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films", ""); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	aired := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	for show, episodes := range map[string][]int{"Severance": {1, 2}, "Andor": {1}} {
		var eps []store.Episode
		meta := map[int]domain.Metadata{}
		for _, n := range episodes {
			file := fmt.Sprintf("S01E0%d.mkv", n)
			eps = append(eps, store.Episode{Season: 1, Episodes: []int{n}, Title: file, Folder: show, ByNumber: true, Versions: []store.Version{{
				ContentKey: []byte(show + file), Parts: []store.Part{{RelPath: show + "/" + file, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}},
			}}})
			meta[n] = domain.Metadata{Title: fmt.Sprintf("%s %d", show, n), ReleaseDate: aired}
		}
		if _, err := st.SaveShowFolder(ctx, tv.ID, show, []byte("v"), store.Show{Title: show, Folder: show}, eps, nil); err != nil {
			t.Fatal(err)
		}
		shows, _, err := st.Wall(ctx, []uuid.UUID{tv.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortAdded, Order: domain.Descending, Limit: 1})
		if err != nil || len(shows) != 1 {
			t.Fatal(shows, err)
		}
		if err := st.SaveIdentity(ctx, shows[0], domain.SourceTMDB, domain.Metadata{Title: show}, map[int]domain.SeasonMetadata{1: {Episodes: meta}}); err != nil {
			t.Fatal(err)
		}
	}
	api := New(log, uuid.NewV7().String(), func() string { return "Den" }, Services{RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{}, Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, Playlists: st})
	names := func(target string) []string {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
		var got struct {
			Items []struct {
				Name, Type, SeriesName string
				IndexNumber            int
			}
			TotalRecordCount int
		}
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", target, w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, it := range got.Items {
			if it.Type != "Episode" || it.SeriesName == "" {
				t.Errorf("%s: %+v, want an episode of its show", target, it)
			}
			out = append(out, it.Name)
		}
		if got.TotalRecordCount != len(out) {
			t.Errorf("%s: %d items of %d", target, len(out), got.TotalRecordCount)
		}
		return out
	}
	lib := "/Items?ParentId=" + tv.ID.String() + "&IncludeItemTypes=Episode&Recursive=true"
	for target, want := range map[string][]string{
		// The modern web app's default, and the legacy one's newest first.
		lib + "&SortBy=SeriesSortName&SortOrder=Ascending":                        {"Andor 1", "Severance 1", "Severance 2"},
		lib + "&SortBy=PremiereDate,SeriesSortName,SortName&SortOrder=Descending": {"Severance 2", "Severance 1", "Andor 1"},
		// Every library's, where an app names none.
		"/Items?IncludeItemTypes=Episode&Recursive=true&SortBy=SortName": {"Andor 1", "Severance 1", "Severance 2"},
	} {
		if got := names(target); !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", target, got, want)
		}
	}

	// A season asked for by id, as a box set's member is listed, is numbered and placed under its
	// show as Jellyfin's own are.
	shows, _, err := st.Wall(ctx, []uuid.UUID{tv.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 10})
	if err != nil || len(shows) != 2 {
		t.Fatal(shows, err)
	}
	seasons, err := st.Seasons(ctx, ada.ID, shows[1])
	if err != nil || len(seasons) != 1 {
		t.Fatal(seasons, err)
	}
	w := serve(api, http.MethodGet, "/Items?Ids="+seasons[0].ID.String(), `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
	var got struct {
		Items []struct {
			Type, SeriesName  string
			SeriesID          string `json:"SeriesId"`
			ParentID          string `json:"ParentId"`
			IndexNumber       int
			ParentIndexNumber *int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Items) != 1 {
		t.Fatal(w.Body, err)
	}
	if it := got.Items[0]; it.Type != "Season" || it.SeriesName != "Severance" || it.SeriesID != it.ParentID || it.SeriesID == "" ||
		it.IndexNumber != 1 || it.ParentIndexNumber != nil {
		t.Errorf("season = %+v, want season 1 of Severance, under it", it)
	}
}
