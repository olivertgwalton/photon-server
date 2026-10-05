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
}
