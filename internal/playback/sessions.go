package playback

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// sessionLife is how long a playback lasts with no word from its player; players report every
// ten seconds or so, as Jellyfin's and Plex's do.
const sessionLife = 2 * time.Minute

// ErrNoPlayback is a playback that has stopped, lapsed, or is another profile's.
var ErrNoPlayback = errors.New("no such playback")

type sessionStore interface {
	SavePlayback(ctx context.Context, p domain.Playback, ttl time.Duration) error
	Playback(ctx context.Context, id uuid.UUID) (domain.Playback, bool, error)
	EndPlayback(ctx context.Context, id uuid.UUID) error
}

type progressStore interface {
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position time.Duration) (domain.Reach, error)
}

// Sessions keeps who is playing what, and keeps each profile's place in it as they go.
type Sessions struct {
	live  sessionStore
	saved progressStore
	ended func(uuid.UUID)
}

// NewSessions keeps playbacks in live and places in saved, and calls ended as a playback stops,
// to let go of what it held.
func NewSessions(live sessionStore, saved progressStore, ended func(uuid.UUID)) *Sessions {
	return &Sessions{live: live, saved: saved, ended: ended}
}

// Start opens a playback of a copy of a title.
func (s *Sessions) Start(ctx context.Context, profile, item, version uuid.UUID, method domain.PlayMethod) (domain.Playback, error) {
	now := time.Now()
	p := domain.Playback{
		ID: uuid.NewV7(), Profile: profile, Item: item, Version: version, Method: method,
		State: domain.StatePlaying, Started: now, Updated: now,
	}
	return p, s.live.SavePlayback(ctx, p, sessionLife)
}

// Progress records where a profile's playback has got to, and how far through the title that is.
func (s *Sessions) Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState) (domain.Reach, error) {
	p, err := s.own(ctx, profile, id)
	if err != nil {
		return "", err
	}
	reach, err := s.saved.SaveProgress(ctx, profile, p.Item, position)
	if err != nil {
		return "", err
	}
	p.Position, p.State, p.Updated = position, state, time.Now()
	return reach, s.live.SavePlayback(ctx, p, sessionLife)
}

// Stop ends a profile's playback where it stopped.
func (s *Sessions) Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error) {
	p, err := s.own(ctx, profile, id)
	if err != nil {
		return "", err
	}
	reach, err := s.saved.SaveProgress(ctx, profile, p.Item, position)
	if err != nil {
		return "", err
	}
	s.ended(id)
	return reach, s.live.EndPlayback(ctx, id)
}

func (s *Sessions) own(ctx context.Context, profile, id uuid.UUID) (domain.Playback, error) {
	p, ok, err := s.live.Playback(ctx, id)
	if err != nil {
		return domain.Playback{}, err
	}
	if !ok || p.Profile != profile {
		return domain.Playback{}, ErrNoPlayback
	}
	return p, nil
}
