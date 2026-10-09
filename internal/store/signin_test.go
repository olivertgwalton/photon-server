//go:build integration

package store

import (
	"crypto/rand"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func pocketID(issuer string) domain.SignInProvider {
	return domain.SignInProvider{
		Slug: "pocket-id", Name: "Pocket ID", Issuer: issuer, ClientID: "photon", ClientSecret: "secret",
		Provisioning: domain.ProvisionLink, Recheck: domain.RecheckHourly,
	}
}

// An account at a provider signs in as the one profile that linked it, and is linked to no other;
// moving the provider to another issuer forgets the accounts linked at the old.
func TestAnAccountAtAProviderSignsInAsTheProfileThatLinkedIt(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	if err := s.SetSignInProvider(ctx, pocketID("https://id.example.com")); err != nil {
		t.Fatal(err)
	}
	ada, err := s.AddProfile(ctx, "Ada", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.AddProfile(ctx, "Bob", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkSignIn(ctx, ada.ID, "pocket-id", "sub-ada", "ada"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ProfileBySignIn(ctx, "pocket-id", "sub-ada"); err != nil || got.ID != ada.ID {
		t.Fatalf("ProfileBySignIn = %v, %v; want Ada", got.Name, err)
	}
	if err := s.LinkSignIn(ctx, bob.ID, "pocket-id", "sub-ada", "ada"); !errors.Is(err, ErrAccountLinked) {
		t.Errorf("linking Ada's account to Bob: %v; want ErrAccountLinked", err)
	}
	if err := s.LinkSignIn(ctx, ada.ID, "nowhere", "sub-ada", "ada"); !errors.Is(err, ErrNotFound) {
		t.Errorf("linking at no provider: %v; want ErrNotFound", err)
	}

	if err := s.SetSignInProvider(ctx, pocketID("https://id.example.com")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProfileBySignIn(ctx, "pocket-id", "sub-ada"); err != nil {
		t.Errorf("saving the provider unchanged forgot Ada's account: %v", err)
	}
	if err := s.SetSignInProvider(ctx, pocketID("https://elsewhere.example.com")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProfileBySignIn(ctx, "pocket-id", "sub-ada"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an account at the old issuer still signs in: %v", err)
	}
}

// A profile made for an account has no password, takes the first free name like the account's, and
// cannot unlink the account it has no other way in than; once it has a password, it can.
func TestAProfileMadeForAnAccountKeepsAWayIn(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	p := pocketID("https://id.example.com")
	p.Provisioning, p.Group = domain.ProvisionCreate, "photon"
	if err := s.SetSignInProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddProfile(ctx, "Ada", domain.RoleAdmin, "h", nil); err != nil {
		t.Fatal(err)
	}
	made, err := s.AddSignInProfile(ctx, "ada", "pocket-id", "sub-ada", "ada")
	if err != nil {
		t.Fatal(err)
	}
	if made.Name != "ada 2" || made.Role != domain.RoleUser {
		t.Errorf("made %q, a %s; want ada 2, a user", made.Name, made.Role)
	}
	if _, err := s.AddSignInProfile(ctx, "ada", "pocket-id", "sub-ada", "ada"); !errors.Is(err, ErrAccountLinked) {
		t.Errorf("a second profile for the same account: %v; want ErrAccountLinked", err)
	}
	if _, hash, err := s.ProfileByName(ctx, "ada 2"); err != nil || hash != "" {
		t.Errorf("the made profile's password hash is %q, %v; want none", hash, err)
	}

	if err := s.UnlinkSignIn(ctx, made.ID, "pocket-id"); !errors.Is(err, ErrLastWayIn) {
		t.Errorf("unlinking the one way in: %v; want ErrLastWayIn", err)
	}
	if _, err := s.ProfileBySignIn(ctx, "pocket-id", "sub-ada"); err != nil {
		t.Errorf("a refused unlink forgot the account: %v", err)
	}
	if err := s.ChangePassword(ctx, made.ID, "hash", made.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkSignIn(ctx, made.ID, "pocket-id"); err != nil {
		t.Errorf("unlinking with a password set: %v", err)
	}
	if err := s.UnlinkSignIn(ctx, made.ID, "pocket-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unlinking twice: %v; want ErrNotFound", err)
	}
}

func TestANameTooLongForItsNumberIsCutShort(t *testing.T) {
	long := string(make([]rune, domain.MaxProfileName))
	if got := []rune(numbered(long, 12)); len(got) != domain.MaxProfileName {
		t.Errorf("numbered name is %d long; want %d", len(got), domain.MaxProfileName)
	}
}

// A profile made for an account sees what its provider gives the profiles it makes, and no provider
// makes profiles for everyone it signs in.
func TestAProfileMadeForAnAccountSeesWhatItsProviderGives(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	films, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddLibrary(ctx, "Shows", domain.LibraryShows, "/srv/shows"); err != nil {
		t.Fatal(err)
	}
	p := pocketID("https://id.example.com")
	p.Provisioning = domain.ProvisionCreate
	if err := s.SetSignInProvider(ctx, p); err == nil {
		t.Error("a provider made profiles for anyone it signs in")
	}
	p.Group, p.MaxAge, p.Unrated, p.Libraries = "kids", new(12), domain.UnratedBlock, []uuid.UUID{films.ID}
	if err := s.SetSignInProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	made, err := s.AddSignInProfile(ctx, "ada", "pocket-id", "sub-ada", "ada")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Access(ctx, made.ID, nil)
	want := ProfileAccess{MaxAge: new(12), Unrated: domain.UnratedBlock, Libraries: []uuid.UUID{films.ID}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("access %+v, %v; want %+v", got, err, want)
	}
	if _, err := s.AddSignInProfile(ctx, "bob", "nowhere", "sub-bob", "bob"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a profile made for no provider: %v; want ErrNotFound", err)
	}
}

// A device's session goes with the account that signed it in: relinking the same account keeps it,
// and unlinking it, linking another in its place, moving the provider or removing it signs it out.
func TestASessionGoesWithTheAccountThatSignedItIn(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	if err := s.SetSignInProvider(ctx, pocketID("https://id.example.com")); err != nil {
		t.Fatal(err)
	}
	ada, err := s.AddProfile(ctx, "Ada", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	signedIn := func(t *testing.T, subject string) func() bool {
		t.Helper()
		if err := s.LinkSignIn(ctx, ada.ID, "pocket-id", subject, "ada"); err != nil {
			t.Fatal(err)
		}
		token := []byte(subject + rand.Text())
		_, err := s.CreateSession(ctx, NewSession{
			Kind: domain.SessionDevice, ProfileID: ada.ID, TokenHash: token, DeviceName: "TV", Client: "Photon",
			ExpiresAt: new(time.Now().Add(time.Hour)), Identity: domain.SignInIdentity{Provider: "pocket-id", Subject: subject},
		})
		if err != nil {
			t.Fatal(err)
		}
		return func() bool {
			got, _, err := s.SessionByToken(ctx, token, time.Now())
			return err == nil && got.Identity.Subject == subject
		}
	}
	for _, c := range []struct {
		name  string
		end   func() error
		lives bool
	}{
		{"relinking the same account", func() error { return s.LinkSignIn(ctx, ada.ID, "pocket-id", "sub-a", "renamed") }, true},
		{"unlinking it", func() error { return s.UnlinkSignIn(ctx, ada.ID, "pocket-id") }, false},
		{"linking another in its place", func() error { return s.LinkSignIn(ctx, ada.ID, "pocket-id", "sub-b", "ada") }, false},
		{"moving the provider", func() error { return s.SetSignInProvider(ctx, pocketID("https://moved.example.com")) }, false},
		{"removing the provider", func() error { return s.RemoveSignInProvider(ctx, "pocket-id") }, false},
	} {
		if err := s.SetSignInProvider(ctx, pocketID("https://id.example.com")); err != nil {
			t.Fatal(err)
		}
		alive := signedIn(t, "sub-a")
		if err := c.end(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if alive() != c.lives {
			t.Errorf("after %s the session lives: %v; want %v", c.name, alive(), c.lives)
		}
	}
	if _, err := s.CreateSession(ctx, NewSession{
		Kind: domain.SessionDevice, ProfileID: ada.ID, TokenHash: []byte("t"), ExpiresAt: new(time.Now().Add(time.Hour)),
		Identity: domain.SignInIdentity{Provider: "pocket-id", Subject: "unlinked"},
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a session for an account not linked: %v; want ErrNotFound", err)
	}
}

// Of nodes looking for accounts due a check at once, each account goes to one; a check refused
// after the account signed in again, and was let in since, signs nothing out.
func TestAnAccountIsCheckedByOneNodeAndASignInSinceStands(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	if err := s.SetSignInProvider(ctx, pocketID("https://id.example.com")); err != nil {
		t.Fatal(err)
	}
	ada, err := s.AddProfile(ctx, "Ada", domain.RoleUser, "h", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.SignInIdentity{Provider: "pocket-id", Subject: "sub-ada"}
	if err := s.LinkSignIn(ctx, ada.ID, id.Provider, id.Subject, "ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.KeepSignInToken(ctx, id, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE sign_in_accounts SET checked_at = now() - interval '2 hours'`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var taken atomic.Int32
	for range 4 {
		wg.Go(func() {
			due, err := s.SignInChecksDue(ctx, time.Hour)
			if err != nil {
				t.Error(err)
			}
			taken.Add(int32(len(due)))
		})
	}
	wg.Wait()
	if taken.Load() != 1 {
		t.Fatalf("the account was taken %d times; want once", taken.Load())
	}

	token := []byte("tv")
	if _, err := s.CreateSession(ctx, NewSession{
		Kind: domain.SessionDevice, ProfileID: ada.ID, TokenHash: token, ExpiresAt: new(time.Now().Add(time.Hour)), Identity: id,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.KeepSignInToken(ctx, id, "second"); err != nil {
		t.Fatal(err)
	}
	if ended, err := s.EndSignInSessions(ctx, SignInCheck{SignInIdentity: id, Token: "first"}); err != nil || ended {
		t.Errorf("a stale check ended the sessions: %v, %v", ended, err)
	}
	if _, _, err := s.SessionByToken(ctx, token, time.Now()); err != nil {
		t.Errorf("the session was signed out: %v", err)
	}
	if ended, err := s.EndSignInSessions(ctx, SignInCheck{SignInIdentity: id, Token: "second"}); err != nil || !ended {
		t.Errorf("a refused check left the sessions: %v, %v", ended, err)
	}
	if _, _, err := s.SessionByToken(ctx, token, time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("the session is still signed in: %v", err)
	}
}
