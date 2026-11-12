package playback

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"slices"
	"testing"
	"testing/synctest"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// thisNode is the node the tests' sessions are on, and start their playbacks on.
var thisNode = uuid.MustParse("0199b3c0-0000-7000-8000-0000000000e1")

// memory keeps playbacks as Valkey does, and the admins' stops beside them.
type memory struct {
	plays map[uuid.UUID]domain.Playback
	stops map[uuid.UUID]uuid.UUID
}

func newMemory() memory {
	return memory{plays: map[uuid.UUID]domain.Playback{}, stops: map[uuid.UUID]uuid.UUID{}}
}

func (m memory) KeepAdminStop(_ context.Context, id, profile uuid.UUID, _ time.Duration) error {
	m.stops[id] = profile
	return nil
}

func (m memory) AdminStop(_ context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	profile, ok := m.stops[id]
	return profile, ok, nil
}

func (m memory) ClaimPlayback(_ context.Context, p domain.Playback, _ time.Duration) (bool, error) {
	if _, ok := m.plays[p.ID]; ok {
		return false, nil
	}
	m.plays[p.ID] = p
	return true, nil
}

func (m memory) RenewPlayback(_ context.Context, p domain.Playback, _ time.Duration) (bool, error) {
	if _, ok := m.plays[p.ID]; !ok {
		return false, nil
	}
	m.plays[p.ID] = p
	return true, nil
}

func (m memory) SavePlayback(_ context.Context, p domain.Playback, _ time.Duration) error {
	m.plays[p.ID] = p
	return nil
}

func (m memory) Playback(_ context.Context, id uuid.UUID) (domain.Playback, bool, error) {
	p, ok := m.plays[id]
	return p, ok, nil
}

func (m memory) Playbacks(context.Context) ([]domain.Playback, error) {
	return slices.Collect(maps.Values(m.plays)), nil
}

func (m memory) EndPlayback(_ context.Context, id uuid.UUID) (bool, error) {
	_, ok := m.plays[id]
	delete(m.plays, id)
	return ok, nil
}

// served is the streams a node serves, by playback.
type served map[uuid.UUID]bool

func (s served) Playbacks() []uuid.UUID { return slices.Collect(maps.Keys(s)) }

func (s served) Close(id uuid.UUID) { delete(s, id) }

// positions keeps each title's place; gone is a title removed.
type positions map[uuid.UUID]time.Duration

var gone = uuid.MustParse("0199b3c0-0000-7000-8000-00000000d0e5")

func (positions) Length(context.Context, uuid.UUID) (time.Duration, error) { return 2 * time.Hour, nil }

func (p positions) SaveProgress(_ context.Context, _, item uuid.UUID, at, _ time.Duration, _ domain.Reach, _ *time.Time) (domain.Reach, error) {
	if item == gone {
		return "", store.ErrNotFound
	}
	p[item] = at
	return domain.ReachResumable, nil
}

// RecordPlay keeps a play as how far it got, under its playback's id.
func (positions) ChooseTracks(context.Context, uuid.UUID, uuid.UUID, domain.ChosenTracks) error {
	return nil
}

func (p positions) RecordPlay(_ context.Context, pb domain.Playback, _ time.Time, at time.Duration) error {
	p[pb.ID] = at
	return nil
}

func TestAPlaybackKeepsItsProfilesPlace(t *testing.T) {
	live, saved, streams := newMemory(), positions{}, served{}
	var told []domain.EventKind
	raise := func(_ context.Context, e domain.Event) { told = append(told, e.Kind) }
	s := NewSessions(live, saved, streams, raise, thisNode)
	ctx := t.Context()
	oliver, guest, film := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()

	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(oliver, film), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	s.Opened(ctx, p)
	streams[p.ID] = true
	if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePaused, domain.ChosenTracks{}); err != nil {
		t.Fatal(err)
	}
	if got := live.plays[p.ID]; got.Position != 20*time.Minute || got.State != domain.StatePaused || saved[film] != 20*time.Minute {
		t.Errorf("after progress: live %+v, saved %v; want both at 20 minutes, paused", got, saved[film])
	}
	if _, err := s.Progress(ctx, guest, p.ID, time.Hour, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("another profile reporting: %v, want ErrNoPlayback", err)
	}
	if _, err := s.Stop(ctx, oliver, p.ID, 25*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok := live.plays[p.ID]; ok || saved[film] != 25*time.Minute || streams[p.ID] {
		t.Errorf("after stop: still live %v, saved %v, stream open %v; want it gone, kept at 25 minutes, and closed", ok, saved[film], streams[p.ID])
	}
	if saved[p.ID] != 25*time.Minute {
		t.Errorf("history kept %v, want the play at 25 minutes", saved[p.ID])
	}
	if _, err := s.Progress(ctx, oliver, p.ID, time.Hour, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("reporting a stopped playback: %v, want ErrNoPlayback", err)
	}
	want := []domain.EventKind{
		domain.EventPlaybackStarted, domain.EventUserDataChanged, domain.EventPlaybackPaused,
		domain.EventUserDataChanged, domain.EventPlaybackStopped,
	}
	if !slices.Equal(told, want) {
		t.Errorf("told %v, want %v", told, want)
	}
}

func TestAResumedPlaybackStartsWhereItResumes(t *testing.T) {
	var told []domain.Event
	raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
	s := NewSessions(newMemory(), positions{}, served{}, raise, thisNode)
	ctx := t.Context()

	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(uuid.NewV7(), uuid.NewV7()), 20*time.Minute, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	s.Opened(ctx, p)
	shown := told[0].Details.(domain.PlaybackDetails).Playback
	if shown.PositionMS != (20 * time.Minute).Milliseconds() {
		t.Errorf("started at %dms, want 20 minutes in", shown.PositionMS)
	}
}

func card(profile, title uuid.UUID) domain.PlaybackCard {
	return domain.PlaybackCard{
		Profile: domain.PlaybackProfile{ID: profile}, Title: domain.PlaybackTitle{ID: title}, Version: domain.PlaybackVersion{ID: uuid.NewV7()},
	}
}

func TestAnAdminEndsAnyonesPlaybackWhereItGotTo(t *testing.T) {
	live, saved, streams := newMemory(), positions{}, served{}
	var told []domain.Event
	raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
	s := NewSessions(live, saved, streams, raise, thisNode)
	ctx := t.Context()
	guest, film := uuid.NewV7(), uuid.NewV7()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayRemux, card(guest, film), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	streams[p.ID] = true
	if _, err := s.Progress(ctx, guest, p.ID, 40*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); err != nil {
		t.Fatal(err)
	}
	if err := s.End(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := live.plays[p.ID]; ok || saved[p.ID] != 40*time.Minute || saved[film] != 40*time.Minute || streams[p.ID] {
		t.Errorf("after ending: still live %v, history %v, place %v, stream open %v; want it gone and kept at 40 minutes", ok, saved[p.ID], saved[film], streams[p.ID])
	}
	// Its player is told an admin stopped it, so it closes rather than opening it again as it does
	// a playback that lapsed; no other profile is told whose playback it was.
	if _, err := s.Progress(ctx, guest, p.ID, 41*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrStoppedByAdmin) {
		t.Errorf("its player reporting after: %v, want ErrStoppedByAdmin", err)
	}
	if _, err := s.Stop(ctx, guest, p.ID, 41*time.Minute); !errors.Is(err, ErrStoppedByAdmin) {
		t.Errorf("its player stopping after: %v, want ErrStoppedByAdmin", err)
	}
	if _, err := s.Progress(ctx, uuid.NewV7(), p.ID, 41*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("another profile reporting after: %v, want ErrNoPlayback", err)
	}
	if err := s.End(ctx, p.ID); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("ending it again: %v, want ErrNoPlayback", err)
	}
	last := told[len(told)-1]
	if shown, _ := last.Details.(domain.PlaybackDetails); last.Kind != domain.EventPlaybackStopped || shown.Playback.PositionMS != (40*time.Minute).Milliseconds() || shown.Playback.Title.ID != film || shown.StoppedBy != domain.StoppedByAdmin {
		t.Errorf("told %v %+v, want it stopped by an admin at 40 minutes", last.Kind, last.Details)
	}
}

// A file streamed to a playback played as it is is cut off as the playback stops, here or on
// another node, and is refused after.
func TestAStopCutsOffAFilePlayedAsItIs(t *testing.T) {
	live := newMemory()
	s := NewSessions(live, positions{}, served{}, func(context.Context, domain.Event) {}, thisNode)
	ctx := t.Context()
	guest := uuid.NewV7()
	here, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(guest, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(guest, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	cut := map[uuid.UUID]int{}
	for _, id := range []uuid.UUID{here.ID, elsewhere.ID} {
		if _, err := s.Serve(ctx, id, func() { cut[id]++ }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.End(ctx, here.ID); err != nil {
		t.Fatal(err)
	}
	if cut[here.ID] != 1 || cut[elsewhere.ID] != 0 {
		t.Errorf("cut %v after ending one, want only it cut", cut)
	}
	delete(live.plays, elsewhere.ID)
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if cut[elsewhere.ID] != 1 {
		t.Errorf("one stopped on another node cut %d times after a sweep, want once", cut[elsewhere.ID])
	}
	if _, err := s.Serve(ctx, here.ID, func() {}); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("streaming a stopped playback: %v, want ErrNoPlayback", err)
	}
}

// A node counts a file it streams as it is among the playbacks it serves, so a node stopping waits
// for it to play to its end as it waits for a remux, and stops waiting once it has.
func TestAFilePlayedAsItIsIsAmongANodesPlaybacks(t *testing.T) {
	remuxed := uuid.NewV7()
	s := NewSessions(newMemory(), positions{}, served{remuxed: true}, func(context.Context, domain.Event) {}, thisNode)
	ctx := t.Context()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(uuid.NewV7(), uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(p.ID); err != nil {
		t.Fatal(err)
	}
	done, err := s.Serve(ctx, p.ID, func() {})
	if err != nil {
		t.Fatal(err)
	}
	byID := cmpopts.SortSlices(uuid.UUID.Compare)
	if diff := cmp.Diff([]uuid.UUID{remuxed, p.ID}, s.Playbacks(), byID); diff != "" {
		t.Errorf("playbacks while the file streams, each once (-want +got):\n%s", diff)
	}
	done()
	s.Release(p.ID)
	if diff := cmp.Diff([]uuid.UUID{remuxed}, s.Playbacks(), byID); diff != "" {
		t.Errorf("playbacks once the file has played (-want +got):\n%s", diff)
	}
}

// Ending a node's streams, as a restore does, cuts off the files it streams as they are as well as
// its remuxes.
func TestEndingANodesStreamsCutsOffAFilePlayedAsItIs(t *testing.T) {
	remuxed := uuid.NewV7()
	streams := served{remuxed: true}
	s := NewSessions(newMemory(), positions{}, streams, func(context.Context, domain.Event) {}, thisNode)
	ctx := t.Context()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(uuid.NewV7(), uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	cut := 0
	if _, err := s.Serve(ctx, p.ID, func() { cut++ }); err != nil {
		t.Fatal(err)
	}
	for _, id := range s.Playbacks() {
		s.Close(id)
	}
	if cut != 1 || len(streams) != 0 || len(s.Playbacks()) != 0 {
		t.Errorf("ended: file cut %d times, remuxes left %v, playbacks left %v; want the file cut once and nothing left", cut, streams, s.Playbacks())
	}
}

// A playback of a title removed while it played is swept like any other, once: it is not kept to
// be swept, and fail, again.
func TestAPlaybackOfARemovedTitleIsSweptOnce(t *testing.T) {
	live := newMemory()
	var told []domain.Event
	s := NewSessions(live, positions{}, served{}, func(_ context.Context, e domain.Event) { told = append(told, e) }, thisNode)
	ctx := t.Context()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(uuid.NewV7(), gone), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	p.Updated = time.Now().Add(-time.Hour)
	live.plays[p.ID] = p
	if err := s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep = %v, want nothing to fail", err)
	}
	if _, ok := live.plays[p.ID]; ok || told[len(told)-1].Kind != domain.EventPlaybackStopped {
		t.Errorf("after the sweep: still live %v, last told %v; want it stopped and gone", ok, told[len(told)-1].Kind)
	}
}

// endsMeanwhile has the playback ended, once, while a report's place is being saved, and counts
// the plays kept in the history.
type endsMeanwhile struct {
	positions
	end   func()
	plays int
}

func (e *endsMeanwhile) SaveProgress(ctx context.Context, profile, item uuid.UUID, at, length time.Duration, before domain.Reach, when *time.Time) (domain.Reach, error) {
	if end := e.end; end != nil {
		e.end = nil
		end()
	}
	return e.positions.SaveProgress(ctx, profile, item, at, length, before, when)
}

func (e *endsMeanwhile) RecordPlay(ctx context.Context, p domain.Playback, stopped time.Time, at time.Duration) error {
	e.plays++
	return e.positions.RecordPlay(ctx, p, stopped, at)
}

// A report racing an admin's end does not bring the playback back, to be swept and kept in the
// history a second time, and its player is told an admin stopped it.
func TestAPlaybackEndedWhileItReportsStaysEnded(t *testing.T) {
	live, saved := newMemory(), &endsMeanwhile{positions: positions{}}
	var stopped int
	s := NewSessions(live, saved, served{}, func(_ context.Context, e domain.Event) {
		if e.Kind == domain.EventPlaybackStopped {
			stopped++
		}
	}, thisNode)
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(oliver, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	saved.end = func() {
		if err := s.End(ctx, p.ID); err != nil {
			t.Error(err)
		}
	}
	if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrStoppedByAdmin) {
		t.Errorf("reporting as it is ended: %v, want ErrStoppedByAdmin", err)
	}
	if _, ok := live.plays[p.ID]; ok {
		t.Error("the report brought the ended playback back")
	}
	// Were it back, its player's silence would see it swept in time.
	for id, p := range live.plays {
		p.Updated = time.Now().Add(-time.Hour)
		live.plays[id] = p
	}
	if err := s.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if saved.plays != 1 || stopped != 1 {
		t.Errorf("kept %d plays and told %d stops, want one of each", saved.plays, stopped)
	}
}

// superseded holds a newer place than any report: each is refused, as one from before the title's
// state last changed is.
type superseded struct{ positions }

func (superseded) SaveProgress(context.Context, uuid.UUID, uuid.UUID, time.Duration, time.Duration, domain.Reach, *time.Time) (domain.Reach, error) {
	return "", store.ErrSuperseded
}

// A report whose place is older than what is kept, as one racing another is, keeps no place but
// goes on all the same: the playback moves on, and its stop ends it and keeps the play, rather
// than failing and keeping it live to be swept, and refused, again.
func TestAPlaybackGoesOnPastASupersededPlace(t *testing.T) {
	live, saved := newMemory(), superseded{positions{}}
	var stopped int
	s := NewSessions(live, saved, served{}, func(_ context.Context, e domain.Event) {
		if e.Kind == domain.EventPlaybackStopped {
			stopped++
		}
	}, thisNode)
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(oliver, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePaused, domain.ChosenTracks{}); err != nil {
		t.Errorf("a superseded report: %v, want it taken", err)
	}
	if got := live.plays[p.ID]; got.Position != 20*time.Minute || got.State != domain.StatePaused {
		t.Errorf("after a superseded report: %+v, want the playback at 20 minutes, paused", got)
	}
	if _, err := s.Stop(ctx, oliver, p.ID, 25*time.Minute); err != nil {
		t.Errorf("a superseded stop: %v, want it stopped", err)
	}
	if _, ok := live.plays[p.ID]; ok || saved.positions[p.ID] != 25*time.Minute || stopped != 1 {
		t.Errorf("after a superseded stop: still live %v, play kept at %v, %d stops told; want it gone, its play at 25 minutes, one stop",
			ok, saved.positions[p.ID], stopped)
	}
}

// ends answers the end for every report, and keeps how far each report said the viewing had got.
type ends struct{ before []domain.Reach }

func (*ends) Length(context.Context, uuid.UUID) (time.Duration, error) { return time.Hour, nil }

func (e *ends) SaveProgress(_ context.Context, _, _ uuid.UUID, _, _ time.Duration, before domain.Reach, _ *time.Time) (domain.Reach, error) {
	e.before = append(e.before, before)
	return domain.ReachEnd, nil
}

func (*ends) RecordPlay(context.Context, domain.Playback, time.Time, time.Duration) error { return nil }

func (*ends) ChooseTracks(context.Context, uuid.UUID, uuid.UUID, domain.ChosenTracks) error {
	return nil
}

func TestAPlaybackTellsTheStoreItHasReachedTheEnd(t *testing.T) {
	saved := &ends{}
	s := NewSessions(newMemory(), saved, served{}, func(context.Context, domain.Event) {}, thisNode)
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(oliver, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.Progress(ctx, oliver, p.ID, time.Hour, domain.StatePlaying, domain.ChosenTracks{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Stop(ctx, oliver, p.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	want := []domain.Reach{"", domain.ReachEnd, domain.ReachEnd, domain.ReachEnd}
	if !slices.Equal(saved.before, want) {
		t.Errorf("reports said the viewing had got %v, want %v: only the first may count a play", saved.before, want)
	}
}

func TestAPausedPlayerThatKeepsReportingKeepsItsStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		remuxer, err := hls.NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: "ffmpeg"}}, t.TempDir(), t.TempDir(), hls.Hardware{Accel: domain.AccelSoftware}, hls.Unlimited, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		closing(t, remuxer)
		s := NewSessions(newMemory(), positions{}, remuxer, func(context.Context, domain.Event) {}, thisNode)
		ctx := t.Context()
		oliver := uuid.NewV7()
		p, err := s.Start(ctx, uuid.NewV7(), domain.PlayRemux, card(oliver, uuid.NewV7()), 0, thisNode)
		if err != nil {
			t.Fatal(err)
		}
		if err := remuxer.Open(ctx, p.ID, hls.Version{Parts: []hls.Source{{Open: func() (media.Input, error) { return media.Input{}, os.ErrNotExist }, Part: hls.Part{Duration: time.Hour, Keyframes: hls.Forced(time.Hour)}}}}); err != nil {
			t.Fatal(err)
		}
		// Paused for five minutes, saying so every ten seconds, with every node sweeping.
		for range 30 {
			synctest.Sleep(10 * time.Second)
			if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePaused, domain.ChosenTracks{}); err != nil {
				t.Fatal(err)
			}
			if err := s.Sweep(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); err != nil {
			t.Errorf("playing on after a five-minute pause: %v", err)
		}
		if _, err := remuxer.Playlist(p.ID, hls.MasterName); err != nil {
			t.Errorf("its playlist after a five-minute pause: %v, want its remux still there", err)
		}
	})
}

func TestAPlayerThatGoesQuietIsStoppedWithItsHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		live, saved, streams := newMemory(), positions{}, served{}
		var told []domain.Event
		raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
		s := NewSessions(live, saved, streams, raise, thisNode)
		ctx := t.Context()
		oliver, film := uuid.NewV7(), uuid.NewV7()
		p, err := s.Start(ctx, uuid.NewV7(), domain.PlayTranscode, card(oliver, film), 0, thisNode)
		if err != nil {
			t.Fatal(err)
		}
		streams[p.ID] = true
		// A stream this node serves of a playback another node has ended.
		elsewhere := uuid.NewV7()
		streams[elsewhere] = true
		if _, err := s.Progress(ctx, oliver, p.ID, 40*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); err != nil {
			t.Fatal(err)
		}
		synctest.Sleep(sessionLife - time.Second)
		if err := s.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		if _, ok := live.plays[p.ID]; !ok || !streams[p.ID] || streams[elsewhere] {
			t.Errorf("swept just short of its life: live %v, stream open %v, other stream open %v; want it kept, the other closed", ok, streams[p.ID], streams[elsewhere])
		}
		synctest.Sleep(time.Second)
		if err := s.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		if _, ok := live.plays[p.ID]; ok || streams[p.ID] || saved[p.ID] != 40*time.Minute || saved[film] != 40*time.Minute {
			t.Errorf("swept at its life: live %v, stream open %v, history %v, place %v; want it stopped at 40 minutes", ok, streams[p.ID], saved[p.ID], saved[film])
		}
		if last := told[len(told)-1]; last.Kind != domain.EventPlaybackStopped {
			t.Errorf("told %v, want it stopped", last.Kind)
		}
		if _, err := s.Progress(ctx, oliver, p.ID, 41*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrNoPlayback) {
			t.Errorf("its player back after: %v, want ErrNoPlayback", err)
		}
	})
}

// chosen keeps each choice of tracks written, after positions.
type chosen struct {
	positions
	writes []domain.ChosenTracks
}

func (c *chosen) ChooseTracks(_ context.Context, _, _ uuid.UUID, t domain.ChosenTracks) error {
	c.writes = append(c.writes, t)
	return nil
}

func TestAPlayerKeepsTheTracksItLastChose(t *testing.T) {
	saved := &chosen{positions: positions{}}
	s := NewSessions(newMemory(), saved, served{}, func(context.Context, domain.Event) {}, thisNode)
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(oliver, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	english, french, off := 1, 2, domain.NoSubtitle
	reports := []domain.ChosenTracks{
		{Audio: &english, Subtitle: &off},
		{Audio: &english, Subtitle: &off},
		{Audio: &french, Subtitle: &off},
		{Audio: &french, Subtitle: &off},
	}
	for i, tracks := range reports {
		if _, err := s.Progress(ctx, oliver, p.ID, time.Duration(i+1)*time.Minute, domain.StatePlaying, tracks); err != nil {
			t.Fatal(err)
		}
	}
	if len(saved.writes) != 2 || *saved.writes[0].Audio != english || *saved.writes[1].Audio != french {
		t.Errorf("tracks kept %+v, want English, then French once it was chosen", saved.writes)
	}
}

// timed answers how far each report got through a title of the length the playback carries.
type timed struct{ positions }

func (timed) SaveProgress(_ context.Context, _, _ uuid.UUID, at, length time.Duration, _ domain.Reach, _ *time.Time) (domain.Reach, error) {
	return domain.ReachOf(at, length), nil
}

func TestAProfileIsToldOfItsPlaceAsItsReachChanges(t *testing.T) {
	var told int
	raise := func(_ context.Context, e domain.Event) {
		if e.Kind == domain.EventUserDataChanged {
			told++
		}
	}
	s := NewSessions(newMemory(), timed{positions{}}, served{}, raise, thisNode)
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, uuid.NewV7(), domain.PlayDirect, card(oliver, uuid.NewV7()), 0, thisNode)
	if err != nil {
		t.Fatal(err)
	}
	// Two hours long: from the start, on to somewhere to resume, then the end.
	for _, at := range []time.Duration{0, time.Second, 20 * time.Minute, 21 * time.Minute, 22 * time.Minute, 119 * time.Minute, 119 * time.Minute} {
		if _, err := s.Progress(ctx, oliver, p.ID, at, domain.StatePlaying, domain.ChosenTracks{}); err != nil {
			t.Fatal(err)
		}
	}
	if told != 3 {
		t.Errorf("told %d times, want 3: as it started, became resumable and reached the end", told)
	}
}

// closing ends each of r's remuxes as the test does, before its folders are removed: Windows
// removes no folder a remux still holds open.
func closing(t *testing.T, r *hls.Remuxer) {
	t.Helper()
	t.Cleanup(func() {
		for _, p := range r.Playbacks() {
			r.Close(p)
		}
	})
}
