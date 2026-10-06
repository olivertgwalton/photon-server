//go:build integration

package store

import (
	"errors"
	"testing"
	"uuid"

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
		{"removing the last admin", func() error { _, err := s.RemoveProfile(ctx, oliver.ID); return err }, ErrLastAdmin},
		{"clearing an admin's password", func() error {
			_, err := s.SetProfile(ctx, oliver.ID, ProfileChange{PasswordHash: &empty})
			return err
		}, ErrAdminNeedsPassword},
		{"making an admin of a profile with no password", func() error {
			_, err := s.SetProfile(ctx, kid.ID, ProfileChange{Role: domain.RoleAdmin})
			return err
		}, ErrAdminNeedsPassword},
		{"adding an admin with no password", func() error {
			_, err := s.AddProfile(ctx, "Guest", domain.RoleAdmin, "")
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
	if _, err := s.RemoveProfile(ctx, oliver.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProfileByID(ctx, oliver.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removing: %v, want ErrNotFound", err)
	}
}

func TestAProfileKeepsItsPicture(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleMember, "")
	if err != nil {
		t.Fatal(err)
	}
	picture := uuid.NewV7()
	got, err := s.SetAvatar(ctx, kid.ID, picture)
	if err != nil || got.Avatar != picture {
		t.Fatalf("SetAvatar = %+v, %v", got, err)
	}
	if pic, err := s.Picture(ctx, picture); err != nil || !pic.Kept {
		t.Errorf("Picture = %+v, %v; want it kept by the server", pic, err)
	}
	if live, err := s.LivePictures(ctx, []uuid.UUID{picture}); err != nil || !live[picture] {
		t.Errorf("the avatar is swept: %v, %v", live, err)
	}
	listed, err := s.Profiles(ctx)
	if err != nil || len(listed) != 1 || listed[0].Profile.Avatar != picture {
		t.Errorf("Profiles = %+v, %v; want the avatar listed", listed, err)
	}
	if got, err := s.SetAvatar(ctx, kid.ID, uuid.UUID{}); err != nil || got.Avatar != (uuid.UUID{}) {
		t.Errorf("taking it away = %+v, %v", got, err)
	}
	if live, _ := s.LivePictures(ctx, []uuid.UUID{picture}); live[picture] {
		t.Error("a picture taken away is kept from the sweep")
	}
	if _, err := s.SetAvatar(ctx, uuid.NewV7(), picture); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such profile: %v", err)
	}
}
