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
	"syscall"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A folder Windows marks hidden, as a drive's $RECYCLE.BIN is, is hidden as a dot-folder is.
func TestAFolderMarkedHiddenIsHidden(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"Films", "$RECYCLE.BIN"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	p, err := syscall.UTF16PtrFromString(filepath.Join(root, "$RECYCLE.BIN"))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}})
	list := func(query url.Values) []string {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/folders?"+query.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+goodToken)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		var got folderListJSON
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
		}
		var names []string
		for _, f := range got.Items {
			names = append(names, f.Name)
		}
		return names
	}
	if got := list(url.Values{"path": {root}}); !slices.Equal(got, []string{"Films"}) {
		t.Errorf("listing = %v, want Films alone", got)
	}
	if got := list(url.Values{"path": {root}, "hidden": {"show"}}); !slices.Contains(got, "$RECYCLE.BIN") {
		t.Errorf("showing hidden = %v, want $RECYCLE.BIN among them", got)
	}
}
