//go:build integration

package store

import (
	"errors"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAnInviteGivesAProfileItsFirstPasswordOnce(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	now := time.Now()
	manager, err := s.AddProfile(ctx, "Manager", domain.RoleManager, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.AddProfile(ctx, "Guest", domain.RoleUser, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddInvite(ctx, manager.ID, nil, []byte("manager's"), now, now.Add(time.Hour)); !errors.Is(err, ErrHasPassword) {
		t.Errorf("inviting a profile with a password: %v, want %v", err, ErrHasPassword)
	}
	if err := s.AddInvite(ctx, guest.ID, &manager.ID, []byte("not kept"), now, now.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("a manager inviting a profile it does not keep: %v, want %v", err, ErrNotFound)
	}
	for _, code := range []string{"lapsed", "first", "second"} {
		expires := now.Add(time.Hour)
		if code == "lapsed" {
			expires = now.Add(-time.Minute)
		}
		if err := s.AddInvite(ctx, guest.ID, nil, []byte(code), now.Add(-time.Hour), expires); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AcceptInvite(ctx, []byte("lapsed"), "chosen", now); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("a lapsed invite: %v, want %v", err, ErrInviteNotFound)
	}
	accepted, err := s.AcceptInvite(ctx, []byte("first"), "chosen", now)
	if err != nil || accepted.ID != guest.ID {
		t.Fatalf("accepting: %+v, %v", accepted, err)
	}
	if _, secrets, err := s.ProfileSecrets(ctx, guest.ID); err != nil || secrets.Password != "chosen" {
		t.Errorf("the password chosen: %q, %v", secrets.Password, err)
	}
	if _, err := s.AcceptInvite(ctx, []byte("second"), "another", now); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("another invite to a profile with a password now: %v, want %v", err, ErrInviteNotFound)
	}
}
