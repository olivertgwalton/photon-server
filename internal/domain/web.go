package domain

// Web is whether the server serves its web app beside the API.
type Web string

const (
	WebServe Web = "serve"
	WebOff   Web = "off"
)

func Webs() []Web {
	return []Web{WebServe, WebOff}
}
