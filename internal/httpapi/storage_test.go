package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fakeStorage keeps what is set and the move begun, and fakeStores answers a check of any bucket
// but "unreachable".
type fakeStorage struct {
	kept   domain.Storage
	moving *domain.StorageMove
}

func (f *fakeStorage) Storage(context.Context) (domain.Storage, error) { return f.kept, nil }

func (f *fakeStorage) SetStorage(_ context.Context, s domain.Storage) error {
	f.kept = s
	return nil
}

func (f *fakeStorage) StorageMove(context.Context) (domain.StorageMove, bool, error) {
	if f.moving == nil {
		return domain.StorageMove{}, false, nil
	}
	return *f.moving, true, nil
}

func (f *fakeStorage) StartStorageMove(_ context.Context, to domain.Storage) error {
	f.moving = &domain.StorageMove{To: to, Started: time.Now()}
	return nil
}

func (f *fakeStorage) CancelStorageMove(context.Context) error {
	if f.moving == nil {
		return store.ErrNotFound
	}
	f.moving = nil
	return nil
}

type fakeStores struct {
	checked []domain.Bucket
}

func (f *fakeStores) Probe(context.Context) (string, error) {
	return "https://media.example.com/photon/.photon-probe.png?X-Amz-Signature=s", nil
}

func (f *fakeStores) Check(_ context.Context, b domain.Bucket) error {
	f.checked = append(f.checked, b)
	if b.Name == "unreachable" {
		return errors.New("there is no bucket \"unreachable\" at s3.example.com")
	}
	return nil
}

func storageAPI(kept domain.Storage) (*API, *fakeStorage, *fakeStores, *fakeEvents) {
	settings, stores, events := &fakeStorage{kept: kept}, &fakeStores{}, &fakeEvents{}
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
	Delivery: domain.DeliverRedirect, PublicEndpoint: "https://media.example.com",
}}

// The secret key is written and never read back: the admin is told only that one is kept.
func TestABucketsSecretKeyIsNeverShown(t *testing.T) {
	api, _, _, _ := storageAPI(keptBucket)
	rec := ask(api, http.MethodGet, "/api/v1/admin/storage", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "kept secret") ||
		!strings.Contains(rec.Body.String(), `"secret_key_set":true`) {
		t.Errorf("answered %d %s, want the bucket with its secret key said to be set and not shown", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"probe":"https://media.example.com/`) {
		t.Errorf("answered %s, want a picture in the bucket for the browser to try", rec.Body)
	}
}

// Clients are given what is kept through this server unless an admin sends them to the bucket.
func TestClientsAreSentToTheBucketOnlyWhenAsked(t *testing.T) {
	api, settings, _, _ := storageAPI(domain.Storage{Kind: domain.StorageDisk})
	ask(api, http.MethodPut, "/api/v1/admin/storage", `{"kind":"bucket","bucket":{"name":"photon"}}`)
	if settings.moving == nil || settings.moving.To.Bucket.Delivery != domain.DeliverProxy {
		t.Errorf("moving %+v, want what is kept given through this server", settings.moving)
	}
	settings.moving = nil
	rec := ask(api, http.MethodPut, "/api/v1/admin/storage", `{"kind":"bucket","bucket":{"name":"photon","delivery":"teleport"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown delivery: answered %d, want it refused", rec.Code)
	}
}

// A server keeping things on disk chooses a bucket: it is checked, then what is kept is moved
// there, every node told, and the admin shown the move and never the secret key.
func TestABucketIsCheckedThenMovedTo(t *testing.T) {
	api, settings, stores, events := storageAPI(domain.Storage{Kind: domain.StorageDisk})
	rec := ask(api, http.MethodPut, "/api/v1/admin/storage",
		`{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon","access_key":"AKIA","secret_key":"s"}}`)
	if rec.Code != http.StatusOK || len(stores.checked) != 1 || settings.moving == nil || settings.moving.To.Bucket.Name != "photon" {
		t.Fatalf("answered %d %s after %d checks, moving %+v; want the bucket checked and moved to", rec.Code, rec.Body, len(stores.checked), settings.moving)
	}
	if settings.kept.Kind != domain.StorageDisk {
		t.Errorf("kept %+v, want things kept on disk until the move is done", settings.kept)
	}
	if len(events.raised) != 1 || events.raised[0].Kind != domain.EventStorageChanged {
		t.Errorf("raised %+v, want every node told", events.raised)
	}
	if !strings.Contains(rec.Body.String(), `"move":{"to":{"kind":"bucket"`) || strings.Contains(rec.Body.String(), `"s"`) {
		t.Errorf("answered %s, want the move shown without the secret key", rec.Body)
	}
}

// While what is kept is being moved, nothing else is chosen until the move is cancelled, which
// every node is told of.
func TestAMoveIsCancelledBeforeAnythingElseIsChosen(t *testing.T) {
	api, settings, _, events := storageAPI(domain.Storage{Kind: domain.StorageDisk})
	ask(api, http.MethodPut, "/api/v1/admin/storage", `{"kind":"bucket","bucket":{"name":"photon"}}`)
	if rec := ask(api, http.MethodPut, "/api/v1/admin/storage", `{"kind":"bucket","bucket":{"name":"other"}}`); rec.Code != http.StatusConflict {
		t.Errorf("choosing again while moving: answered %d %s, want it refused", rec.Code, rec.Body)
	}
	if rec := ask(api, http.MethodGet, "/api/v1/admin/storage", ""); !strings.Contains(rec.Body.String(), `"move":`) {
		t.Errorf("answered %s, want the move under way shown", rec.Body)
	}
	if rec := ask(api, http.MethodDelete, "/api/v1/admin/storage/move", ""); rec.Code != http.StatusNoContent || settings.moving != nil {
		t.Errorf("cancelling: answered %d, moving %+v; want the move gone", rec.Code, settings.moving)
	}
	if rec := ask(api, http.MethodDelete, "/api/v1/admin/storage/move", ""); rec.Code != http.StatusNotFound {
		t.Errorf("cancelling no move: answered %d, want 404", rec.Code)
	}
	if n := len(events.raised); n != 2 {
		t.Errorf("raised %d events, want every node told of the move and of its cancelling", n)
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
		api, settings, _, events := storageAPI(domain.Storage{Kind: domain.StorageDisk})
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
	api, settings, stores, _ := storageAPI(keptBucket)
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

// Signing for the same place differently is taken up at once, with no move.
func TestTheSamePlaceSignedForDifferentlyNeedsNoMove(t *testing.T) {
	api, settings, _, _ := storageAPI(keptBucket)
	rec := ask(api, http.MethodPut, "/api/v1/admin/storage", `{"kind":"bucket","bucket":{"endpoint":"https://s3.example.com","name":"photon"}}`)
	if rec.Code != http.StatusOK || settings.moving != nil || settings.kept.Bucket.AccessKey != "" {
		t.Errorf("answered %d %s, kept %+v, moving %+v; want the server's own credentials at once", rec.Code, rec.Body, settings.kept.Bucket, settings.moving)
	}
}
