package playback

import (
	"context"
	"errors"
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

func TestAPlaybackKeepsItsProfilesPlace(t *testing.T) {
	live, saved := memory{}, positions{}
	s := NewSessions(live, saved)
	ctx := t.Context()
	oliver, guest, film := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()

	p, err := s.Start(ctx, oliver, film, uuid.NewV7(), domain.PlayDirect)
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
	if _, ok := live[p.ID]; ok || saved[film] != 25*time.Minute {
		t.Errorf("after stop: still live %v, saved %v; want it gone, kept at 25 minutes", ok, saved[film])
	}
	if _, err := s.Progress(ctx, oliver, p.ID, time.Hour, domain.StatePlaying); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("reporting a stopped playback: %v, want ErrNoPlayback", err)
	}
}
