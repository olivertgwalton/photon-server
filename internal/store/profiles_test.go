//go:build integration

package store

import (
	"errors"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestTheServerKeepsAnAdmin(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	oliver, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func() error
		want   error
	}{
		{"demoting the last admin", func() error {
			_, err := s.SetProfile(ctx, oliver.ID, ProfileChange{Role: domain.RoleUser}, nil)
			return err
		}, ErrLastAdmin},
		{"removing the last admin", func() error { _, err := s.RemoveProfile(ctx, oliver.ID, nil); return err }, ErrLastAdmin},
		{"renaming onto another's name", func() error {
			_, err := s.SetProfile(ctx, kid.ID, ProfileChange{Name: "Oliver"}, nil)
			return err
		}, ErrProfileExists},
		{"adding another's name in another case", func() error {
			_, err := s.AddProfile(ctx, "kid", domain.RoleUser, "hash", nil)
			return err
		}, ErrProfileExists},
		{"renaming onto another's name in another case", func() error {
			_, err := s.SetProfile(ctx, kid.ID, ProfileChange{Name: "OLIVER"}, nil)
			return err
		}, ErrProfileExists},
	} {
		if err := tc.change(); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	promoted, err := s.SetProfile(ctx, kid.ID, ProfileChange{Name: "Teen", Role: domain.RoleAdmin}, nil)
	if err != nil || promoted.Name != "Teen" || promoted.Role != domain.RoleAdmin {
		t.Fatalf("promoting: %+v, %v", promoted, err)
	}
	// With two admins, either may go.
	if _, err := s.SetProfile(ctx, oliver.ID, ProfileChange{Role: domain.RoleUser}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveProfile(ctx, oliver.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProfileByID(ctx, oliver.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after removing: %v, want ErrNotFound", err)
	}
}

// A profile keeps its password through a change that leaves it out, and never goes without one.
func TestEveryProfileHasAPassword(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []ProfileChange{{Name: "Teen"}, {PasswordHash: "new hash"}, {}} {
		if _, err := s.SetProfile(ctx, kid.ID, c, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, secrets, err := s.ProfileSecrets(ctx, kid.ID); err != nil || secrets.Password != "new hash" {
		t.Errorf("password hash %q, %v; want the one set last", secrets.Password, err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO profiles (name, role) VALUES ('Guest', 'user')`); err == nil {
		t.Error("a profile was stored with no password")
	}
}

func TestAProfileKeepsItsPicture(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	picture := uuid.NewV7()
	got, err := s.SetAvatar(ctx, kid.ID, picture, nil)
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
	if got, err := s.SetAvatar(ctx, kid.ID, uuid.UUID{}, nil); err != nil || got.Avatar != (uuid.UUID{}) {
		t.Errorf("taking it away = %+v, %v", got, err)
	}
	live, err := s.LivePictures(ctx, []uuid.UUID{picture})
	if err != nil {
		t.Fatal(err)
	}
	if live[picture] {
		t.Error("a picture taken away is kept from the sweep")
	}
	if _, err := s.SetAvatar(ctx, uuid.NewV7(), picture, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such profile: %v", err)
	}
}

// A manager keeps the profiles it adds: they start seeing what it sees, it may grant them no more,
// and another's profiles are not there for it. Removing it hands its profiles to the admin.
func TestAManagerKeepsTheProfilesItAdds(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddLibrary(ctx, "Other", domain.LibraryMovies, "/srv/other")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.AddProfile(ctx, "Oliver", domain.RoleAdmin, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	sam, err := s.AddProfile(ctx, "Sam", domain.RoleManager, "hash", nil)
	if err != nil {
		t.Fatal(err)
	}
	twelve, fifteen := 12, 15
	if err := s.SetAccess(ctx, sam.ID, ProfileAccess{MaxAge: &twelve, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}}, nil); err != nil {
		t.Fatal(err)
	}
	for _, role := range []domain.Role{domain.RoleAdmin, domain.RoleManager} {
		if _, err := s.AddProfile(ctx, "Boss", role, "hash", &sam.ID); !errors.Is(err, ErrBeyondManager) {
			t.Errorf("a manager adding a %s: %v, want ErrBeyondManager", role, err)
		}
	}
	kid, err := s.AddProfile(ctx, "Kid", domain.RoleUser, "hash", &sam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kid.Manager != sam.ID {
		t.Errorf("kept by %s, want %s", kid.Manager, sam.ID)
	}
	if got, err := s.Access(ctx, kid.ID, &sam.ID); err != nil || got.MaxAge == nil || *got.MaxAge != 12 ||
		got.Unrated != domain.UnratedBlock || !slices.Equal(got.Libraries, []uuid.UUID{films.ID}) {
		t.Errorf("a new profile sees %+v, %v; want what its manager sees", got, err)
	}
	for name, a := range map[string]ProfileAccess{
		"every library":   {MaxAge: &twelve, Unrated: domain.UnratedBlock},
		"another library": {MaxAge: &twelve, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID, other.ID}},
		"an older age":    {MaxAge: &fifteen, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}},
		"any age":         {Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}},
		"unrated titles":  {MaxAge: &twelve, Unrated: domain.UnratedAllow, Libraries: []uuid.UUID{films.ID}},
	} {
		if err := s.SetAccess(ctx, kid.ID, a, &sam.ID); !errors.Is(err, ErrBeyondManager) {
			t.Errorf("granting %s: %v, want ErrBeyondManager", name, err)
		}
	}
	six := 6
	if err := s.SetAccess(ctx, kid.ID, ProfileAccess{MaxAge: &six, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}}, &sam.ID); err != nil {
		t.Errorf("granting less: %v", err)
	}
	if _, err := s.SetProfile(ctx, kid.ID, ProfileChange{Role: domain.RoleManager}, &sam.ID); !errors.Is(err, ErrBeyondManager) {
		t.Errorf("making a manager: %v, want ErrBeyondManager", err)
	}
	for name, change := range map[string]func() error{
		"changing": func() error { _, err := s.SetProfile(ctx, admin.ID, ProfileChange{Name: "Ollie"}, &sam.ID); return err },
		"removing": func() error { _, err := s.RemoveProfile(ctx, admin.ID, &sam.ID); return err },
		"reading":  func() error { _, err := s.Access(ctx, admin.ID, &sam.ID); return err },
		"granting": func() error {
			return s.SetAccess(ctx, admin.ID, ProfileAccess{MaxAge: &six, Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}}, &sam.ID)
		},
		"picturing": func() error {
			_, err := s.SetAvatar(ctx, admin.ID, uuid.NewV7(), &sam.ID)
			return err
		},
	} {
		if err := change(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s another's profile: %v, want ErrNotFound", name, err)
		}
	}
	if _, err := s.SetProfile(ctx, kid.ID, ProfileChange{Role: domain.RoleAdmin}, nil); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func() error{
		"changing": func() error {
			_, err := s.SetProfile(ctx, kid.ID, ProfileChange{PasswordHash: "mine"}, &sam.ID)
			return err
		},
		"removing": func() error { _, err := s.RemoveProfile(ctx, kid.ID, &sam.ID); return err },
	} {
		if err := change(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s a kept profile the admin raised: %v, want ErrNotFound", name, err)
		}
	}
	if _, err := s.RemoveProfile(ctx, sam.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ProfileByID(ctx, kid.ID); err != nil || got.Manager != (uuid.UUID{}) {
		t.Errorf("after its manager went: %+v, %v; want the admin's", got, err)
	}
}
