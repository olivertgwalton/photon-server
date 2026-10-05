package domain

import (
	"testing"
	"time"
)

func TestReachOf(t *testing.T) {
	const film = 2 * time.Hour
	for _, tc := range []struct {
		position, duration time.Duration
		want               Reach
	}{
		{5 * time.Minute, film, ReachStart},
		{7 * time.Minute, film, ReachResumable},
		{time.Hour, film, ReachResumable},
		{109 * time.Minute, film, ReachEnd},
		{119*time.Minute + 30*time.Second, film, ReachEnd},
		{5 * time.Second, 4 * time.Minute, ReachStart},
		{2 * time.Minute, 4 * time.Minute, ReachEnd},
		{4 * time.Minute, 4 * time.Minute, ReachEnd},
		{10 * time.Minute, 0, ReachResumable},
	} {
		if got := ReachOf(tc.position, tc.duration); got != tc.want {
			t.Errorf("ReachOf(%v, %v) = %s, want %s", tc.position, tc.duration, got, tc.want)
		}
	}
}
