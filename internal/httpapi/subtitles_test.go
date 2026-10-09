package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeFetched has films alone, and answers err where it is set.
type fakeFetched struct {
	err     error
	fetched domain.FoundSubtitle
}

func (f *fakeFetched) Search(_ context.Context, _, item, _ uuid.UUID, lang language.Tag) (uuid.UUID, []domain.FoundSubtitle, error) {
	if item != films {
		return uuid.UUID{}, nil, store.ErrNotFound
	}
	return films, []domain.FoundSubtitle{
		{Source: domain.SourceOpenSubtitles, ID: "2", Language: lang, Release: "Heat.1995.BluRay", ForRelease: true, Downloads: 10},
		{Source: domain.SourceOpenSubtitles, ID: "1", Language: lang, Release: "Any", Downloads: 900},
	}, f.err
}

func (f *fakeFetched) Fetch(_ context.Context, _, _, _ uuid.UUID, s domain.FoundSubtitle) (uuid.UUID, error) {
	f.fetched = s
	return uuid.NewV7(), f.err
}

func (*fakeFetched) RemoveFetchedSubtitle(context.Context, uuid.UUID) error { return nil }

func TestAViewerFindsAndFetchesSubtitlesForTheirCopy(t *testing.T) {
	fetched := &fakeFetched{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Subtitles: fetched})
	do := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	base := "/api/v1/titles/" + films.String() + "/subtitles"
	rec := do(memberToken, http.MethodGet, base+"/candidates?language=fr", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK ||
		!strings.Contains(body, `"items":[{"source":"opensubtitles","id":"2","language":"fr","release":"Heat.1995.BluRay","for_release":true`) {
		t.Errorf("search: %d %s; want the one made for the file, in French", rec.Code, body)
	}
	if rec := do(memberToken, http.MethodGet, base+"/candidates?language=Klingon!", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a language that is no tag: %d, want 400", rec.Code)
	}
	if rec := do(memberToken, http.MethodGet, "/api/v1/titles/"+uuid.NewV7().String()+"/subtitles/candidates?language=fr", ""); rec.Code != http.StatusNotFound {
		t.Errorf("no such title: %d, want 404", rec.Code)
	}
	rec = do(memberToken, http.MethodPost, base, `{"version_id": "`+films.String()+`", "source": "opensubtitles", "id": "2", "language": "fr", "release": "Heat.1995.BluRay", "forced": true}`)
	if want := (domain.FoundSubtitle{Source: domain.SourceOpenSubtitles, ID: "2", Language: language.French, Release: "Heat.1995.BluRay", Forced: true}); rec.Code != http.StatusCreated || fetched.fetched != want {
		t.Errorf("fetch: %d %s, fetched %+v", rec.Code, rec.Body, fetched.fetched)
	}
	for err, want := range map[error]int{provider.ErrNoSubtitler: http.StatusConflict, provider.ErrQuota: http.StatusTooManyRequests} {
		fetched.err = err
		if rec := do(memberToken, http.MethodPost, base, `{"source": "opensubtitles", "id": "2"}`); rec.Code != want {
			t.Errorf("fetching with %v: %d, want %d", err, rec.Code, want)
		}
	}
	if rec := do(memberToken, http.MethodDelete, "/api/v1/admin/subtitles/"+films.String(), ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member forgetting a subtitle: %d, want 403", rec.Code)
	}
}
