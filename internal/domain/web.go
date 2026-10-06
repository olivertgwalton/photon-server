package domain

import (
	"fmt"
	"slices"
)

// Web is whether the server serves its web app beside the API.
type Web string

const (
	WebServe Web = "serve"
	WebOff   Web = "off"
)

func Webs() []Web {
	return []Web{WebServe, WebOff}
}

func ParseWeb(s string) (Web, error) {
	if w := Web(s); slices.Contains(Webs(), w) {
		return w, nil
	}
	return "", fmt.Errorf("web %q is not one of %v", s, Webs())
}
