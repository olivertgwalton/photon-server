package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
	"uuid"
)

type Role string

const (
	RoleAdmin Role = "admin"
	// RoleManager adds users and keeps the ones it added, granting them no more than it may see
	// itself; the rest of the server is the admin's.
	RoleManager Role = "manager"
	// RoleUser watches: what it sees is what its access allows.
	RoleUser Role = "user"
)

func Roles() []Role {
	return []Role{RoleAdmin, RoleManager, RoleUser}
}

type Profile struct {
	ID   uuid.UUID
	Name string
	Role Role
	// Avatar is its picture's id, served as any picture is; zero for none.
	Avatar uuid.UUID
	// Manager is the manager that added and keeps it; zero for one the admin keeps.
	Manager uuid.UUID
}

// SessionKind is how a session began: a device signing in, or an admin making an API key.
type SessionKind string

const (
	SessionDevice SessionKind = "device"
	// SessionKey is an API key: it acts as the admin who made it and never lapses.
	SessionKey SessionKind = "key"
)

func SessionKinds() []SessionKind {
	return []SessionKind{SessionDevice, SessionKey}
}

// Session is a signed-in device, or an API key, and the profile it is watching as.
type Session struct {
	ID      uuid.UUID
	Kind    SessionKind
	Profile Profile
	// Device and Client are what the device called itself and its app when it signed in.
	Device string
	Client string
}

// ProfileLock is what switching to a profile asks for.
type ProfileLock string

const (
	LockPIN      ProfileLock = "pin"
	LockPassword ProfileLock = "password"
)

func ProfileLocks() []ProfileLock {
	return []ProfileLock{LockPIN, LockPassword}
}

// Lock is what a profile asks for: its PIN if it set one, else its password. An admin or a manager
// asks for its password always, so a household profile cannot come to keep others by guessing a
// few digits.
func Lock(role Role, hasPIN bool) ProfileLock {
	switch role {
	case RoleAdmin, RoleManager:
		return LockPassword
	case RoleUser:
	}
	if hasPIN {
		return LockPIN
	}
	return LockPassword
}

// Unrated is whether a profile with an age limit sees titles no certificate rates.
type Unrated string

const (
	UnratedAllow Unrated = "allow"
	UnratedBlock Unrated = "block"
)

func UnratedPolicies() []Unrated {
	return []Unrated{UnratedAllow, UnratedBlock}
}

// MaxProfileName is the longest a profile's name may be, in characters.
const MaxProfileName = 64

// ProfileName is a name a profile may have, trimmed, as Jellyfin's user names are: something
// besides space, and no control characters.
func ProfileName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, s != "" && utf8.RuneCountInString(s) <= MaxProfileName && !strings.ContainsFunc(s, unicode.IsControl)
}
