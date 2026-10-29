package httpapi

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type fakePush struct{ subs []store.PushSubscription }

func (f *fakePush) Subscribe(_ context.Context, _ uuid.UUID, sub store.PushSubscription) (store.PushSubscription, bool, error) {
	if i := slices.IndexFunc(f.subs, func(s store.PushSubscription) bool { return s.Endpoint == sub.Endpoint }); i >= 0 {
		return f.subs[i], false, nil
	}
	sub.ID = uuid.NewV7()
	f.subs = append(f.subs, sub)
	return sub, true, nil
}

func (f *fakePush) PushSubscriptions(context.Context, uuid.UUID) ([]store.PushSubscription, error) {
	return f.subs, nil
}

func (f *fakePush) Unsubscribe(_ context.Context, _, id uuid.UUID) error {
	n := len(f.subs)
	f.subs = slices.DeleteFunc(f.subs, func(s store.PushSubscription) bool { return s.ID == id })
	if len(f.subs) == n {
		return store.ErrNotFound
	}
	return nil
}

type fixedKey []byte

func (k fixedKey) PublicKey() []byte { return k }

func TestABrowserSubscribesToItsProfilesNotifications(t *testing.T) {
	push := &fakePush{}
	api := New(slog.New(slog.DiscardHandler), domain.Info{}, Services{Auth: fakeAuth{}, Push: push, PushKey: fixedKey{4, 1, 2}})
	call := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+goodToken)
		req.Header.Set("Accept-Language", "fr-FR, en;q=0.5")
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	k, err := ecdh.P256().GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding.EncodeToString
	key, secret := b64(k.PublicKey().Bytes()), b64(make([]byte, 16))
	body := func(endpoint, p256dh, auth string) string {
		return `{"endpoint":"` + endpoint + `","keys":{"p256dh":"` + p256dh + `","auth":"` + auth + `"}}`
	}
	for _, c := range []struct{ name, body string }{
		{"a plain http endpoint", body("http://push.example.com/a", key, secret)},
		{"a key that is no P-256 point", body("https://push.example.com/a", b64([]byte{4, 1}), secret)},
		{"a secret of the wrong size", body("https://push.example.com/a", key, b64([]byte{1}))},
	} {
		if rec := call(http.MethodPost, "/api/v1/profile/push-subscriptions", c.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", c.name, rec.Code)
		}
	}
	rec := call(http.MethodPost, "/api/v1/profile/push-subscriptions", body("https://push.example.com/a", key, secret))
	var made pushSubscriptionJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &made); rec.Code != http.StatusCreated || err != nil || made.Endpoint != "https://push.example.com/a" {
		t.Fatalf("subscribing: %d %s", rec.Code, rec.Body)
	}
	if len(push.subs) != 1 || push.subs[0].Language != "fr" || len(push.subs[0].Auth) != 16 {
		t.Errorf("kept %+v, want the browser's keys and the language it reads", push.subs)
	}
	if rec := call(http.MethodPost, "/api/v1/profile/push-subscriptions", body("https://push.example.com/a", key, secret)); rec.Code != http.StatusOK {
		t.Errorf("subscribing again: %d, want 200", rec.Code)
	}

	rec = call(http.MethodGet, "/api/v1/profile/push-subscriptions", "")
	var listed listJSON[pushSubscriptionJSON]
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || len(listed.Items) != 1 || listed.Items[0].ID != made.ID {
		t.Errorf("listed: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodGet, "/api/v1/push-key", ""); !strings.Contains(rec.Body.String(), `"key":"BAEC"`) {
		t.Errorf("the key: %d %s", rec.Code, rec.Body)
	}
	// An Apple device subscribes by its APNs token and the push service that issued it.
	for _, bad := range []string{
		`{"kind":"apple","token":"not hex","environment":"production"}`,
		`{"kind":"apple","token":"a1b2"}`,
		`{"kind":"apple","token":"a1b2","environment":"sandbox"}`,
	} {
		if rec := call(http.MethodPost, "/api/v1/profile/push-subscriptions", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, rec.Code)
		}
	}
	rec = call(http.MethodPost, "/api/v1/profile/push-subscriptions", `{"kind":"apple","token":"A1B2C3","environment":"development"}`)
	var phone pushSubscriptionJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &phone); rec.Code != http.StatusCreated || err != nil ||
		phone.Kind != domain.PushApple || phone.Token != "a1b2c3" || phone.Endpoint != "" {
		t.Errorf("an iPhone subscribing: %d %s", rec.Code, rec.Body)
	}
	if kept := push.subs[len(push.subs)-1]; kept.ApnsEnvironment == nil || *kept.ApnsEnvironment != domain.APNsDevelopment {
		t.Errorf("kept %+v, want its APNs environment", kept)
	}

	// An Android device subscribes by its FCM registration token alone, kept as it is.
	for _, bad := range []string{`{"kind":"android"}`, `{"kind":"android","token":"has space"}`} {
		if rec := call(http.MethodPost, "/api/v1/profile/push-subscriptions", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, rec.Code)
		}
	}
	const fcm = "dQw4w9WgXcQ:APA91bH-Example_Token"
	rec = call(http.MethodPost, "/api/v1/profile/push-subscriptions", `{"kind":"android","token":"`+fcm+`"}`)
	var android pushSubscriptionJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &android); rec.Code != http.StatusCreated || err != nil ||
		android.Kind != domain.PushAndroid || android.Token != fcm || android.Endpoint != "" {
		t.Errorf("an Android phone subscribing: %d %s", rec.Code, rec.Body)
	}
	if kept := push.subs[len(push.subs)-1]; kept.Kind != domain.PushAndroid || kept.Endpoint != fcm || kept.ApnsEnvironment != nil || kept.P256DH != nil {
		t.Errorf("kept %+v, want its token alone", kept)
	}
	rec = call(http.MethodGet, "/api/v1/profile/push-subscriptions", "")
	if !strings.Contains(rec.Body.String(), `{"id":"`+android.ID.String()+`","kind":"android","token":"`+fcm+`"}`) {
		t.Errorf("listed: %s, want the Android phone by its token", rec.Body)
	}

	if rec := call(http.MethodDelete, "/api/v1/profile/push-subscriptions/"+made.ID.String(), ""); rec.Code != http.StatusNoContent {
		t.Errorf("unsubscribing: %d", rec.Code)
	}
	if rec := call(http.MethodDelete, "/api/v1/profile/push-subscriptions/"+made.ID.String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("unsubscribing again: %d, want 404", rec.Code)
	}
}
