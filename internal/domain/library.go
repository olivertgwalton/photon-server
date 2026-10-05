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
}
