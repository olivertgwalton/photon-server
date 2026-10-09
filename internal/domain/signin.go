package domain

import (
	"time"
	"uuid"
)

// SignInMethod is how a device proves who it signs in as: a profile's password, or a pairing that a
// device already signed in approved.
type SignInMethod string

const (
	SignInPassword SignInMethod = "password"
	SignInPairing  SignInMethod = "pairing"
)

func SignInMethods() []SignInMethod {
	return []SignInMethod{SignInPassword, SignInPairing}
}

// Provisioning is who a sign-in provider signs in: only the accounts profiles linked there, or
// anyone it signs in, each new account given a profile of its own.
type Provisioning string

const (
	ProvisionLink   Provisioning = "link"
	ProvisionCreate Provisioning = "create"
)

func Provisionings() []Provisioning {
	return []Provisioning{ProvisionLink, ProvisionCreate}
}

// Recheck is when the server asks a sign-in provider whether an account may still sign in: each
// hour, by the refresh token it granted, so an account the provider disabled or that left the
// group signs its devices out; or only as it signs in, for a provider whose refresh tokens lapse
// sooner than the server could go unanswered, or that does not say.
type Recheck string

const (
	RecheckHourly   Recheck = "hourly"
	RecheckAtSignIn Recheck = "at_sign_in"
)

func Rechecks() []Recheck {
	return []Recheck{RecheckHourly, RecheckAtSignIn}
}

// SignInProvider is an OpenID Connect provider an admin registered the server on as a client, which
// the household signs in through. Slug names it in the address the provider sends a browser back
// to, and Group, when set, is the group an account must be in there to sign in here, read as it
// signs in. MaxAge, Unrated and Libraries are what a profile it makes may see, as a profile's
// access says it: every library where none are named.
type SignInProvider struct {
	Slug         string
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	Provisioning Provisioning
	Group        string
	Recheck      Recheck
	MaxAge       *int
	Unrated      Unrated
	Libraries    []uuid.UUID
}

// SignInIdentity is the account at a sign-in provider that a session was signed in by, or whose
// session approved the device's pairing: the session ends when the account is unlinked. Zero for a
// session nothing but a password began.
type SignInIdentity struct {
	Provider string
	Subject  string
}

// SignInAccount is the account a profile linked at a sign-in provider, named as the provider named
// it then.
type SignInAccount struct {
	Provider string
	Username string
	LinkedAt time.Time
}
