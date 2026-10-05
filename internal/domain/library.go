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

type Library struct {
	ID   uuid.UUID
	Name string
	Kind LibraryKind
	Root string
	// Sources are where its metadata may come from, most trusted first.
	Sources []FieldSource
}
