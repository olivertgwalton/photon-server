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
	RecordPlay(ctx context.Context, p domain.Playback, stopped time.Time, position time.Duration) error
}

// Sessions keeps who is playing what, and keeps each profile's place in it as they go.
type Sessions struct {
	live  sessionStore
	saved progressStore
	ended func(uuid.UUID)
	raise func(context.Context, domain.Event)
	node  uuid.UUID
}

// NewSessions keeps playbacks in live and places in saved, and calls ended as a playback stops,
// to let go of what it held; raise says as one starts, pauses, resumes and stops. Each playback
// started is node's to serve.
func NewSessions(live sessionStore, saved progressStore, ended func(uuid.UUID), raise func(context.Context, domain.Event), node uuid.UUID) *Sessions {
	return &Sessions{live: live, saved: saved, ended: ended, raise: raise, node: node}
}

// Start opens a playback of a copy of a title.
func (s *Sessions) Start(ctx context.Context, profile, item, version uuid.UUID, method domain.PlayMethod) (domain.Playback, error) {
	now := time.Now()
	p := domain.Playback{
		ID: uuid.NewV7(), Profile: profile, Item: item, Version: version, Method: method,
		State: domain.StatePlaying, Started: now, Updated: now, Node: s.node,
	}
	if err := s.live.SavePlayback(ctx, p, sessionLife); err != nil {
		return p, err
	}
	s.raise(ctx, event(domain.EventPlaybackStarted, p))
	return p, nil
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
	was := p.State
	p.Position, p.State, p.Updated = position, state, time.Now()
	if err := s.live.SavePlayback(ctx, p, sessionLife); err != nil {
		return "", err
	}
	switch {
	case was == state:
	case state == domain.StatePaused:
		s.raise(ctx, event(domain.EventPlaybackPaused, p))
	case state == domain.StatePlaying:
		s.raise(ctx, event(domain.EventPlaybackResumed, p))
	}
	return reach, nil
}

// Stop ends a profile's playback where it stopped, and keeps it in the history.
func (s *Sessions) Stop(ctx context.Context, profile, id uuid.UUID, position time.Duration) (domain.Reach, error) {
	p, err := s.own(ctx, profile, id)
	if err != nil {
		return "", err
	}
	reach, err := s.saved.SaveProgress(ctx, profile, p.Item, position)
	if err != nil {
		return "", err
	}
	if err := s.saved.RecordPlay(ctx, p, time.Now(), position); err != nil {
		return "", err
	}
	s.ended(id)
	if err := s.live.EndPlayback(ctx, id); err != nil {
		return "", err
	}
	p.Position = position
	stopped := event(domain.EventPlaybackStopped, p)
	// How far it got says whether it was watched to the end, as Plex's media.scrobble does.
	stopped.Details["reach"] = reach
	s.raise(ctx, stopped)
	return reach, nil
}

func event(kind domain.EventKind, p domain.Playback) domain.Event {
	return domain.Event{Kind: kind, Profile: p.Profile, Item: p.Item, Details: map[string]any{
		"playback_id": p.ID, "method": p.Method, "position_ms": p.Position.Milliseconds(),
	}}
}

// Abandon ends a playback whose stream could not be opened, before any of it was watched.
func (s *Sessions) Abandon(ctx context.Context, id uuid.UUID) error {
	return s.live.EndPlayback(ctx, id)
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
