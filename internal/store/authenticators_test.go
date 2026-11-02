//go:build integration

package store

import (
	"errors"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnAuthenticatorsCodeIsTakenOnceAndGoesWithAReset(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := s.AddProfile(ctx, "Parent", domain.RoleManager, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", &manager.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []domain.Profile{admin, kid} {
		if err := s.SetAuthenticator(ctx, p.ID, []byte("12345678901234567890"), 100); err != nil {
			t.Fatal(err)
		}
	}

	for _, step := range []int64{100, 99} {
		if err := s.TakeAuthenticatorStep(ctx, admin.ID, step); !errors.Is(err, ErrNotFound) {
			t.Errorf("taking step %d after 100: %v, want %v", step, err, ErrNotFound)
		}
	}
	if err := s.TakeAuthenticatorStep(ctx, admin.ID, 101); err != nil {
		t.Errorf("taking step 101: %v", err)
	}
	if a, err := s.Authenticator(ctx, admin.ID, nil); err != nil || a.Step != 101 || string(a.Secret) != "12345678901234567890" {
		t.Errorf("Authenticator = %+v, %v; want step 101 and the secret", a, err)
	}

	if _, err := s.Authenticator(ctx, admin.ID, &manager.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a manager reading the admin's: %v, want %v", err, ErrNotFound)
	}
	if err := s.RemoveAuthenticator(ctx, admin.ID, &manager.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a manager removing the admin's: %v, want %v", err, ErrNotFound)
	}
	if err := s.RemoveAuthenticator(ctx, kid.ID, &manager.ID); err != nil {
		t.Errorf("a manager removing its user's: %v", err)
	}
	if _, err := s.Authenticator(ctx, kid.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removing it: %v, want %v", err, ErrNotFound)
	}

	if err := s.ResetPassword(ctx, admin.ID, "new hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticator(ctx, admin.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("after a reset: %v, want %v", err, ErrNotFound)
	}
}
