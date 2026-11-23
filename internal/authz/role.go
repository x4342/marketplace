// Package authz defines the three effective marketplace roles.
package authz

const (
	User      = "user"
	Moderator = "moderator"
	Admin     = "admin"
)

type Capability int

const (
	Staff Capability = iota
	Platform
)

func Allows(role string, capability Capability) bool {
	switch capability {
	case Staff:
		return role == Moderator || role == Admin
	case Platform:
		return role == Admin
	default:
		return false
	}
}

func ValidRole(role string) bool {
	return role == User || role == Moderator || role == Admin
}
