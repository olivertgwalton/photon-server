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

// Release is a published release of the server: its version, as its tag names it, and its page.
type Release struct {
	Version string
	URL     string
}
