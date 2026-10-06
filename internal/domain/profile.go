package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
	"uuid"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	// RoleRestricted is a profile whose library and rating limits apply; set up for children.
	RoleRestricted Role = "restricted"
)

func Roles() []Role {
	return []Role{RoleAdmin, RoleMember, RoleRestricted}
}

type Profile struct {
	ID   uuid.UUID
	Name string
	Role Role
	// Avatar is its picture's id, served as any picture is; zero for none.
	Avatar uuid.UUID
}

// Session is a signed-in device and the profile it is watching as.
type Session struct {
	ID      uuid.UUID
	Profile Profile
	// Device and Client are what the device called itself and its app when it signed in.
	Device string
	Client string
}

// ProfileLock is what switching to a profile asks for.
type ProfileLock string

const (
	LockNone     ProfileLock = "none"
	LockPIN      ProfileLock = "pin"
	LockPassword ProfileLock = "password"
)

func ProfileLocks() []ProfileLock {
	return []ProfileLock{LockNone, LockPIN, LockPassword}
}

// Lock is what a profile asks for: an admin its password always, so a household profile cannot
// become an admin by switching; anyone else their PIN, if they set one.
func Lock(role Role, hasPIN bool) ProfileLock {
	switch {
	case role == RoleAdmin:
		return LockPassword
	case hasPIN:
		return LockPIN
	}
	return LockNone
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
