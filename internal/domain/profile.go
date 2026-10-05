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
}
