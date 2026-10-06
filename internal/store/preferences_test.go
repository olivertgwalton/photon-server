//go:build integration

package store

import (
	"errors"
	"testing"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestAProfileKeepsItsPreferencesAndTheTracksItChose(t *testing.T) {
	s, film, _, profiles := downloadable(t)
	ctx := t.Context()
	oliver, ada := profiles[0], profiles[1]

	// Saved is not compared: when it was is the server's.
	same := cmp.Options{
		cmp.Comparer(func(a, b language.Tag) bool { return a == b }),
		cmpopts.IgnoreFields(domain.Preferences{}, "SavedAt"),
	}
	if got, err := s.Preferences(ctx, oliver); err != nil || !cmp.Equal(got, domain.DefaultPreferences(), same) {
		t.Fatalf("before any change: %+v, %v", got, err)
	}
	want := domain.DefaultPreferences()
	want.AudioLanguage, want.SubtitleLanguage = language.Japanese, language.MustParse("en-GB")
	want.SubtitleMode, want.MaxBitrateKbps, want.CreditsAction = domain.SubtitlesSmart, 8000, domain.SegmentSkip
	for range 2 {
		got, err := s.SetPreferences(ctx, oliver, want)
		if err != nil {
			t.Fatal(err)
		}
		if got.SavedAt.IsZero() || !cmp.Equal(got, want, same) {
			t.Errorf("kept %+v, want %+v", got, want)
		}
	}
	if got, _ := s.Preferences(ctx, ada); !got.SavedAt.IsZero() {
		t.Errorf("another profile's changed too: %+v", got)
	}
	if _, err := s.SetPreferences(ctx, uuid.NewV7(), want); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such profile: %v", err)
	}

	// What a report leaves out stays, and one subtitle said replaces the other.
	file := uuid.NewV7()
	for _, chose := range []domain.ChosenTracks{
		{Audio: new(2), Subtitle: new(3)},
		{SubtitleFile: &file},
		{Audio: new(1)},
	} {
		if err := s.ChooseTracks(ctx, oliver, film, chose); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ChosenTracks(ctx, oliver, film)
	if diff := cmp.Diff(domain.ChosenTracks{Audio: new(1), SubtitleFile: &file}, got); err != nil || diff != "" {
		t.Errorf("chosen (-want +got), %v:\n%s", err, diff)
	}
	if err := s.ChooseTracks(ctx, oliver, film, domain.ChosenTracks{Subtitle: new(domain.NoSubtitle)}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ChosenTracks(ctx, oliver, film)
	if diff := cmp.Diff(domain.ChosenTracks{Audio: new(1), Subtitle: new(domain.NoSubtitle)}, got); diff != "" {
		t.Errorf("subtitles off (-want +got):\n%s", diff)
	}
	if got, _ := s.ChosenTracks(ctx, ada, film); got != (domain.ChosenTracks{}) {
		t.Errorf("another profile's tracks: %+v", got)
	}
}
