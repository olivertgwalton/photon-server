package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeEditing has one film, "heat" from 1995, and keeps what is done to it.
// bare is a title with nothing on disk.
var bare = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000b0")

type fakeEditing struct {
	edited    domain.Metadata
	reset     []domain.Field
	pinned    string
	order     domain.EpisodeOrder
	mode      domain.RefreshMode
	analysed  uuid.UUID
	unmatched uuid.UUID
	split     uuid.UUID
	locale    domain.Locale
	// files are films' own; forgot is the title forgotten once they were deleted.
	files  []store.LibraryFile
	forgot uuid.UUID
	marked []domain.Marker
	absent []domain.MarkerAbsent
	chosen map[domain.ArtworkKind]uuid.UUID
}

func (f *fakeEditing) EditMetadata(_ context.Context, id uuid.UUID, m domain.Metadata) error {
	if id != films {
		return store.ErrNotFound
	}
	f.edited = m
	return nil
}

func (f *fakeEditing) ResetEdits(_ context.Context, _ uuid.UUID, fields []domain.Field) error {
	f.reset = fields
	return nil
}

func (f *fakeEditing) PinMatch(_ context.Context, id uuid.UUID, p domain.Provider, v string) error {
	if id != films {
		return store.ErrNotFound
	}
	f.pinned = string(p) + "/" + v
	return nil
}

func (f *fakeEditing) SetEpisodeOrder(_ context.Context, id uuid.UUID, order domain.EpisodeOrder) error {
	if id != films {
		return store.ErrNotFound
	}
	f.order = order
	return nil
}

func (f *fakeEditing) Refresh(_ context.Context, id uuid.UUID, mode domain.RefreshMode) error {
	if id != films {
		return store.ErrNotFound
	}
	f.mode = mode
	return nil
}

func (f *fakeEditing) TitleFiles(_ context.Context, id uuid.UUID) ([]store.LibraryFile, error) {
	switch id {
	case films:
		return f.files, nil
	case bare:
		return nil, store.ErrDeletionOff
	}
	return nil, store.ErrNotFound
}

func (f *fakeEditing) ForgetTitle(_ context.Context, id uuid.UUID) error {
	f.forgot = id
	return nil
}

func (f *fakeEditing) SetTitleLocale(_ context.Context, id uuid.UUID, loc domain.Locale) error {
	if id != films {
		return store.ErrNotFound
	}
	f.locale = loc
	return nil
}

func (f *fakeEditing) SplitTitle(_ context.Context, id uuid.UUID) error {
	switch id {
	case films:
		f.split = id
		return nil
	case bare:
		return store.ErrOneCopy
	}
	return store.ErrNotFound
}

func (f *fakeEditing) Unmatch(_ context.Context, id uuid.UUID) error {
	if id != films {
		return store.ErrNotFound
	}
	f.unmatched = id
	return nil
}

func (f *fakeEditing) AnalyseTitle(_ context.Context, id uuid.UUID) error {
	switch id {
	case films:
		f.analysed = id
		return nil
	case bare:
		return store.ErrNothingOnDisk
	}
	return store.ErrNotFound
}

func (f *fakeEditing) SetMarkers(_ context.Context, id uuid.UUID, markers []domain.Marker, absent []domain.MarkerAbsent) error {
	if id != films {
		return store.ErrNotFound
	}
	if len(markers) > 1 && markers[0].Kind == markers[1].Kind {
		return store.ErrMarkerRepeated
	}
	f.marked, f.absent = markers, absent
	return nil
}

// poster is the one picture fakeEditing's film has to choose from.
var poster = uuid.NewV7()

func (f *fakeEditing) ArtworkCandidates(_ context.Context, id uuid.UUID, kind domain.ArtworkKind) ([]store.ArtworkCandidate, error) {
	if id != films {
		return nil, store.ErrNotFound
	}
	if kind != domain.ArtworkPoster {
		return nil, nil
	}
	return []store.ArtworkCandidate{{ID: poster, Source: domain.SourceTMDB, Language: "en", Width: 2000, Height: 3000, Chosen: f.chosen[kind] == poster}}, nil
}

func (f *fakeEditing) ChooseArtwork(_ context.Context, id uuid.UUID, kind domain.ArtworkKind, picture uuid.UUID) error {
	if id != films {
		return store.ErrNotFound
	}
	if kind != domain.ArtworkPoster || picture != poster {
		return store.ErrNotACandidate
	}
	f.chosen = map[domain.ArtworkKind]uuid.UUID{kind: picture}
	return nil
}

func (f *fakeEditing) ForgetArtworkChoice(_ context.Context, id uuid.UUID, kind domain.ArtworkKind) error {
	if id != films {
		return store.ErrNotFound
	}
	delete(f.chosen, kind)
	return nil
}

func (f *fakeEditing) IdentifySubject(_ context.Context, id uuid.UUID) (store.Subject, bool, error) {
	return store.Subject{Kind: domain.ItemMovie, Title: "heat", Year: 1995}, id == films, nil
}

// films searches by name and year.
type filmSearch struct{}

func (filmSearch) Info() provider.Info {
	return provider.Info{ID: domain.SourceTMDB, Name: "TMDB", Kinds: []domain.ItemKind{domain.ItemMovie}}
}

func (filmSearch) Candidates(_ context.Context, _ domain.Locale, _ domain.ItemKind, title string, year int) ([]domain.Candidate, error) {
	return []domain.Candidate{{ID: "949", Title: title + " asked", Year: year, Poster: "https://image.tmdb.org/t/p/original/heat.jpg"}}, nil
}

func TestAnAdminFixesATitle(t *testing.T) {
	e, told := &fakeEditing{}, &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Editing: e, Providers: provider.NewRegistry(nil, filmSearch{}), Events: told})
	base := "/api/v1/admin/titles/" + films.String()
	copyBase := "/api/v1/admin/versions/" + films.String() + "/markers"
	for _, tc := range []struct {
		token, method, target, body string
		want                        int
		has                         string
	}{
		{memberToken, http.MethodPatch, base, `{"title": "Mine"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPatch, base, `{"title": "Heat", "release_date": "1995-12-15", "locked": ["overview"]}`, http.StatusNoContent, ""},
		{goodToken, http.MethodPatch, base, `{"release_date": "15/12/1995"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPatch, base, `{"locked": ["colour"]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodDelete, base + "/edits?field=title&field=overview", "", http.StatusAccepted, ""},
		{goodToken, http.MethodGet, base + "/candidates?provider=tmdb", "", http.StatusOK, `"id":"949","title":"heat asked","year":1995,"poster"`},
		{goodToken, http.MethodGet, base + "/candidates?provider=tmdb&title=Heat%201986", "", http.StatusOK, `"title":"Heat 1986 asked"`},
		{goodToken, http.MethodGet, base + "/candidates?provider=mdblist", "", http.StatusBadRequest, ""},
		{goodToken, http.MethodGet, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/candidates?provider=tmdb", "", http.StatusNotFound, ""},
		{goodToken, http.MethodPut, base + "/match", `{"provider": "tmdb", "id": "949"}`, http.StatusAccepted, ""},
		{goodToken, http.MethodPut, base + "/match", `{"provider": "netflix", "id": "1"}`, http.StatusBadRequest, ""},
		{memberToken, http.MethodDelete, base + "/match", "", http.StatusForbidden, ""},
		{goodToken, http.MethodDelete, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/match", "", http.StatusNotFound, "no film or show"},
		{goodToken, http.MethodDelete, base + "/match", "", http.StatusNoContent, ""},
		{memberToken, http.MethodPut, base + "/locale", `{"metadata_language": "fr"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPut, base + "/locale", `{"certification_country": "Gaul"}`, http.StatusBadRequest, "certification_country"},
		{goodToken, http.MethodPut, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/locale", `{}`, http.StatusNotFound, "no film or show"},
		{goodToken, http.MethodPut, base + "/locale", `{"metadata_language": "fr-fr", "certification_country": "fr"}`, http.StatusAccepted, ""},
		{memberToken, http.MethodPost, base + "/split", "", http.StatusForbidden, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/titles/" + bare.String() + "/split", "", http.StatusConflict, "one copy"},
		{goodToken, http.MethodPost, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/split", "", http.StatusNotFound, "no film"},
		{goodToken, http.MethodPost, base + "/split", "", http.StatusNoContent, ""},
		{goodToken, http.MethodPut, base + "/episode-order", `{"order": "dvd"}`, http.StatusAccepted, ""},
		{goodToken, http.MethodPut, base + "/episode-order", `{"order": "production"}`, http.StatusBadRequest, ""},
		{memberToken, http.MethodPost, base + "/refresh", `{"mode": "all"}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPost, base + "/refresh", `{"mode": "images"}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/refresh", `{"mode": "all"}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPost, base + "/refresh", `{"mode": "all"}`, http.StatusAccepted, ""},
		{memberToken, http.MethodPost, base + "/analysis", "", http.StatusForbidden, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/titles/" + bare.String() + "/analysis", "", http.StatusConflict, "nothing of it is on disk"},
		{goodToken, http.MethodPost, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/analysis", "", http.StatusNotFound, ""},
		{goodToken, http.MethodPost, base + "/analysis", "", http.StatusAccepted, ""},
		{memberToken, http.MethodGet, base + "/artwork/candidates?kind=poster", "", http.StatusForbidden, ""},
		{goodToken, http.MethodGet, base + "/artwork/candidates?kind=disc", "", http.StatusBadRequest, ""},
		{goodToken, http.MethodGet, base + "/artwork/candidates?kind=poster", "", http.StatusOK, `"source":"tmdb","language":"en","width":2000,"height":3000,"chosen":false`},
		{goodToken, http.MethodPut, base + "/artwork/backdrop", `{"id": "` + poster.String() + `"}`, http.StatusBadRequest, "candidates"},
		{goodToken, http.MethodPut, base + "/artwork/disc", `{"id": "` + poster.String() + `"}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPut, "/api/v1/admin/titles/" + uuid.NewV7().String() + "/artwork/poster", `{"id": "` + poster.String() + `"}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPut, base + "/artwork/poster", `{"id": "` + poster.String() + `"}`, http.StatusNoContent, ""},
		{goodToken, http.MethodGet, base + "/artwork/candidates?kind=poster", "", http.StatusOK, `"chosen":true`},
		{goodToken, http.MethodDelete, base + "/artwork/poster", "", http.StatusNoContent, ""},
		{goodToken, http.MethodGet, base + "/artwork/candidates?kind=poster", "", http.StatusOK, `"chosen":false`},
		{memberToken, http.MethodPut, copyBase, `{"markers": []}`, http.StatusForbidden, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "commercial", "start_ms": 0, "end_ms": 1000}]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "intro", "start_ms": 5000, "end_ms": 5000}]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "intro", "start_ms": 0, "end_ms": 1}, {"kind": "intro", "start_ms": 2, "end_ms": 3}]}`, http.StatusBadRequest, "one marker of each kind"},
		{goodToken, http.MethodPut, "/api/v1/admin/versions/" + uuid.NewV7().String() + "/markers", `{"markers": []}`, http.StatusNotFound, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [], "absent": [{"kind": "commercial", "part": 0}]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [], "absent": [{"kind": "intro", "part": -1}]}`, http.StatusBadRequest, ""},
		{goodToken, http.MethodPut, copyBase, `{"markers": [{"kind": "intro", "start_ms": 62000, "end_ms": 121500}], "absent": [{"kind": "recap", "part": 0}]}`, http.StatusNoContent, ""},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.has) {
			t.Errorf("%s %s %s: %d %s, want %d with %s", tc.method, tc.target, tc.body, rec.Code, rec.Body, tc.want, tc.has)
		}
	}
	if e.edited.Title != "Heat" || e.edited.ReleaseDate.Year() != 1995 || len(e.edited.Locked) != 1 || len(e.reset) != 2 || e.pinned != "tmdb/949" || e.order != domain.OrderDVD || e.mode != domain.RefreshAll || e.analysed != films || e.unmatched != films || e.split != films || e.locale != (domain.Locale{Language: "fr-FR", Country: "FR"}) || len(e.marked) != 1 || e.marked[0].EndMS != 121500 ||
		len(e.absent) != 1 || e.absent[0] != (domain.MarkerAbsent{Kind: domain.MarkerRecap}) {
		t.Errorf("done: %+v %v %q", e.edited, e.reset, e.pinned)
	}
	// The edit and the two pictures; a match is told by the job that makes it.
	if got := told.kinds(); len(got) != 3 || got[0] != domain.EventTitleUpdated {
		t.Errorf("told %v, want title.updated thrice", got)
	}
}

func TestAnAdminDeletesATitleFilesFirst(t *testing.T) {
	root := t.TempDir()
	film := filepath.Join(root, "Heat (1995)")
	if err := os.MkdirAll(film, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Heat.mkv", "Heat.en.srt"} {
		if err := os.WriteFile(filepath.Join(film, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := &fakeEditing{files: []store.LibraryFile{
		{Root: root, Rel: "Heat (1995)/Heat.en.srt"},
		{Root: root, Rel: "Heat (1995)/Heat.mkv"},
		// Gone already, as a file deleted by hand is: no reason to stop.
		{Root: root, Rel: "Heat (1995)/Heat.cd2.mkv"},
	}}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Editing: e})
	del := func(token string, id uuid.UUID) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/titles/"+id.String(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := del(memberToken, films); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	if rec := del(goodToken, bare); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "does not allow") {
		t.Errorf("where its library does not allow it: %d %s, want 409 saying so", rec.Code, rec.Body)
	}
	if rec := del(goodToken, uuid.NewV7()); rec.Code != http.StatusNotFound {
		t.Errorf("no title: %d, want 404", rec.Code)
	}

	// A file that will not go stops the delete, and the title is kept.
	if err := os.Chmod(film, 0o555); err != nil {
		t.Fatal(err)
	}
	rec := del(goodToken, films)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "0 of its 3 files were deleted") || e.forgot != (uuid.UUID{}) {
		t.Errorf("a file that would not go: %d %s, forgot %v; want 409 saying so and the title kept", rec.Code, rec.Body, e.forgot)
	}
	if err := os.Chmod(film, 0o755); err != nil {
		t.Fatal(err)
	}

	if rec := del(goodToken, films); rec.Code != http.StatusNoContent || e.forgot != films {
		t.Fatalf("deleting: %d %s, forgot %v; want 204 and the title forgotten", rec.Code, rec.Body, e.forgot)
	}
	if _, err := os.Stat(film); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the film's folder, emptied: %v, want it gone", err)
	}
}
