package analysis

import (
	"math/bits"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// How episodes' sound is compared, as Intro Skipper compares it by default.
const (
	// pointLength is the stretch of sound one chromaprint point stands for.
	pointLength = time.Second * 4096 / 33075
	// samePoint is how many of a point's 32 bits may differ for two points to sound alike.
	samePoint = 6
	// longestGap is how long two episodes may sound unalike inside one shared stretch.
	longestGap = 3500 * time.Millisecond
	// valueSlack is how far apart two points' values may be to be tried as the same moment.
	valueSlack = 2
	// introWindow is the longest opening an episode is fingerprinted over: a quarter of an
	// episode of shortEpisode or more, the whole of a shorter one.
	introWindow  = 10 * time.Minute
	shortEpisode = 5 * time.Minute
	// creditsWindow is how much of an episode's end is fingerprinted.
	creditsWindow = 450 * time.Second
	// startSnap is how close to the start an intro may begin and be taken to begin there.
	startSnap = 5 * time.Second
	// endSnap is how close to the end credits may finish and be taken to run to it: the last
	// second or so of sound makes no point of its own.
	endSnap = 5 * time.Second
)

// window is the stretch of a part of length fingerprinted for kind.
func window(kind domain.MarkerKind, length time.Duration) (from, span time.Duration) {
	if kind == domain.MarkerCredits {
		from = max(length-creditsWindow, 0)
		return from, length - from
	}
	if length < shortEpisode {
		return 0, length
	}
	return 0, min(length/4, introWindow)
}

// sound is one copy's fingerprint over a window of one of its parts.
type sound struct {
	episode uuid.UUID
	// from is where the window starts in the part, and length is the part's.
	from, length time.Duration
	points       []uint32
}

// shared finds the stretch of kind each sound shares with another episode's, as Intro Skipper
// does: each sound is compared with those after it until one shares a stretch of a plausible
// length, and a sound keeps the longest it is found to share. It answers nil for a sound that
// shares none.
func shared(kind domain.MarkerKind, prints []sound) []*domain.Marker {
	found := make([]*domain.Marker, len(prints))
	keep := func(i int, r run) {
		m := prints[i].place(kind, r)
		if found[i] == nil || m.EndMS-m.StartMS > found[i].EndMS-found[i].StartMS {
			found[i] = &m
		}
	}
	for i, a := range prints {
		for j := i + 1; j < len(prints); j++ {
			b := prints[j]
			if a.episode == b.episode {
				continue
			}
			ra, rb, ok := sharedRun(a.points, b.points)
			if !ok || longer(kind, a, ra) || longer(kind, b, rb) {
				continue
			}
			keep(i, ra)
			keep(j, rb)
			break
		}
	}
	return found
}

// longer reports whether a stretch is too long to be kind: credits are never the whole window,
// which two copies of one programme would share.
func longer(kind domain.MarkerKind, p sound, r run) bool {
	limit := kind.Longest()
	if kind == domain.MarkerCredits {
		limit = min(limit, p.length-p.from-time.Second)
	}
	return r.length() > limit
}

func (p sound) place(kind domain.MarkerKind, r run) domain.Marker {
	start, end := p.from+r.start(), p.from+r.end()
	if kind == domain.MarkerIntro && start <= startSnap {
		start = 0
	}
	if kind == domain.MarkerCredits && end >= p.length-endSnap {
		end = p.length
	}
	return domain.Marker{Kind: kind, StartMS: start.Milliseconds(), EndMS: end.Milliseconds()}
}

// run is a stretch of points, first to last inclusive.
type run struct{ first, last int }

func (r run) start() time.Duration  { return time.Duration(r.first) * pointLength }
func (r run) end() time.Duration    { return time.Duration(r.last+1) * pointLength }
func (r run) length() time.Duration { return r.end() - r.start() }

// sharedRun is the longest stretch two fingerprints share, at least domain.MarkerShortest long.
// The shifts tried are those at which a point of one has a nearly equal point in the other.
func sharedRun(a, b []uint32) (ra, rb run, ok bool) {
	at := map[uint32]int{}
	for i, p := range b {
		at[p] = i
	}
	shifts := map[int]bool{}
	for i, p := range a {
		for d := -valueSlack; d <= valueSlack; d++ {
			if j, found := at[p+uint32(d)]; found {
				shifts[j-i] = true
			}
		}
	}
	best := -1
	for shift := range shifts {
		r, found := alike(a, b, shift)
		if found && (best < 0 || r.last-r.first > ra.last-ra.first || r.last-r.first == ra.last-ra.first && shift < best) {
			ra, best, ok = r, shift, true
		}
	}
	if !ok || ra.length() < domain.MarkerShortest {
		return run{}, run{}, false
	}
	return ra, run{ra.first + best, ra.last + best}, true
}

// alike is the longest run of a whose points sound like b's shifted points later, allowing gaps
// of up to longestGap, in a's positions.
func alike(a, b []uint32, shift int) (run, bool) {
	gap := int(longestGap / pointLength)
	var best, cur run
	found, open := false, false
	for i := max(0, -shift); i < len(a) && i+shift < len(b); i++ {
		if bits.OnesCount32(a[i]^b[i+shift]) > samePoint {
			continue
		}
		if open && i-cur.last <= gap {
			cur.last = i
			continue
		}
		if open && (!found || cur.last-cur.first > best.last-best.first) {
			best, found = cur, true
		}
		cur, open = run{i, i}, true
	}
	if open && (!found || cur.last-cur.first > best.last-best.first) {
		best, found = cur, true
	}
	return best, found
}
