//go:build integration

package store

import (
	"encoding/json"
	"errors"
	"strconv"
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
	other, err := s.Preferences(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	if !other.SavedAt.IsZero() {
		t.Errorf("another profile's changed too: %+v", other)
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
	got, err = s.ChosenTracks(ctx, oliver, film)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(domain.ChosenTracks{Audio: new(1), Subtitle: new(domain.NoSubtitle)}, got); diff != "" {
		t.Errorf("subtitles off (-want +got):\n%s", diff)
	}
	got, err = s.ChosenTracks(ctx, ada, film)
	if err != nil {
		t.Fatal(err)
	}
	if got != (domain.ChosenTracks{}) {
		t.Errorf("another profile's tracks: %+v", got)
	}
}

// Each of a profile's apps keeps how it lays out each view, as it sent it, apart from every other
// app's and profile's.
func TestAProfileKeepsHowEachAppLaysOutItsViews(t *testing.T) {
	s, _, _, profiles := downloadable(t)
	ctx := t.Context()
	oliver, ada := profiles[0], profiles[1]
	if _, err := s.DisplayPreferences(ctx, oliver, "emby", "usersettings"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("never saved: %v, want ErrNotFound", err)
	}
	for _, sent := range []string{`{"SortBy":"SortName","CustomPrefs":{"homesection0":"resume"}}`, `{"SortBy":"DateCreated"}`} {
		if err := s.SetDisplayPreferences(ctx, oliver, "emby", "usersettings", json.RawMessage(sent)); err != nil {
			t.Fatal(err)
		}
		got, err := s.DisplayPreferences(ctx, oliver, "emby", "usersettings")
		var kept, want any
		if err != nil || json.Unmarshal(got, &kept) != nil || json.Unmarshal([]byte(sent), &want) != nil || !cmp.Equal(kept, want) {
			t.Errorf("kept %s, %v, want %s", got, err, sent)
		}
	}
	for _, other := range []struct {
		profile      uuid.UUID
		client, view string
	}{{oliver, "Jellyfin Web", "usersettings"}, {oliver, "emby", "home"}, {ada, "emby", "usersettings"}} {
		if _, err := s.DisplayPreferences(ctx, other.profile, other.client, other.view); !errors.Is(err, ErrNotFound) {
			t.Errorf("%+v: %v, want ErrNotFound", other, err)
		}
	}
	if err := s.SetDisplayPreferences(ctx, uuid.NewV7(), "emby", "usersettings", json.RawMessage(`{}`)); !errors.Is(err, ErrNotFound) {
		t.Errorf("no such profile: %v", err)
	}
}

// A profile's apps keep the layout of maxDisplayViews views between them: one more is refused,
// whichever app names it, while each kept goes on changing and another profile keeps its own.
func TestAProfilesAppsKeepTheLayoutOfSoManyViews(t *testing.T) {
	s, _, _, profiles := downloadable(t)
	ctx := t.Context()
	oliver, ada := profiles[0], profiles[1]
	for n := range maxDisplayViews {
		if err := s.SetDisplayPreferences(ctx, oliver, "emby", strconv.Itoa(n), json.RawMessage(`{}`)); err != nil {
			t.Fatalf("view %d: %v", n, err)
		}
	}
	for _, client := range []string{"emby", "Jellyfin Web"} {
		if err := s.SetDisplayPreferences(ctx, oliver, client, "one more", json.RawMessage(`{}`)); !errors.Is(err, ErrTooManyViews) {
			t.Errorf("%s's view beyond the limit: %v, want ErrTooManyViews", client, err)
		}
	}
	if err := s.SetDisplayPreferences(ctx, oliver, "emby", "0", json.RawMessage(`{"SortBy":"DateCreated"}`)); err != nil {
		t.Errorf("changing a view kept: %v", err)
	}
	if got, err := s.DisplayPreferences(ctx, oliver, "emby", "0"); err != nil || string(got) != `{"SortBy": "DateCreated"}` {
		t.Errorf("the view changed: %s, %v", got, err)
	}
	if err := s.SetDisplayPreferences(ctx, ada, "emby", "one more", json.RawMessage(`{}`)); err != nil {
		t.Errorf("another profile's view: %v", err)
	}
}
