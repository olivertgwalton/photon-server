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

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
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

func (m memory) Playbacks(context.Context) ([]domain.Playback, error) {
	return slices.Collect(maps.Values(m)), nil
}

func (m memory) EndPlayback(_ context.Context, id uuid.UUID) (bool, error) {
	_, ok := m[id]
	delete(m, id)
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
	live, saved, streams := memory{}, positions{}, served{}
	var told []domain.EventKind
	raise := func(_ context.Context, e domain.Event) { told = append(told, e.Kind) }
	s := NewSessions(live, saved, streams, raise, uuid.NewV7())
	ctx := t.Context()
	oliver, guest, film := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()

	p, err := s.Start(ctx, domain.PlayDirect, card(oliver, film))
	if err != nil {
		t.Fatal(err)
	}
	streams[p.ID] = true
	if _, err := s.Progress(ctx, oliver, p.ID, 20*time.Minute, domain.StatePaused, domain.ChosenTracks{}); err != nil {
		t.Fatal(err)
	}
	if got := live[p.ID]; got.Position != 20*time.Minute || got.State != domain.StatePaused || saved[film] != 20*time.Minute {
		t.Errorf("after progress: live %+v, saved %v; want both at 20 minutes, paused", got, saved[film])
	}
	if _, err := s.Progress(ctx, guest, p.ID, time.Hour, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("another profile reporting: %v, want ErrNoPlayback", err)
	}
	if _, err := s.Stop(ctx, oliver, p.ID, 25*time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok := live[p.ID]; ok || saved[film] != 25*time.Minute || streams[p.ID] {
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

func card(profile, title uuid.UUID) domain.PlaybackCard {
	return domain.PlaybackCard{
		Profile: domain.PlaybackProfile{ID: profile}, Title: domain.PlaybackTitle{ID: title}, Version: domain.PlaybackVersion{ID: uuid.NewV7()},
	}
}

func TestAnAdminEndsAnyonesPlaybackWhereItGotTo(t *testing.T) {
	live, saved, streams := memory{}, positions{}, served{}
	var told []domain.Event
	raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
	s := NewSessions(live, saved, streams, raise, uuid.NewV7())
	ctx := t.Context()
	guest, film := uuid.NewV7(), uuid.NewV7()
	p, err := s.Start(ctx, domain.PlayRemux, card(guest, film))
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
	if _, ok := live[p.ID]; ok || saved[p.ID] != 40*time.Minute || saved[film] != 40*time.Minute || streams[p.ID] {
		t.Errorf("after ending: still live %v, history %v, place %v, stream open %v; want it gone and kept at 40 minutes", ok, saved[p.ID], saved[film], streams[p.ID])
	}
	if _, err := s.Progress(ctx, guest, p.ID, 41*time.Minute, domain.StatePlaying, domain.ChosenTracks{}); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("its player reporting after: %v, want ErrNoPlayback", err)
	}
	if err := s.End(ctx, p.ID); !errors.Is(err, ErrNoPlayback) {
		t.Errorf("ending it again: %v, want ErrNoPlayback", err)
	}
	last := told[len(told)-1]
	if shown, _ := last.Details["playback"].(NowPlaying); last.Kind != domain.EventPlaybackStopped || shown.PositionMS != (40*time.Minute).Milliseconds() || shown.Title.ID != film {
		t.Errorf("told %v %+v, want it stopped at 40 minutes", last.Kind, last.Details)
	}
}

// A file streamed to a playback played as it is is cut off as the playback stops, here or on
// another node, and is refused after.
func TestAStopCutsOffAFilePlayedAsItIs(t *testing.T) {
	live := memory{}
	s := NewSessions(live, positions{}, served{}, func(context.Context, domain.Event) {}, uuid.NewV7())
	ctx := t.Context()
	guest := uuid.NewV7()
	here, err := s.Start(ctx, domain.PlayDirect, card(guest, uuid.NewV7()))
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := s.Start(ctx, domain.PlayDirect, card(guest, uuid.NewV7()))
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
	delete(live, elsewhere.ID)
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

// A playback of a title removed while it played is swept like any other, once: it is not kept to
// be swept, and fail, again.
func TestAPlaybackOfARemovedTitleIsSweptOnce(t *testing.T) {
	live := memory{}
	var told []domain.Event
	s := NewSessions(live, positions{}, served{}, func(_ context.Context, e domain.Event) { told = append(told, e) }, uuid.NewV7())
	ctx := t.Context()
	p, err := s.Start(ctx, domain.PlayDirect, card(uuid.NewV7(), gone))
	if err != nil {
		t.Fatal(err)
	}
	p.Updated = time.Now().Add(-time.Hour)
	live[p.ID] = p
	if err := s.Sweep(ctx); err != nil {
		t.Fatalf("Sweep = %v, want nothing to fail", err)
	}
	if _, ok := live[p.ID]; ok || told[len(told)-1].Kind != domain.EventPlaybackStopped {
		t.Errorf("after the sweep: still live %v, last told %v; want it stopped and gone", ok, told[len(told)-1].Kind)
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
	s := NewSessions(memory{}, saved, served{}, func(context.Context, domain.Event) {}, uuid.NewV7())
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, domain.PlayDirect, card(oliver, uuid.NewV7()))
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
		s := NewSessions(memory{}, positions{}, remuxer, func(context.Context, domain.Event) {}, uuid.NewV7())
		ctx := t.Context()
		oliver := uuid.NewV7()
		p, err := s.Start(ctx, domain.PlayRemux, card(oliver, uuid.NewV7()))
		if err != nil {
			t.Fatal(err)
		}
		if err := remuxer.Open(ctx, p.ID, hls.Copy{Parts: []hls.Source{{Open: func() (*os.File, error) { return nil, os.ErrNotExist }, Part: hls.Part{Duration: time.Hour, Keyframes: hls.Forced(time.Hour)}}}}); err != nil {
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
		live, saved, streams := memory{}, positions{}, served{}
		var told []domain.Event
		raise := func(_ context.Context, e domain.Event) { told = append(told, e) }
		s := NewSessions(live, saved, streams, raise, uuid.NewV7())
		ctx := t.Context()
		oliver, film := uuid.NewV7(), uuid.NewV7()
		p, err := s.Start(ctx, domain.PlayTranscode, card(oliver, film))
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
		if _, ok := live[p.ID]; !ok || !streams[p.ID] || streams[elsewhere] {
			t.Errorf("swept just short of its life: live %v, stream open %v, other stream open %v; want it kept, the other closed", ok, streams[p.ID], streams[elsewhere])
		}
		synctest.Sleep(time.Second)
		if err := s.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
		if _, ok := live[p.ID]; ok || streams[p.ID] || saved[p.ID] != 40*time.Minute || saved[film] != 40*time.Minute {
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
	s := NewSessions(memory{}, saved, served{}, func(context.Context, domain.Event) {}, uuid.NewV7())
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, domain.PlayDirect, card(oliver, uuid.NewV7()))
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
	s := NewSessions(memory{}, timed{positions{}}, served{}, raise, uuid.NewV7())
	ctx := t.Context()
	oliver := uuid.NewV7()
	p, err := s.Start(ctx, domain.PlayDirect, card(oliver, uuid.NewV7()))
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
