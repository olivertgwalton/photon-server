//go:build integration

package jellyfin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/analysis"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// An app reads a film's chapters, to list them and to seek by them: on the film itself, and in a
// list where it asks for them; and fetches a chapter's picture by the tag the chapter gives.
func TestAnAppReadsAFilmsChapters(t *testing.T) {
	st, ada, heat, _ := aFilm(t)
	versions, err := st.Versions(t.Context(), []uuid.UUID{heat})
	if err != nil {
		t.Fatal(err)
	}
	// The second chapter is pictured; the first not yet.
	part := versions[heat][0].Files[0].ID
	if err := st.SavePreviews(t.Context(), part, []int{1}, nil); err != nil {
		t.Fatal(err)
	}
	dir, err := blob.OpenDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	if err := dir.Put(t.Context(), path.Join(part.String(), "chapters", "1.jpg"), strings.NewReader("chapter 1")); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{ID: uuid.NewV7().String(), Name: "Den"}, Services{
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st, PreviewFiles: analysis.NewPreviews(dir),
	})
	chapters := func(target string) []map[string]any {
		t.Helper()
		w := serve(api, http.MethodGet, target, `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
		var it struct {
			Items    []map[string]any
			Chapters []map[string]any
		}
		if err := json.Unmarshal(w.Body.Bytes(), &it); w.Code != http.StatusOK || err != nil {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		if it.Items == nil {
			return it.Chapters
		}
		raw, _ := it.Items[0]["Chapters"].([]any)
		var out []map[string]any
		for _, c := range raw {
			out = append(out, c.(map[string]any))
		}
		return out
	}
	for _, target := range []string{"/Items/" + guid(heat), "/Items?recursive=true&includeItemTypes=Movie&fields=Chapters"} {
		got := chapters(target)
		if len(got) != 2 {
			t.Fatalf("%s: chapters %v, want two", target, got)
		}
		for _, c := range got {
			requireKeys(t, "ChapterInfo", c, "StartPositionTicks", "ImageDateModified")
		}
		if got[0]["Name"] != "The Bank" || got[0]["StartPositionTicks"] != 0.0 || got[0]["ImageTag"] != nil {
			t.Errorf("%s: the first chapter %v, want The Bank from the start, no picture", target, got[0])
		}
		if got[1]["StartPositionTicks"] != float64((40*time.Minute).Milliseconds()*ticksPerMS) || got[1]["ImageTag"] != chapterTag(part, 1) {
			t.Errorf("%s: the second chapter %v, want 40 minutes in with its picture", target, got[1])
		}
	}
	if got := chapters("/Items?recursive=true&includeItemTypes=Movie&fields=MediaSources"); got != nil {
		t.Errorf("a list that did not ask for chapters: %v", got)
	}

	// Its picture is fetched by its tag, without a token, as Jellyfin's web app sets it on a card.
	w := serve(api, http.MethodGet, "/Items/"+guid(heat)+"/Images/Chapter/1?maxWidth=400&tag="+chapterTag(part, 1), "", "")
	if w.Code != http.StatusOK || w.Body.String() != "chapter 1" || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("the second chapter's picture: %d %s %q", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	for _, tag := range []string{chapterTag(part, 0), "nonsense", guid(part)} {
		if w := serve(api, http.MethodGet, "/Items/"+guid(heat)+"/Images/Chapter/0?tag="+tag, "", ""); w.Code != http.StatusNotFound {
			t.Errorf("a chapter picture tagged %q: %d, want 404", tag, w.Code)
		}
	}
}
