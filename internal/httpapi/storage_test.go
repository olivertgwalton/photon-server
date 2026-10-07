package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakeStorage keeps what is set, and fakeStores holds something or nothing and answers a check of
// any bucket but "unreachable".
type fakeStorage struct{ kept domain.Storage }

func (f *fakeStorage) Storage(context.Context) (domain.Storage, error) { return f.kept, nil }

func (f *fakeStorage) SetStorage(_ context.Context, s domain.Storage) error {
	f.kept = s
	return nil
}

type fakeStores struct {
	holding bool
	checked []domain.Bucket
}

func (f *fakeStores) Empty(context.Context) (bool, error) { return !f.holding, nil }

func (f *fakeStores) Check(_ context.Context, b domain.Bucket) error {
	f.checked = append(f.checked, b)
	if b.Name == "unreachable" {
		return errors.New("there is no bucket \"unreachable\" at s3.example.com")
	}
	return nil
}

func storageAPI(kept domain.Storage, holding bool) (*API, *fakeStorage, *fakeStores, *fakeEvents) {
	settings, stores, events := &fakeStorage{kept: kept}, &fakeStores{holding: holding}, &fakeEvents{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{
		Auth: fakeAuth{}, Events: events, Storage: settings, Stores: stores,
	})
	return api, settings, stores, events
}

func ask(api *API, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+goodToken)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

var keptBucket = domain.Storage{Kind: domain.StorageBucket, Bucket: domain.Bucket{
	Endpoint: "https://s3.example.com", Name: "photon", AccessKey: "AKIA", SecretKey: "kept secret",
}}

// The secret key is written and never read back: the admin is told only that one is kept.
func TestABucketsSecretKeyIsNeverShown(t *testing.T) {
	api, _, _, _ := storageAPI(keptBucket, true)
	rec := ask(api, http.MethodGet, "/api/v1/admin/storage", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "kept secret") ||
		!strings.Contains(rec.Body.String(), `"secret_key_set":true`) {
		t.Errorf("answered %d %s, want the bucket with its secret key said to be set and not shown", rec.Code, rec.Body)
	}
}

// A new server chooses a bucket: it is checked, kept, and every node told.
func TestABucketIsCheckedThenEveryNodeTold(t *testing.T) {
	api, settings, stores, events := storageAPI(domain.Storage{Kind: domain.StorageDisk}, false)
	rec := ask(api, http.MethodPut, "/api/v1/admin/storage",
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon","access_key":"AKIA","secret_key":"s"}}`)
	if rec.Code != http.StatusOK || settings.kept.Kind != domain.StorageBucket || len(stores.checked) != 1 {
		t.Fatalf("answered %d %s, kept %+v after %d checks; want the bucket kept once checked", rec.Code, rec.Body, settings.kept, len(stores.checked))
	}
	if len(events.raised) != 1 || events.raised[0].Kind != domain.EventStorageChanged {
		t.Errorf("raised %+v, want every node told", events.raised)
	}
	if strings.Contains(rec.Body.String(), `"s"`) {
		t.Errorf("answered %s, which shows the secret key", rec.Body)
	}
}

// A bucket the server cannot reach, or one half-signed for, is refused with why, and nothing kept.
func TestABucketThatCannotBeUsedIsRefusedWithWhy(t *testing.T) {
	for body, why := range map[string]string{
		`{"kind":"bucket","bucket":{"name":"unreachable"}}`:             "no bucket",
		`{"kind":"bucket","bucket":{"name":""}}`:                        "name",
		`{"kind":"bucket","bucket":{"name":"photon","secret_key":"s"}}`: "access key",
		`{"kind":"cloud"}`: "kind",
	} {
		api, settings, _, events := storageAPI(domain.Storage{Kind: domain.StorageDisk}, false)
		for _, target := range []string{"/api/v1/admin/storage/check", "/api/v1/admin/storage"} {
			method := http.MethodPost
			if target == "/api/v1/admin/storage" {
				method = http.MethodPut
			}
			rec := ask(api, method, target, body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), why) {
				t.Errorf("%s %s: answered %d %s, want it refused saying %q", method, body, rec.Code, rec.Body, why)
			}
		}
		if settings.kept.Kind != domain.StorageDisk || len(events.raised) != 0 {
			t.Errorf("%s: kept %+v, raised %+v; want nothing changed", body, settings.kept, events.raised)
		}
	}
}

// A secret key left out keeps the one kept for the same access key, so an admin changing only the
// region need not type it again; a new access key needs its own.
func TestALeftOutSecretKeyIsKeptForItsAccessKey(t *testing.T) {
	api, settings, stores, _ := storageAPI(keptBucket, true)
	rec := ask(api, http.MethodPut, "/api/v1/admin/storage",
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon","region":"eu-west-2","access_key":"AKIA"}}`)
	if rec.Code != http.StatusOK || settings.kept.Bucket.SecretKey != "kept secret" || stores.checked[0].SecretKey != "kept secret" {
		t.Errorf("answered %d %s, kept %+v; want the secret key kept and checked with", rec.Code, rec.Body, settings.kept)
	}
	rec = ask(api, http.MethodPut, "/api/v1/admin/storage",
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon","access_key":"OTHER"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a new access key without its secret key: answered %d, want it refused", rec.Code)
	}
}

// Nothing kept is left behind: moving away from where artwork and previews are kept is refused,
// though how that place is signed for may change.
func TestArtworkIsNeverLeftBehind(t *testing.T) {
	api, settings, _, _ := storageAPI(keptBucket, true)
	for body, want := range map[string]int{
		`{"kind":"disk"}`: http.StatusConflict,
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"other"}}`:                 http.StatusConflict,
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon","folder":"new"}}`: http.StatusConflict,
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon"}}`:                http.StatusOK,
	} {
		if rec := ask(api, http.MethodPut, "/api/v1/admin/storage", body); rec.Code != want {
			t.Errorf("%s: answered %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
	if b := settings.kept.Bucket; b.Name != "photon" || b.AccessKey != "" {
		t.Errorf("kept %+v, want the same bucket signed for by the server's own credentials", b)
	}
}
