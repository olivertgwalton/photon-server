//go:build integration

package main

import (
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/store/storetest"
)

// A library added from the command line is scanned as it was asked to be read, without waiting for
// the schedule.
func TestALibraryAddedIsScannedWithItsSettings(t *testing.T) {
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	args := []string{"-name", "Films", "-kind", "movies", "-previews", "off", "-keyframes", "full", t.TempDir()}
	if err := addLibrary(t.Context(), st, io.Discard, args); err != nil {
		t.Fatal(err)
	}
	libs, err := st.Libraries(t.Context())
	if err != nil || len(libs) != 1 {
		t.Fatalf("libraries %v, %v", libs, err)
	}
	if libs[0].Previews != domain.PreviewsOff || libs[0].Keyframes != domain.KeyframesFull {
		t.Errorf("library %+v, want previews off and keyframes read whole where there is no index", libs[0])
	}
	counts, _, err := st.JobQueue(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(counts, store.JobCount{Kind: domain.JobScanLibrary, State: domain.JobQueued, Count: 1}) {
		t.Errorf("jobs %+v, want its scan queued", counts)
	}
}

// Each kind's sources are set from the command line and listed as they are given.
func TestALibrarysSourcesAreSetPerKind(t *testing.T) {
	url := storetest.FreshDatabase(t)
	log := slog.New(slog.DiscardHandler)
	if err := store.Migrate(t.Context(), url, log); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := addLibrary(t.Context(), st, io.Discard, []string{"-name", "TV", "-kind", "shows", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := setLibrary(t.Context(), st, io.Discard, []string{"-name", "TV", "-metadata", "movie=tmdb"}); err == nil {
		t.Error("a shows library took sources for films")
	}
	args := []string{"-name", "TV", "-metadata", "episode=nfo,tvdb,tmdb", "-images", "show=tvdb;season="}
	if err := setLibrary(t.Context(), st, io.Discard, args); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := listLibraries(t.Context(), st, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"show=nfo,tmdb;season=nfo,tmdb;episode=nfo,tvdb,tmdb", "show=tvdb;season=;episode=tmdb"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("listed:\n%s\nwant %s", out.String(), want)
		}
	}
}
