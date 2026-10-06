package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeWatching knows one title, films; progress past an hour reaches its end, and its state last
// changed at the start of 2026.
type fakeWatching struct{}

func (fakeWatching) SaveProgress(_ context.Context, profile, item uuid.UUID, position time.Duration, _ domain.Reach, at *time.Time) (domain.Reach, error) {
	switch {
	case item != films || profile != oliver.ID:
		return "", store.ErrNotFound
	case at != nil && at.Year() < 2026:
		return "", store.ErrSuperseded
	case position > time.Hour:
		return domain.ReachEnd, nil
	}
	return domain.ReachResumable, nil
}

func (fakeWatching) mark(_ context.Context, _, item uuid.UUID) error {
	if item != films {
		return store.ErrNotFound
	}
	return nil
}

func (f fakeWatching) MarkWatched(ctx context.Context, p, i uuid.UUID, _ *time.Time) error {
	return f.mark(ctx, p, i)
}

func (f fakeWatching) MarkUnwatched(ctx context.Context, p, i uuid.UUID) error {
	return f.mark(ctx, p, i)
}

func (f fakeWatching) ClearProgress(ctx context.Context, p, i uuid.UUID) error {
	return f.mark(ctx, p, i)
}

func (f fakeWatching) Favourite(ctx context.Context, p, i uuid.UUID) error { return f.mark(ctx, p, i) }

func (f fakeWatching) Unfavourite(ctx context.Context, p, i uuid.UUID) error {
	return f.mark(ctx, p, i)
}

func TestWatching(t *testing.T) {
	title := "/api/v1/titles/" + films.String()
	// A client's clock a minute ahead is skew; an hour ahead is wrong.
	soon, later := time.Now().Add(time.Minute).Format(time.RFC3339), time.Now().Add(time.Hour).Format(time.RFC3339)
	for _, tc := range []struct {
		method, target, body string
		want                 int
		wantBody             string
	}{
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000}`, http.StatusOK, `{"reach":"resumable"}`},
		{http.MethodPut, title + "/progress", `{"position_ms": 7000000}`, http.StatusOK, `{"reach":"end"}`},
		{http.MethodPut, title + "/progress", `{"position_ms": -1}`, http.StatusBadRequest, ""},
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000, "at": "2026-03-01T20:00:00Z"}`, http.StatusOK, `{"reach":"resumable"}`},
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000, "at": "2025-03-01T20:00:00Z"}`, http.StatusConflict, ""},
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000, "at": "` + soon + `"}`, http.StatusOK, ""},
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000, "at": "` + later + `"}`, http.StatusBadRequest, ""},
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000, "at": "0001-01-01T00:00:00Z"}`, http.StatusBadRequest, ""},
		{http.MethodPut, title + "/progress", `{"position_ms": 1200000, "at": "yesterday"}`, http.StatusBadRequest, ""},
		{http.MethodPut, title + "/watched", `{"at": "2026-03-01T20:00:00+01:00"}`, http.StatusNoContent, ""},
		{http.MethodPut, title + "/watched", `{}`, http.StatusNoContent, ""},
		{http.MethodPut, title + "/watched", `{"at": "` + later + `"}`, http.StatusBadRequest, ""},
		{http.MethodPut, title + "/watched", `{"at": "1970-01-01T00:00:00Z"}`, http.StatusBadRequest, ""},
		{http.MethodPut, "/api/v1/titles/" + uuid.NewV7().String() + "/progress", `{"position_ms": 1}`, http.StatusNotFound, ""},
		{http.MethodDelete, title + "/progress", "", http.StatusNoContent, ""},
		{http.MethodDelete, "/api/v1/titles/" + uuid.NewV7().String() + "/progress", "", http.StatusNotFound, ""},
		{http.MethodPut, title + "/watched", "", http.StatusNoContent, ""},
		{http.MethodDelete, title + "/favourite", "", http.StatusNoContent, ""},
		{http.MethodPut, "/api/v1/titles/" + uuid.NewV7().String() + "/favourite", "", http.StatusNotFound, ""},
	} {
		rec := serve(t, tc.method, tc.target, goodToken, tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.target, rec.Code, tc.want)
		}
		if tc.wantBody != "" && rec.Body.String() != tc.wantBody+"\n" {
			t.Errorf("%s %s: body %q, want %q", tc.method, tc.target, rec.Body.String(), tc.wantBody)
		}
	}
}
