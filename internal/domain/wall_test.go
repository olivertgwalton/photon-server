package domain

import "testing"

// A copy is named by the resolution a wall's filter finds it under, by its width, so a scope
// master is not taken for less than it is.
func TestResolutionOf(t *testing.T) {
	for width, want := range map[int]Resolution{
		720:  ResolutionSD,
		1280: ResolutionHD,
		1920: ResolutionFHD,
		1998: ResolutionFHD,
		3840: ResolutionUHD,
		4096: ResolutionUHD,
	} {
		if got := ResolutionOf(width); got != want {
			t.Errorf("ResolutionOf(%d) = %q, want %q", width, got, want)
		}
	}
}
