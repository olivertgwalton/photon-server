package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// An admin is answered a gzipped profile go build can read as default.pgo; a member is refused, as
// is a length out of range, and a second profile while one is being taken.
func TestAnAdminTakesACPUProfile(t *testing.T) {
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}})
	get := func(token, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	if rec := get(memberToken, "/api/v1/admin/cpu-profile?seconds=1"); rec.Code != http.StatusForbidden {
		t.Errorf("a member: %d, want 403", rec.Code)
	}
	for _, seconds := range []string{"0", "301", "soon"} {
		if rec := get(goodToken, "/api/v1/admin/cpu-profile?seconds="+seconds); rec.Code != http.StatusBadRequest {
			t.Errorf("%s seconds: %d, want 400", seconds, rec.Code)
		}
	}
	rec := get(goodToken, "/api/v1/admin/cpu-profile?seconds=1")
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x1f, 0x8b}) {
		t.Fatalf("a profile: %d, %d bytes; want 200 and a gzipped profile", rec.Code, rec.Body.Len())
	}
	if err := pprof.StartCPUProfile(io.Discard); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	if rec := get(goodToken, "/api/v1/admin/cpu-profile?seconds=1"); rec.Code != http.StatusConflict {
		t.Errorf("while one is taken: %d, want 409", rec.Code)
	}
}
