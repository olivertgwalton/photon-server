package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnAdminBrowsesTheServersFolders(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"Films", "Shows", ".cache"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "Films"), filepath.Join(root, "Movies")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "notes.txt"), filepath.Join(root, "Notes")); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}})
	get := func(token string, query url.Values) (*httptest.ResponseRecorder, folderListJSON) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/folders?"+query.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var got folderListJSON
		if rec.Code == http.StatusOK {
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
		}
		return rec, got
	}
	names := func(l folderListJSON) []string {
		var out []string
		for _, f := range l.Items {
			out = append(out, f.Name)
		}
		return out
	}

	if rec, _ := get(memberToken, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	if _, roots := get(goodToken, nil); !slices.Contains(names(roots), filepath.VolumeName(root)+string(filepath.Separator)) {
		t.Errorf("roots = %v, want the one %s is on among them", roots.Items, root)
	}
	_, got := get(goodToken, url.Values{"path": {root + "/"}})
	if want := []string{"Films", "Movies", "Shows"}; !slices.Equal(names(got), want) || got.Path != root || got.Parent != filepath.Dir(root) {
		t.Errorf("listing = %+v, want %v in %s", got, want, root)
	}
	if got.Items[0].Path != filepath.Join(root, "Films") {
		t.Errorf("Films is at %q", got.Items[0].Path)
	}
	if _, got := get(goodToken, url.Values{"path": {root}, "hidden": {"show"}}); !slices.Contains(names(got), ".cache") {
		t.Errorf("showing hidden = %v, want .cache among them", names(got))
	}
	for _, q := range []url.Values{
		{"path": {"media/Films"}},
		{"path": {filepath.Join(root, "notes.txt")}},
		{"path": {filepath.Join(root, "missing")}},
		{"path": {root}, "hidden": {"maybe"}},
	} {
		if rec, _ := get(goodToken, q); rec.Code != http.StatusBadRequest {
			t.Errorf("%v: %d, want 400", q, rec.Code)
		}
	}

	many := t.TempDir()
	for i := range maxFolders + 1 {
		if err := os.Mkdir(filepath.Join(many, strconv.Itoa(i)), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if _, got := get(goodToken, url.Values{"path": {many}}); len(got.Items) != maxFolders || !got.Truncated {
		t.Errorf("a folder of %d: %d listed, truncated %v", maxFolders+1, len(got.Items), got.Truncated)
	}
}
