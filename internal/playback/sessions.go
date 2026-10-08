package playback

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// sessionLife is how long a playback lasts with no word from its player before it is ended, as
	// Jellyfin's session timeout ends one. Players report every ten seconds or so, paused as well,
	// as Jellyfin's and Plex's do.
	sessionLife = 2 * time.Minute
	// keptFor is how long Valkey keeps a playback with no word from its player: past its life, so
	// a sweep ends it with its history. It lapses on its own only with no node left to sweep.
	keptFor = 2 * sessionLife
)

// ErrNoPlayback is a playback that has stopped, lapsed, or is another profile's.
var ErrNoPlayback = errors.New("no such playback")

// ErrStarted is a playback started already, on this node or another, under the id asked for.
var ErrStarted = errors.New("the playback is started already")

type sessionStore interface {
	ClaimPlayback(ctx context.Context, p domain.Playback, ttl time.Duration) (bool, error)
	SavePlayback(ctx context.Context, p domain.Playback, ttl time.Duration) error
	Playback(ctx context.Context, id uuid.UUID) (domain.Playback, bool, error)
	Playbacks(ctx context.Context) ([]domain.Playback, error)
	EndPlayback(ctx context.Context, id uuid.UUID) (bool, error)
}

// streams are what a node serves its playbacks: their remuxes.
type streams interface {
	Playbacks() []uuid.UUID
	Close(playback uuid.UUID)
}

type progressStore interface {
	Length(ctx context.Context, item uuid.UUID) (time.Duration, error)
	SaveProgress(ctx context.Context, profile, item uuid.UUID, position, length time.Duration, before domain.Reach, at *time.Time) (domain.Reach, error)
	ChooseTracks(ctx context.Context, profile, item uuid.UUID, t domain.ChosenTracks) error
	RecordPlay(ctx context.Context, p domain.Playback, stopped time.Time, position time.Duration) error
}

// Sessions keeps who is playing what, and keeps each profile's place in it as they go.
//
// A playback is going on for as long as live holds it, on every node alike: from Start until it is
// stopped, ended by an admin, or swept for its player's silence. Its stream lives exactly as long.
type Sessions struct {
	live    sessionStore
	saved   progressStore
	streams streams
	raise   func(context.Context, domain.Event)
	node    uuid.UUID
	starts  *prometheus.CounterVec

	mu sync.Mutex
	// direct are the files this node streams of playbacks played as they are, each with what cuts
	// it off: a player holds one request open for the whole file, so refusing the next is not enough.
	direct map[uuid.UUID]map[*func()]bool
}

// NewSessions keeps playbacks in live and places in saved, and closes this node's streams of
// those that end; raise says as one starts, pauses, resumes and stops, and as each profile's place
// moves. Each playback started is node's to serve.
func NewSessions(live sessionStore, saved progressStore, st streams, raise func(context.Context, domain.Event), node uuid.UUID) *Sessions {
	return &Sessions{
		live: live, saved: saved, streams: st, raise: raise, node: node, starts: newStarts(),
		direct: map[uuid.UUID]map[*func()]bool{},
	}
}

// Start opens playback id, of the copy of a title its card names, by its card's profile, served by
// node, unless one is started under id already (ErrStarted).
func (s *Sessions) Start(ctx context.Context, id uuid.UUID, method domain.PlayMethod, card domain.PlaybackCard, node uuid.UUID) (domain.Playback, error) {
	length, err := s.saved.Length(ctx, card.Title.ID)
	if err != nil {
		return domain.Playback{}, err
	}
	now := time.Now()
	p := domain.Playback{
		ID: id, Profile: card.Profile.ID, Item: card.Title.ID, Version: card.Version.ID, Method: method,
		State: domain.StatePlaying, Started: now, Updated: now, Length: length, Node: node, Card: card,
	}
	claimed, err := s.live.ClaimPlayback(ctx, p, keptFor)
	if err != nil {
		return p, err
	}
	if !claimed {
		return p, ErrStarted
	}
	s.raise(ctx, event(domain.EventPlaybackStarted, p))
	return p, nil
}

// Progress records where a profile's playback has got to, and how far through the title that is,
// and the tracks its player says it plays with, as Jellyfin keeps them to play the title with again.
func (s *Sessions) Progress(ctx context.Context, profile, id uuid.UUID, position time.Duration, state domain.PlayState, tracks domain.ChosenTracks) (domain.Reach, error) {
	p, err := s.own(ctx, profile, id)
	if err != nil {
		return "", err
	}
	if !tracks.Equal(p.Tracks) {
		if err := s.saved.ChooseTracks(ctx, profile, p.Item, tracks); err != nil {
			return "", err
		}
		p.Tracks = tracks
	}
	reach, err := s.saved.SaveProgress(ctx, profile, p.Item, position, p.Length, p.Reached, nil)
	if err != nil {
		return "", err
	}
	// A place moving on within a title changes nothing a client shows but its progress, which
	// the player knows and the stop tells, as Jellyfin tells no progress.
	if reach != p.Last {
		s.raise(ctx, domain.Event{Kind: domain.EventUserDataChanged, Profile: profile, Item: p.Item})
	}
	if reach == domain.ReachEnd {
		p.Reached = reach
	}
	p.Last = reach
	was := p.State
	p.Position, p.State, p.Updated = position, state, time.Now()
	if err := s.live.SavePlayback(ctx, p, keptFor); err != nil {
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
	return s.stop(ctx, p, position)
}

// Finish stops a profile's own playback where its player last said it was, as a player does that
// moves on to another stream of the title without saying where it got to.
func (s *Sessions) Finish(ctx context.Context, profile, id uuid.UUID) (domain.Reach, error) {
	p, err := s.own(ctx, profile, id)
	if err != nil {
		return "", err
	}
	return s.stop(ctx, p, p.Position)
}

// End stops anyone's playback where its player last said it was, as an admin does from the
// dashboard; its player is refused from then on.
func (s *Sessions) End(ctx context.Context, id uuid.UUID) error {
	p, ok, err := s.live.Playback(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoPlayback
	}
	_, err = s.stop(ctx, p, p.Position)
	return err
}

// stop ends a playback once, whoever else ends it at the same time, and closes its stream at once
// where this node serves it; another node's sweep closes its own.
func (s *Sessions) stop(ctx context.Context, p domain.Playback, position time.Duration) (domain.Reach, error) {
	ended, err := s.live.EndPlayback(ctx, p.ID)
	if err != nil {
		return "", err
	}
	if !ended {
		return "", ErrNoPlayback
	}
	s.close(p.ID)
	p.Position = position
	reach, err := s.saved.SaveProgress(ctx, p.Profile, p.Item, position, p.Length, p.Reached, nil)
	if errors.Is(err, store.ErrNotFound) {
		// A title removed while it played leaves no place to keep, and its playback ends all the
		// same, rather than kept to be swept again for ever.
		reach, err = domain.ReachStart, nil
	}
	if err == nil {
		err = s.saved.RecordPlay(ctx, p, time.Now(), position)
	}
	if err != nil {
		// Kept again, so its player's stop or the sweep ends it with its history once Postgres answers.
		return "", errors.Join(err, s.live.SavePlayback(ctx, p, keptFor))
	}
	s.raise(ctx, domain.Event{Kind: domain.EventUserDataChanged, Profile: p.Profile, Item: p.Item})
	stopped := event(domain.EventPlaybackStopped, p)
	// How far it got says whether it was watched to the end, as Plex's media.scrobble does.
	stopped.Details["reach"] = reach
	s.raise(ctx, stopped)
	return reach, nil
}

// event tells of a playback as the dashboard lists it.
func event(kind domain.EventKind, p domain.Playback) domain.Event {
	return domain.Event{Kind: kind, Profile: p.Profile, Item: p.Item, Details: map[string]any{"playback": Showing(p)}}
}

// NowPlaying is a playback as an admin's dashboard shows it, in the list of playbacks, the event
// stream's snapshot and each playback event alike.
type NowPlaying struct {
	ID uuid.UUID `json:"id"`
	domain.PlaybackCard
	Method     domain.PlayMethod `json:"method"`
	State      domain.PlayState  `json:"state"`
	PositionMS int64             `json:"position_ms"`
	StartedAt  time.Time         `json:"started_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	NodeID     uuid.UUID         `json:"node_id"`
}

func Showing(p domain.Playback) NowPlaying {
	return NowPlaying{
		ID: p.ID, PlaybackCard: p.Card, Method: p.Method, State: p.State, PositionMS: p.Position.Milliseconds(),
		StartedAt: p.Started.UTC(), UpdatedAt: p.Updated.UTC(), NodeID: p.Node,
	}
}

// Abandon ends a playback whose stream could not be opened, before any of it was watched.
func (s *Sessions) Abandon(ctx context.Context, id uuid.UUID) error {
	_, err := s.live.EndPlayback(ctx, id)
	return err
}

// Sweep ends every playback whose player has said nothing for sessionLife where it last said it
// was, with its history and its stopped event, and closes the streams this node serves playbacks
// that have ended on any node. Every node sweeps; each playback is ended once.
func (s *Sessions) Sweep(ctx context.Context) error {
	// Read first: a stream opened after the playbacks are read is of a playback already among them.
	served := append(s.streams.Playbacks(), s.directPlaybacks()...)
	all, err := s.live.Playbacks(ctx)
	if err != nil {
		return err
	}
	going := map[uuid.UUID]bool{}
	var errs []error
	for _, p := range all {
		if time.Since(p.Updated) < sessionLife {
			going[p.ID] = true
			continue
		}
		if _, err := s.stop(ctx, p, p.Position); err != nil && !errors.Is(err, ErrNoPlayback) {
			errs = append(errs, err)
		}
	}
	for _, id := range served {
		if !going[id] {
			s.close(id)
		}
	}
	return errors.Join(errs...)
}

// Serve holds a file this node streams of a playback played as it is, until done; cut cuts it off
// as the playback ends. A playback that has ended is refused.
func (s *Sessions) Serve(ctx context.Context, id uuid.UUID, cut func()) (done func(), err error) {
	c := &cut
	s.mu.Lock()
	if s.direct[id] == nil {
		s.direct[id] = map[*func()]bool{}
	}
	s.direct[id][c] = true
	s.mu.Unlock()
	done = func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.direct[id], c)
		if len(s.direct[id]) == 0 {
			delete(s.direct, id)
		}
	}
	// Held before it is looked for, so a stop between the two still finds it to cut.
	_, ok, err := s.live.Playback(ctx, id)
	if err == nil && !ok {
		err = ErrNoPlayback
	}
	if err != nil {
		done()
		return nil, err
	}
	return done, nil
}

// close ends this node's streams of a playback: its remux, and the files it streams as they are.
func (s *Sessions) close(id uuid.UUID) {
	s.streams.Close(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	for cut := range s.direct[id] {
		(*cut)()
	}
	delete(s.direct, id)
}

func (s *Sessions) directPlaybacks() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Keys(s.direct))
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
