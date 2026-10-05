package playback

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type memory map[uuid.UUID]domain.Playback

func (m memory) SavePlayback(_ context.Context, p domain.Playback, _ time.Duration) error {
	m[p.ID] = p
	return nil
}

func (m memory) Playback(_ context.Context, id uuid.UUID) (domain.Playback, bool, error) {
	p, ok := m[id]
	return p, ok, nil
}

func (m memory) EndPlayback(_ context.Context, id uuid.UUID) error {
	delete(m, id)
	return nil
}

type positions map[uuid.UUID]time.Duration

func (p positions) SaveProgress(_ context.Context, _, item uuid.UUID, at time.Duration) (domain.Reach, error) {
	p[item] = at
	return domain.ReachResumable, nil
}

// RecordPlay keeps a play as how far it got, under its playback's id.
func (p positions) RecordPlay(_ context.Context, pb domain.Playback, _ time.Time, at time.Duration) error {
	p[pb.ID] = at
	return nil
}

func TestAPlaybackKeepsItsProfilesPlace(t *testing.T) {
	live, saved := memory{}, positions{}
	var ended []uuid.UUID
	var told []domain.EventKind
	raise := func(_ context.Context, e domain.Event) { told = append(told, e.Kind) }
	s := NewSessions(live, saved, func(id uuid.UUID) { ended = append(ended, id) }, raise, uuid.NewV7())
	ctx := t.Context()
	oliver, guest, film := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()

	p, err := s.Start(ctx, domain.PlayDirect, card(oliver, film))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePaused); err != nil {
		t.Fatal(err)
	}
	if got := live[p.ID]; got.Position != 20*time.Minute || got.State != domain.StatePaused || saved[film] != 20*time.Minute {
		t.Errorf("after progress: live %+v, saved %v; want both at 20 minutes, paused", got, saved[film])
	}
	if _, err := s.Progress(ctx, guest, p.ID, time.Hour, domain.StatePlaying); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("another profile reporting: %v, want ErrNoPlayback", err)
	}
	if _, err := s.Stop(ctx, oliver, p.ID, 25*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok := live[p.ID]; ok || saved[film] != 25*time.Minute || len(ended) != 1 || ended[0] != p.ID {
		t.Errorf("after stop: still live %v, saved %v, ended %v; want it gone, kept at 25 minutes, and let go", ok, saved[film], ended)
	}
	if saved[p.ID] != 25*time.Minute {
		t.Errorf("history kept %v, want the play at 25 minutes", saved[p.ID])
	}
	if _, err := s.Progress(ctx, oliver, p.ID, time.Hour, domain.StatePlaying); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("reporting a stopped playback: %v, want ErrNoPlayback", err)
	}
	want := []domain.EventKind{domain.EventPlaybackStarted, domain.EventPlaybackPaused, domain.EventPlaybackStopped}
	if !slices.Equal(told, want) {
		t.Errorf("told %v, want %v", told, want)
	}
}

func card(profile, title uuid.UUID) domain.PlaybackCard {
	return domain.PlaybackCard{
		Profile: domain.PlaybackProfile{ID: profile}, Title: domain.PlaybackTitle{ID: title}, Version: domain.PlaybackVersion{ID: uuid.NewV7()},
	}
}
