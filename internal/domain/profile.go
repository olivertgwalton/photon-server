package domain

import (
	"fmt"
	"slices"
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

func ParseRole(s string) (Role, error) {
	if r := Role(s); slices.Contains(Roles(), r) {
		return r, nil
	}
	return "", fmt.Errorf("role %q is not one of %v", s, Roles())
}

type Profile struct {
	ID   uuid.UUID
	Name string
	Role Role
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
