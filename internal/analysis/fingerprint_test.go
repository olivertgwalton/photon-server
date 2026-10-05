package analysis

import (
	"math/rand/v2"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// points is a stretch of sound: n random points, the same for the same seed.
func points(seed uint64, n int) []uint32 {
	r := rand.New(rand.NewPCG(seed, 0))
	out := make([]uint32, n)
	for i := range out {
		out[i] = r.Uint32()
	}
	return out
}

// noisy is p heard again: every third point a few bits off.
func noisy(p []uint32, seed uint64) []uint32 {
	r := rand.New(rand.NewPCG(seed, 1))
	out := append([]uint32(nil), p...)
	for i := 0; i < len(out); i += 3 {
		for range 3 {
			out[i] ^= 1 << r.IntN(32)
		}
	}
	return out
}

func at(d time.Duration) int { return int(d / pointLength) }

func join(parts ...[]uint32) []uint32 {
	var out []uint32
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func near(got int64, want time.Duration) bool {
	d := time.Duration(got)*time.Millisecond - want
	return d > -time.Second && d < time.Second
}

func TestAnIntroSharedBySomeEpisodes(t *testing.T) {
	theme := points(1, at(50*time.Second))
	cold := func(seed uint64, d time.Duration) []uint32 { return points(seed, at(d)) }
	prints := []sound{
		// A cold open, then the theme.
		{episode: uuid.NewV7(), length: time.Hour, points: join(cold(10, 2*time.Minute), theme, cold(11, 7*time.Minute))},
		// The theme first, heard through some noise.
		{episode: uuid.NewV7(), length: time.Hour, points: join(cold(12, 2*time.Second), noisy(theme, 2), cold(13, 9*time.Minute))},
		// No theme at all.
		{episode: uuid.NewV7(), length: time.Hour, points: cold(14, 10*time.Minute)},
		// Only ten seconds of it: too short to be an intro.
		{episode: uuid.NewV7(), length: time.Hour, points: join(cold(15, time.Minute), theme[:at(10*time.Second)], cold(16, 8*time.Minute))},
	}
	got := shared(domain.MarkerIntro, prints)
	if m := got[0]; m == nil || !near(m.StartMS, 2*time.Minute) || !near(m.EndMS, 2*time.Minute+50*time.Second) {
		t.Errorf("after a cold open: %+v, want 2:00 to 2:50", m)
	}
	if m := got[1]; m == nil || m.StartMS != 0 || !near(m.EndMS, 52*time.Second) {
		t.Errorf("a theme two seconds in: %+v, want it taken from the start to 0:52", m)
	}
	if got[2] != nil {
		t.Errorf("an episode with no theme: %+v, want none", got[2])
	}
	if got[3] != nil {
		t.Errorf("ten seconds of the theme: %+v, want none", got[3])
	}
}

func TestTwoCopiesOfOneEpisodeShareNoIntro(t *testing.T) {
	ep, same := uuid.NewV7(), points(3, at(30*time.Second))
	got := shared(domain.MarkerIntro, []sound{
		{episode: ep, length: time.Hour, points: join(same, points(4, 1000))},
		{episode: ep, length: time.Hour, points: join(same, points(5, 1000))},
	})
	if got[0] != nil || got[1] != nil {
		t.Errorf("two copies of one episode: %+v, want nothing", got)
	}
}

func TestCreditsRunningToTheEnd(t *testing.T) {
	const length = 44 * time.Minute
	from, span := window(domain.MarkerCredits, length)
	credits := points(6, at(90*time.Second))
	// The fingerprint stops short of the window's end, as chromaprint's does.
	tail := at(span) - len(credits) - 10
	prints := []sound{
		{episode: uuid.NewV7(), from: from, length: length, points: join(points(7, tail), credits)},
		{episode: uuid.NewV7(), from: from, length: length, points: join(points(8, tail), noisy(credits, 9))},
	}
	for i, m := range shared(domain.MarkerCredits, prints) {
		if m == nil || !near(m.StartMS, length-90*time.Second-10*pointLength) || m.EndMS != length.Milliseconds() {
			t.Errorf("episode %d: %+v, want the credits run to the end, %v", i, m, length)
		}
	}
}
