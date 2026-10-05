package domain

import (
	"fmt"
	"slices"
	"uuid"
)

type LibraryKind string

const (
	LibraryMovies LibraryKind = "movies"
	LibraryShows  LibraryKind = "shows"
)

func LibraryKinds() []LibraryKind {
	return []LibraryKind{LibraryMovies, LibraryShows}
}

func ParseLibraryKind(s string) (LibraryKind, error) {
	if k := LibraryKind(s); slices.Contains(LibraryKinds(), k) {
		return k, nil
	}
	return "", fmt.Errorf("library kind %q is not one of %v", s, LibraryKinds())
}

// Monitor is whether a library is scanned as its files change, or only on the scan schedule.
type Monitor string

const (
	MonitorRealtime Monitor = "realtime"
	MonitorOff      Monitor = "off"
)

func Monitors() []Monitor {
	return []Monitor{MonitorRealtime, MonitorOff}
}

func ParseMonitor(s string) (Monitor, error) {
	if m := Monitor(s); slices.Contains(Monitors(), m) {
		return m, nil
	}
	return "", fmt.Errorf("monitor %q is not one of %v", s, Monitors())
}

// PreviewLevel is what pictures a library makes of its videos ahead of time: none, an image per
// chapter, or those and trickplay sheets for scrubbing.
type PreviewLevel string

const (
	PreviewsOff      PreviewLevel = "off"
	PreviewsChapters PreviewLevel = "chapters"
	PreviewsAll      PreviewLevel = "all"
)

func PreviewLevels() []PreviewLevel {
	return []PreviewLevel{PreviewsOff, PreviewsChapters, PreviewsAll}
}

func ParsePreviewLevel(s string) (PreviewLevel, error) {
	if l := PreviewLevel(s); slices.Contains(PreviewLevels(), l) {
		return l, nil
	}
	return "", fmt.Errorf("previews %q is not one of %v", s, PreviewLevels())
}

// MarkerDetection is how a library finds the intros and credits a player may offer to skip:
// not at all, only from chapters that name them, or from chapters and by comparing each season's
// sound too, which reads the start and end of every episode.
type MarkerDetection string

const (
	MarkersOff      MarkerDetection = "off"
	MarkersChapters MarkerDetection = "chapters"
	MarkersAll      MarkerDetection = "all"
)

func MarkerDetections() []MarkerDetection {
	return []MarkerDetection{MarkersOff, MarkersChapters, MarkersAll}
}

func ParseMarkerDetection(s string) (MarkerDetection, error) {
	if d := MarkerDetection(s); slices.Contains(MarkerDetections(), d) {
		return d, nil
	}
	return "", fmt.Errorf("markers %q is not one of %v", s, MarkerDetections())
}

// Keeps is whether a marker from source is offered under d. What an admin said always is.
func (d MarkerDetection) Keeps(source MarkerSource) bool {
	switch d {
	case MarkersOff:
		return source == MarkerByUser
	case MarkersChapters:
		return source != MarkerByFingerprint
	case MarkersAll:
		return true
	}
	panic("domain: unknown marker detection: " + string(d))
}

// KeyframeMode is how a library finds the keyframes a copied video's segments are cut at: from the
// container's own index alone, from the index or else by reading the whole file, or not at all. A
// file with none known is cut every segment length, at the keyframe after each.
type KeyframeMode string

const (
	KeyframesIndex KeyframeMode = "index"
	KeyframesFull  KeyframeMode = "full"
	KeyframesOff   KeyframeMode = "off"
)

func KeyframeModes() []KeyframeMode {
	return []KeyframeMode{KeyframesIndex, KeyframesFull, KeyframesOff}
}

func ParseKeyframeMode(s string) (KeyframeMode, error) {
	if m := KeyframeMode(s); slices.Contains(KeyframeModes(), m) {
		return m, nil
	}
	return "", fmt.Errorf("keyframes %q is not one of %v", s, KeyframeModes())
}

type Library struct {
	ID   uuid.UUID
	Name string
	Kind LibraryKind
	Root string
	// Sources are where its metadata may come from, most trusted first.
	Sources []FieldSource
	// RemoteExtras are the kinds of video it keeps links to from its providers.
	RemoteExtras []ExtraKind
	Monitor      Monitor
	// RefreshDays is how often its titles are matched again, in days; zero is never.
	RefreshDays int
	Previews    PreviewLevel
	Markers     MarkerDetection
}
