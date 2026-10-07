//go:build integration

package auth

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/kv"
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
	k, err := kv.Open(os.Getenv("TEST_VALKEY_URL"), uuid.NewV7())
	if err != nil {
		t.Fatalf("TEST_VALKEY_URL: %v", err)
	}
	t.Cleanup(k.Close)
	svc, err := New(t.Context(), st, k)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st
}

func addOliver(t *testing.T, st *store.Store) domain.Profile {
	t.Helper()
	hash, err := HashPassword(t.Context(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.AddProfile(t.Context(), "Oliver", domain.RoleAdmin, hash)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var tv = Device{Name: "Living room", Client: "Photon tvOS"}

func TestSignInAndOut(t *testing.T) {
	svc, st := newService(t)
	oliver := addOliver(t, st)

	for _, c := range []struct{ name, password string }{
		{"Oliver", "wrong"}, {"Nobody", "correct horse"},
	} {
		if _, _, err := svc.SignIn(t.Context(), c.name, c.password, tv); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("SignIn(%q, %q): err = %v, want %v", c.name, c.password, err, ErrInvalidCredentials)
		}
	}

	if _, profile, err := svc.SignIn(t.Context(), "oliver", "correct horse", tv); err != nil || profile != oliver {
		t.Errorf(`SignIn("oliver") = %+v, %v; want %+v`, profile, err, oliver)
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
	addOliver(t, st)
	token, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := st.TouchSession(t.Context(), session.ID, past, &past); err != nil {
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

func TestPairingATelevision(t *testing.T) {
	svc, st := newService(t)
	addOliver(t, st)
	_, phone, err := svc.SignIn(t.Context(), "Oliver", "correct horse", Device{Name: "iPhone", Client: "Photon iOS"})
	if err != nil {
		t.Fatal(err)
	}
	approver := domain.Session{Profile: phone}

	start, err := svc.StartPairing(t.Context(), tv)
	if err != nil {
		t.Fatal(err)
	}
	if len(start.UserCode) != 9 || strings.Trim(strings.ReplaceAll(start.UserCode, "-", ""), userCodeAlphabet) != "" {
		t.Errorf("user code %q is not XXXX-XXXX from %s", start.UserCode, userCodeAlphabet)
	}
	if _, err := svc.ApprovePairing(t.Context(), approver, "ZZZZ-ZZZZ"); start.UserCode != "ZZZZ-ZZZZ" && !errors.Is(err, ErrPairingNotFound) {
		t.Errorf("a code nobody was shown was approved (err %v)", err)
	}
	d, err := svc.ApprovePairing(t.Context(), approver, strings.ToLower(start.UserCode))
	if err != nil {
		t.Fatal(err)
	}
	if d != tv {
		t.Errorf("approved %+v, want the television that asked", d)
	}
	if _, err := svc.ApprovePairing(t.Context(), approver, start.UserCode); !errors.Is(err, ErrPairingNotFound) {
		t.Errorf("a code was approved twice (err %v)", err)
	}

	code, _, _ := strings.Cut(start.DeviceCode, ".")
	if state, _, _, _ := svc.PollPairing(t.Context(), code+".wrong-secret"); state != kv.PairingExpired {
		t.Errorf("a poll with the wrong secret answered %q", state)
	}
	state, token, profile, err := svc.PollPairing(t.Context(), start.DeviceCode)
	if err != nil || state != kv.PairingApproved || profile.Name != "Oliver" {
		t.Fatalf("poll after approval: %q, %+v, %v", state, profile, err)
	}
	if session, err := svc.Authenticate(t.Context(), token); err != nil || session.Profile.Name != "Oliver" {
		t.Errorf("the television's token: %+v, %v", session, err)
	}
	if state, _, _, _ := svc.PollPairing(t.Context(), start.DeviceCode); state != kv.PairingExpired {
		t.Errorf("an approved pairing was handed out twice (%q)", state)
	}
}

func TestPairingPollsAreRateLimited(t *testing.T) {
	svc, _ := newService(t)
	start, err := svc.StartPairing(t.Context(), tv)
	if err != nil {
		t.Fatal(err)
	}
	if state, _, _, _ := svc.PollPairing(t.Context(), start.DeviceCode); state != kv.PairingPending {
		t.Errorf("first poll: %q, want pending", state)
	}
	if state, _, _, _ := svc.PollPairing(t.Context(), start.DeviceCode); state != kv.PairingSlowDown {
		t.Errorf("an immediate second poll: %q, want slow_down", state)
	}
}

func TestSwitchingProfiles(t *testing.T) {
	svc, st := newService(t)
	admin := addOliver(t, st)
	hash, err := HashPassword(t.Context(), "kid's password")
	if err != nil {
		t.Fatal(err)
	}
	sam, err := st.AddProfile(t.Context(), "Sam", domain.RoleMember, hash)
	if err != nil {
		t.Fatal(err)
	}
	kid, err := st.AddProfile(t.Context(), "Kid", domain.RoleRestricted, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPIN(t.Context(), sam.ID, "4821"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"12", "1234567", "12a4"} {
		if err := svc.SetPIN(t.Context(), sam.ID, bad); !errors.Is(err, ErrPINNotDigits) {
			t.Errorf("SetPIN(%q): err = %v", bad, err)
		}
	}
	list, err := st.Profiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var locks []string
	for _, p := range list {
		locks = append(locks, p.Profile.Name+":"+string(p.Lock))
	}
	if got := strings.Join(locks, " "); got != "Kid:password Oliver:password Sam:pin" {
		t.Errorf("the switcher would show %s", got)
	}

	token, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv)
	if err != nil {
		t.Fatal(err)
	}
	session := func() domain.Session {
		t.Helper()
		s, err := svc.Authenticate(t.Context(), token)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	steps := []struct {
		name    string
		target  domain.Profile
		secret  string
		wantErr error
		landsOn domain.Profile
	}{
		{"to a profile with no PIN and no secret", kid, "", ErrWrongSecret, admin},
		{"to a profile with no PIN with its password", kid, "kid's password", nil, kid},
		{"to a PIN profile with its password", sam, "kid's password", ErrWrongSecret, kid},
		{"to a PIN profile with the wrong PIN", sam, "0000", ErrWrongSecret, kid},
		{"to a PIN profile with its PIN", sam, "4821", nil, sam},
		{"to the admin with a PIN-like guess", admin, "4821", ErrWrongSecret, sam},
		{"to the admin with no secret", admin, "", ErrWrongSecret, sam},
		{"to the admin with its password", admin, "correct horse", nil, admin},
	}
	for _, s := range steps {
		_, err := svc.SwitchProfile(t.Context(), session(), s.target.ID, s.secret)
		if !errors.Is(err, s.wantErr) {
			t.Errorf("%s: err = %v, want %v", s.name, err, s.wantErr)
		}
		if got := session().Profile; got != s.landsOn {
			t.Errorf("%s: the device is %s, want %s", s.name, got.Name, s.landsOn.Name)
		}
	}
}

func TestDevicesAreSeenAndSignedOutWithinTheirScope(t *testing.T) {
	svc, st := newService(t)
	addOliver(t, st)
	hash, err := HashPassword(t.Context(), "sam's password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddProfile(t.Context(), "Sam", domain.RoleMember, hash); err != nil {
		t.Fatal(err)
	}
	signIn := func(name, password, device string) domain.Session {
		t.Helper()
		token, _, err := svc.SignIn(t.Context(), name, password, Device{Name: device, Client: "Photon"})
		if err != nil {
			t.Fatal(err)
		}
		s, err := svc.Authenticate(t.Context(), token)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	admin := signIn("Oliver", "correct horse", "Mac")
	sam := signIn("Sam", "sam's password", "Sam's phone")
	samTV := signIn("Sam", "sam's password", "Sam's TV")

	count := func(s domain.Session) int {
		t.Helper()
		list, err := svc.Devices(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	if n := count(admin); n != 3 {
		t.Errorf("the admin sees %d devices, want 3", n)
	}
	if n := count(sam); n != 2 {
		t.Errorf("Sam sees %d devices, want Sam's 2", n)
	}
	if err := svc.SignOutDevice(t.Context(), sam, admin.ID); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("Sam signed out the admin's device (err %v)", err)
	}
	if err := svc.SignOutDevice(t.Context(), sam, samTV.ID); err != nil {
		t.Errorf("Sam could not sign out Sam's own TV: %v", err)
	}
	if err := svc.SignOutDevice(t.Context(), admin, sam.ID); err != nil {
		t.Errorf("the admin could not sign out Sam's phone: %v", err)
	}
	if n := count(admin); n != 1 {
		t.Errorf("%d devices left, want the admin's own", n)
	}
}

func TestChangingAPasswordSignsOutTheProfilesOtherDevices(t *testing.T) {
	svc, st := newService(t)
	addOliver(t, st)
	hash, err := HashPassword(t.Context(), "guest's password")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := st.AddProfile(t.Context(), "Guest", domain.RoleMember, hash)
	if err != nil {
		t.Fatal(err)
	}
	signIn := func(device string) (string, domain.Session) {
		t.Helper()
		token, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", Device{Name: device, Client: "Photon"})
		if err != nil {
			t.Fatal(err)
		}
		s, err := svc.Authenticate(t.Context(), token)
		if err != nil {
			t.Fatal(err)
		}
		return token, s
	}
	phoneToken, phone := signIn("Phone")
	tvToken, _ := signIn("TV")
	guestToken, guestTV := signIn("Guest's TV")
	if _, err := svc.SwitchProfile(t.Context(), guestTV, guest.ID, "guest's password"); err != nil {
		t.Fatal(err)
	}

	if err := svc.ChangePassword(t.Context(), phone, "guess", "battery staple"); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("with the wrong current password: err = %v, want %v", err, ErrWrongSecret)
	}
	if err := svc.ChangePassword(t.Context(), phone, "correct horse", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("to a short password: err = %v, want %v", err, ErrPasswordTooShort)
	}
	if _, err := svc.Authenticate(t.Context(), tvToken); err != nil {
		t.Fatalf("a refused change signed the TV out: %v", err)
	}

	if err := svc.ChangePassword(t.Context(), phone, "correct horse", "battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(t.Context(), phoneToken); err != nil {
		t.Errorf("the device that changed it was signed out: %v", err)
	}
	if _, err := svc.Authenticate(t.Context(), tvToken); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("the profile's other device is still signed in (err %v)", err)
	}
	if _, err := svc.Authenticate(t.Context(), guestToken); err != nil {
		t.Errorf("a device watching as another profile was signed out: %v", err)
	}
	if _, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("the old password still signs in (err %v)", err)
	}
	if _, _, err := svc.SignIn(t.Context(), "Oliver", "battery staple", tv); err != nil {
		t.Errorf("the new password does not sign in: %v", err)
	}
}

func TestAnAPIKeyActsAsItsAdminUntilRevoked(t *testing.T) {
	svc, st := newService(t)
	oliver := addOliver(t, st)
	token, _, err := svc.SignIn(t.Context(), "Oliver", "correct horse", tv)
	if err != nil {
		t.Fatal(err)
	}
	mac, err := svc.Authenticate(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	id, key, err := svc.CreateKey(t.Context(), mac, "Sonarr")
	if err != nil {
		t.Fatal(err)
	}
	s, err := svc.Authenticate(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if s.Profile != oliver || s.Kind != domain.SessionKey {
		t.Errorf("the key is %s's %s, want Oliver's key", s.Profile.Name, s.Kind)
	}
	if devices, err := svc.Devices(t.Context(), mac); err != nil || len(devices) != 1 {
		t.Errorf("%d devices listed (err %v), want the one signed in, not the key", len(devices), err)
	}
	if err := svc.SignOutDevice(t.Context(), mac, id); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("signing the key out as a device: err = %v, want %v", err, ErrDeviceNotFound)
	}
	if err := svc.ChangePassword(t.Context(), mac, "correct horse", "battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(t.Context(), key); err != nil {
		t.Errorf("changing the password revoked the key: %v", err)
	}
	keys, err := svc.Keys(t.Context())
	if err != nil || len(keys) != 1 || keys[0].Name != "Sonarr" || keys[0].Profile != "Oliver" {
		t.Errorf("keys = %+v (err %v), want Oliver's Sonarr", keys, err)
	}

	if err := svc.RevokeKey(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(t.Context(), key); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("a revoked key still authenticates (err %v)", err)
	}
	if err := svc.RevokeKey(t.Context(), id); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("revoking it again: err = %v, want %v", err, ErrKeyNotFound)
	}
}
