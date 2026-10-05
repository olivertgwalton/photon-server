package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

// fakeWork is a server scanning on a schedule, with one dead job and one film playing.
type fakeWork struct{ asked []domain.TaskKey }

func (f *fakeWork) Statuses(context.Context) ([]task.Status, error) {
	return []task.Status{{Key: domain.TaskScanLibraries, Running: true, Next: time.Unix(1_800_000_000, 0)}}, nil
}

func (f *fakeWork) Request(_ context.Context, key domain.TaskKey) error {
	if key != domain.TaskScanLibraries {
		return task.ErrNoTask
	}
	f.asked = append(f.asked, key)
	return nil
}

func (f *fakeWork) JobQueue(context.Context) ([]store.JobCount, []store.DeadJob, error) {
	return []store.JobCount{{Kind: domain.JobIdentify, State: domain.JobDead, Count: 1}},
		[]store.DeadJob{{ID: 7, Kind: domain.JobIdentify, Attempts: 5, Error: "tmdb is down"}}, nil
}

func (f *fakeWork) RetryJob(_ context.Context, id int64) error {
	if id != 7 {
		return store.ErrNotFound
	}
	return nil
}

func (f *fakeWork) RunningJobs(context.Context) ([]store.Job, error) {
	return []store.Job{{ID: 9, Kind: domain.JobScanLibrary, Subject: films, Attempts: 1}}, nil
}

func (f *fakeWork) Playbacks(context.Context) ([]domain.Playback, error) {
	return []domain.Playback{{ID: playbackID, Profile: oliver.ID, Item: films, Method: domain.PlayTranscode, Position: time.Minute}}, nil
}

func TestAnAdminSeesTheServersWork(t *testing.T) {
	work := &fakeWork{}
	api := New(slog.New(slog.DiscardHandler), Info{}, Services{Auth: fakeAuth{}, Tasks: work, Jobs: work, NowPlaying: work, HLS: fakeHLS{}})
	do := func(token, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		token, method, target string
		want                  int
		body                  string
	}{
		{memberToken, http.MethodGet, "/api/v1/admin/playbacks", http.StatusForbidden, ""},
		{goodToken, http.MethodGet, "/api/v1/admin/tasks", http.StatusOK, `"key":"scan_libraries","running":true`},
		{goodToken, http.MethodPost, "/api/v1/admin/tasks/scan_libraries/run", http.StatusAccepted, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/tasks/defragment/run", http.StatusNotFound, ""},
		{goodToken, http.MethodGet, "/api/v1/admin/jobs", http.StatusOK, `"dead":[{"id":7,"kind":"identify"`},
		{goodToken, http.MethodPost, "/api/v1/admin/jobs/7/retry", http.StatusAccepted, ""},
		{goodToken, http.MethodPost, "/api/v1/admin/jobs/8/retry", http.StatusNotFound, ""},
		{goodToken, http.MethodGet, "/api/v1/admin/playbacks", http.StatusOK, `"method":"transcode","state":"","position_ms":60000`},
		{goodToken, http.MethodGet, "/api/v1/admin/playbacks", http.StatusOK, `"transcodes":{"active":1,"conversions":0,"limit":4}`},
	} {
		rec := do(tc.token, tc.method, tc.target)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.body) {
			t.Errorf("%s %s: %d %s, want %d with %s", tc.method, tc.target, rec.Code, rec.Body, tc.want, tc.body)
		}
	}
	if len(work.asked) != 1 {
		t.Errorf("tasks asked for: %v, want the scan", work.asked)
	}
	var playing struct {
		Items []struct {
			TitleID uuid.UUID `json:"title_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(do(goodToken, http.MethodGet, "/api/v1/admin/playbacks").Body).Decode(&playing); err != nil ||
		len(playing.Items) != 1 || playing.Items[0].TitleID != films {
		t.Errorf("playbacks = %+v, %v; want the film", playing, err)
	}
}
