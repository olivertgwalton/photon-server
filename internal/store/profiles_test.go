//go:build integration

package store

import (
	"errors"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestTheServerKeepsAnAdminWithAPassword(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleRestricted, "")
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	for _, tc := range []struct {
		name   string
		change func() error
		want   error
	}{
		{"demoting the last admin", func() error {
			_, err := s.SetProfile(ctx, oliver.ID, ProfileChange{Role: domain.RoleMember})
			return err
		}, ErrLastAdmin},
		{"removing the last admin", func() error { return s.RemoveProfile(ctx, oliver.ID) }, ErrLastAdmin},
		{"clearing an admin's password", func() error {
			_, err := s.SetProfile(ctx, oliver.ID, ProfileChange{PasswordHash: &empty})
			return err
		}, ErrAdminNeedsPassword},
		{"making an admin of a profile with no password", func() error {
			_, err := s.SetProfile(ctx, kid.ID, ProfileChange{Role: domain.RoleAdmin})
			return err
		}, ErrAdminNeedsPassword},
		{"renaming onto another's name", func() error {
			_, err := s.SetProfile(ctx, kid.ID, ProfileChange{Name: "Oliver"})
			return err
		}, ErrProfileExists},
	} {
		if err := tc.change(); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	password := "kidhash"
	promoted, err := s.SetProfile(ctx, kid.ID, ProfileChange{Name: "Teen", Role: domain.RoleAdmin, PasswordHash: &password})
	if err != nil || promoted.Name != "Teen" || promoted.Role != domain.RoleAdmin {
		t.Fatalf("promoting with a password: %+v, %v", promoted, err)
	}
	// With two admins, either may go.
	if _, err := s.SetProfile(ctx, oliver.ID, ProfileChange{Role: domain.RoleMember}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProfile(ctx, oliver.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProfileByID(ctx, oliver.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removing: %v, want ErrNotFound", err)
	}
}
