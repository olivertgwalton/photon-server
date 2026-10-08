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

// An app finds someone by name, opens them by the id it found, and lists their films and shows as
// Jellyfin's web app does; the kid's list holds only what the kid may see.
func TestAnAppOpensSomeoneAndTheirWork(t *testing.T) {
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
	films, err := st.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	tv, err := st.AddLibrary(ctx, "TV", domain.LibraryShows, "/srv/tv")
	if err != nil {
		t.Fatal(err)
	}
	copies := func(rel string) []store.Copy {
		return []store.Copy{{ContentKey: []byte(rel), Parts: []store.Part{{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &domain.Facts{Duration: time.Hour}}}}}
	}
	for _, f := range []string{"Alien", "Heat"} {
		if _, err := st.SaveFolder(ctx, films.ID, f, []byte("v"), []store.Film{{Title: f, Folder: f, Copies: copies(f + ".mkv")}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	ep := store.Episode{Season: 1, Episodes: []int{1}, Title: "Pilot", Folder: "Show", ByNumber: true, Copies: copies("S01E01.mkv")}
	if _, err := st.SaveShowFolder(ctx, tv.ID, "Show", []byte("v"), store.Show{Title: "Show", Folder: "Show"}, []store.Episode{ep}, nil); err != nil {
		t.Fatal(err)
	}
	ada, err := st.AddProfile(ctx, "Ada", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccess(ctx, kid.ID, store.ProfileAccess{Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	titles, _, err := st.Wall(ctx, []uuid.UUID{films.ID, tv.ID}, store.WallPage{Profile: ada.ID, Sort: domain.SortTitle, Limit: 3})
	if err != nil || len(titles) != 3 {
		t.Fatal(titles, err)
	}
	weaver := domain.Credit{Name: "Sigourney Weaver", IDs: map[domain.Provider]string{domain.ProviderTMDB: "10205"}, Kind: domain.CreditActor, Role: "Ripley"}
	for _, title := range []store.Card{titles[0], titles[2]} {
		if err := st.SaveIdentity(ctx, title.ID, domain.SourceTMDB, domain.Metadata{Title: title.Title, Credits: []domain.Credit{weaver}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := st.Title(ctx, ada.ID, titles[0].ID)
	if err != nil || len(page.Credits) != 1 {
		t.Fatal(page.Credits, err)
	}
	her := page.Credits[0].PersonID
	born := time.Date(1949, 10, 8, 0, 0, 0, 0, time.UTC)
	if err := st.DescribePerson(ctx, her, domain.Person{Name: "Sigourney Weaver", Biography: "An actor.", Born: born, Birthplace: "New York City"}); err != nil {
		t.Fatal(err)
	}
	api := New(log, domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{Auth: profiles{"pst_ada": ada, "pst_kid": kid}, Catalogue: st})
	get := func(token, target string, into any) int {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="`+token+`"`, "")
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code
	}
	type found struct {
		ID, Name, Type, Overview, PremiereDate string
		ProductionLocations                    []string
		ProviderIDs                            map[string]string
	}
	var people struct {
		Items            []found
		TotalRecordCount int
	}
	get("pst_ada", "/Persons?searchTerm=weav&limit=10", &people)
	if len(people.Items) != 1 || people.TotalRecordCount != 1 || people.Items[0].ID != guid(her) || people.Items[0].Type != "Person" {
		t.Fatalf("people named weav: %+v", people)
	}
	var one found
	if code := get("pst_ada", "/Users/"+guid(ada.ID)+"/Items/"+guid(her), &one); code != http.StatusOK || one.Type != "Person" || one.Overview != "An actor." ||
		one.PremiereDate[:10] != "1949-10-08" || len(one.ProductionLocations) != 1 || one.ProviderIDs["Tmdb"] != "10205" {
		t.Errorf("her: %d %+v", code, one)
	}
	work := func(token, people string) []string {
		t.Helper()
		var list struct{ Items []found }
		get(token, "/Items?personIds="+people+"&recursive=true&includeItemTypes=Movie,Series&sortBy=SortName", &list)
		var names []string
		for _, it := range list.Items {
			names = append(names, it.Name)
		}
		return names
	}
	if got := work("pst_ada", guid(her)); len(got) != 2 || got[0] != "Alien" || got[1] != "Show" {
		t.Errorf("her work: %v, want Alien and Show", got)
	}
	if got := work("pst_kid", guid(her)); len(got) != 1 || got[0] != "Alien" {
		t.Errorf("her work, for the kid: %v, want Alien alone", got)
	}
	if got := work("pst_ada", "nobody"); len(got) != 0 {
		t.Errorf("the work of an id that is none: %v, want nothing", got)
	}
}
