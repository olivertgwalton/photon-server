package domain

import (
	"regexp"
	"time"
)

// MarkerKind is a stretch of a copy a player may offer to skip.
type MarkerKind string

const (
	MarkerIntro   MarkerKind = "intro"
	MarkerCredits MarkerKind = "credits"
	// MarkerRecap and MarkerPreview are made only from chapters that name them, or by an admin.
	MarkerRecap   MarkerKind = "recap"
	MarkerPreview MarkerKind = "preview"
)

func MarkerKinds() []MarkerKind {
	return []MarkerKind{MarkerIntro, MarkerCredits, MarkerRecap, MarkerPreview}
}

// MarkerSource is where a marker came from, most trusted first.
type MarkerSource string

const (
	MarkerByUser MarkerSource = "user"
	// MarkerByChapter is the file's own chapter of that name: its author marked the boundary to
	// the frame, where a fingerprint only finds it to a fraction of a second either side.
	MarkerByChapter     MarkerSource = "chapter"
	MarkerByFingerprint MarkerSource = "fingerprint"
)

func MarkerSources() []MarkerSource {
	return []MarkerSource{MarkerByUser, MarkerByChapter, MarkerByFingerprint}
}

// Marker is a stretch of one part, in milliseconds from the part's start.
type Marker struct {
	Kind    MarkerKind
	StartMS int64
	EndMS   int64
}

// MarkerAbsent is an admin's word that a copy's part, counted from 0, has no stretch of a kind.
type MarkerAbsent struct {
	Kind MarkerKind
	Part int
}

// MarkerShortest is the shortest stretch found by chapter or fingerprint, as Intro Skipper's.
const MarkerShortest = 15 * time.Second

// Longest is the longest stretch of kind found by chapter or fingerprint, as Intro Skipper's: a
// longer one is more likely a scene shared by two episodes than an opening.
func (k MarkerKind) Longest() time.Duration {
	switch k {
	case MarkerIntro, MarkerRecap, MarkerPreview:
		return 2 * time.Minute
	case MarkerCredits:
		return 15 * time.Minute
	}
	panic("domain: marker kind without a length: " + string(k))
}

// chapterNames are Intro Skipper's chapter patterns. Go's regexp has no lookahead, so each name
// captures a following "End" (a chapter named "Intro End" starts the episode proper), and a match
// that captured one does not count.
var chapterNames = map[MarkerKind]*regexp.Regexp{
	MarkerIntro:   chapterName(`intro|introduction|op|opening`),
	MarkerCredits: chapterName(`credits?|ed|ending|outro`),
	MarkerRecap:   chapterName(`re?cap|sum{1,2}ary|prev(?:ious(?:ly)?)?|(?:last|earlier)(?:\s\w+)?|catch[ -]up`),
	MarkerPreview: chapterName(`preview|pv|sneak\s?peek|coming\s?(?:up|soon)|next\s+(?:time|on|episode)|extra|teaser|trailer`),
}

func chapterName(names string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|\s)(?:` + names + `)([\s:]+end)?(?:\s|:|$)`)
}

// ChapterMarker is the kind of stretch a chapter's title names, if any.
func ChapterMarker(title string) (MarkerKind, bool) {
	for _, k := range MarkerKinds() {
		if m := chapterNames[k].FindStringSubmatch(title); m != nil && m[1] == "" {
			return k, true
		}
	}
	return "", false
}
