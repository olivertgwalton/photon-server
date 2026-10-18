package domain

// ReleaseCheck is whether the server asks GitHub for a newer release of itself.
type ReleaseCheck string

const (
	ReleaseCheckOn  ReleaseCheck = "on"
	ReleaseCheckOff ReleaseCheck = "off"
)

func ReleaseChecks() []ReleaseCheck {
	return []ReleaseCheck{ReleaseCheckOn, ReleaseCheckOff}
}
