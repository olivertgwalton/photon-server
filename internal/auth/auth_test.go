//go:build integration

package auth

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

func newService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	svc, err := New(t.Context(), st)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st
}

func addProfile(t *testing.T, svc *Service, st *store.Store, name, password string) domain.Profile {
	t.Helper()
	hash, err := svc.HashPassword(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.AddProfile(t.Context(), name, domain.RoleAdmin, hash)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var tv = Device{Name: "Living room", Client: "Photon tvOS"}

func TestSignInAndOut(t *testing.T) {
	svc, st := newService(t)
	oliver := addProfile(t, svc, st, "Oliver", "correct horse")
	if _, err := st.AddProfile(t.Context(), "Guest", domain.RoleMember, ""); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, password string }{
		{"Oliver", "wrong"}, {"Nobody", "correct horse"}, {"Guest", ""},
	} {
		if _, _, err := svc.SignIn(t.Context(), c.name, c.password, tv); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("SignIn(%q, %q): err = %v, want %v", c.name, c.password, err, ErrInvalidCredentials)
		}
	}

	token, profile, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv)
	if err != nil {
		t.Fatal(err)
	}
	if profile != oliver {
		t.Errorf("signed in as %+v, want %+v", profile, oliver)
	}
	session, err := svc.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	if session.Profile != oliver {
		t.Errorf("the token's session is %+v's", session.Profile)
	}
	if _, err := svc.Authenticate(t.Context(), token+"x"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("an altered token authenticated (err %v)", err)
	}

	if err := svc.SignOut(t.Context(), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(t.Context(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("a signed-out token still authenticates (err %v)", err)
	}
}

func TestExpiredSessionsAreRefused(t *testing.T) {
	svc, st := newService(t)
	addProfile(t, svc, st, "Oliver", "correct horse")
	token, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := st.TouchSession(t.Context(), session.ID, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(t.Context(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("an expired token authenticated (err %v)", err)
	}
}

func TestWeakHashIsUpgradedAtSignIn(t *testing.T) {
	svc, st := newService(t)
	weak := &hasher{slots: make(chan struct{}, 1)}
	salt := []byte("0123456789abcdef")
	key, err := weak.key(t.Context(), []byte("correct horse"), salt, 1, 19<<10, 1)
	if err != nil {
		t.Fatal(err)
	}
	old := "$argon2id$v=19$m=19456,t=1,p=1$" + b64(salt) + "$" + b64(key)
	if _, err := st.AddProfile(t.Context(), "Oliver", domain.RoleAdmin, old); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv); err != nil {
		t.Fatal(err)
	}
	_, hash, err := st.ProfileByName(t.Context(), "Oliver")
	if err != nil {
		t.Fatal(err)
	}
	if _, stale, _ := svc.hasher.Verify(t.Context(), hash, "correct horse"); hash == old || stale {
		t.Errorf("the stored hash was not upgraded: %s", hash)
	}
}

func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }
