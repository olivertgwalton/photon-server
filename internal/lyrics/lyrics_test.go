package lyrics

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type noFile struct{}

func (noFile) Lyrics(context.Context, uuid.UUID) (media.Lyrics, error) {
	return media.Lyrics{}, library.ErrNoLyrics
}

type answer struct {
	text    string
	timing  domain.LyricsTiming
	current bool
}

type kept struct {
	sources []domain.FieldSource
	saved   map[domain.FieldSource]answer
	queued  int
}

func (k *kept) LyricsAsk(context.Context, uuid.UUID) (store.LyricsAsk, error) {
	return store.LyricsAsk{Query: domain.LyricsQuery{Artist: "The Weeknd", Title: "Timeless"}, Sources: k.sources}, nil
}

func (k *kept) KeptLyrics(_ context.Context, _ uuid.UUID, s domain.FieldSource) (string, bool, error) {
	a := k.saved[s]
	return a.text, a.current, nil
}

func (k *kept) KeepLyrics(_ context.Context, _ uuid.UUID, s domain.FieldSource, text string, timing domain.LyricsTiming) error {
	k.saved[s] = answer{text, timing, true}
	return nil
}

func (k *kept) AskLyrics(context.Context, uuid.UUID) error {
	k.queued++
	return nil
}

type lyricist struct {
	answer string
	err    error
	asked  int
}

func (l *lyricist) Info() provider.Info { return provider.Info{ID: domain.SourceLRCLIB} }

func (l *lyricist) Lyrics(context.Context, domain.LyricsQuery) (string, error) {
	l.asked++
	return l.answer, l.err
}

type registry struct{ p provider.Provider }

func (r registry) Get(context.Context, domain.FieldSource) (provider.Provider, bool, error) {
	return r.p, true, nil
}

func finder(sources []domain.FieldSource, l *lyricist) (Finder, *kept) {
	k := &kept{sources: sources, saved: map[domain.FieldSource]answer{}}
	return Finder{Parts: noFile{}, Store: k, Providers: registry{l}}, k
}

var lrclib = []domain.FieldSource{domain.SourceLRCLIB}

// A track's lyrics are fetched by a job, which asks its library's source once and keeps what it
// found, timed; reading them asks no source.
func TestATracksLyricsAreFetchedOnceAndReadFromWhatWasKept(t *testing.T) {
	l := &lyricist{answer: "[00:01.00]Timeless"}
	f, k := finder(lrclib, l)
	if _, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7()); !errors.Is(err, library.ErrNoLyrics) || k.queued != 1 || l.asked != 0 {
		t.Fatalf("before the fetch: %v, %d fetches queued, asked %d times; want none yet, one queued, none asked", err, k.queued, l.asked)
	}
	for range 2 {
		if err := f.Fetch(t.Context(), uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
	}
	if a := k.saved[domain.SourceLRCLIB]; l.asked != 1 || a.text != "[00:01.00]Timeless" || a.timing != domain.TimedByLine {
		t.Errorf("asked %d times, kept %+v; want once, its answer kept timed by line", l.asked, a)
	}
	got, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7())
	if err != nil || got.Timing != domain.TimedByLine || got.Lines[0].Text != "Timeless" || k.queued != 1 {
		t.Errorf("after the fetch: %+v, %v, %d queued; want the lyrics kept, nothing more queued", got, err, k.queued)
	}
}

// A source that cannot be reached keeps nothing and fails the fetch, so it is asked again; one
// that has none is kept as having none; a library that ranks no lyrics source asks none.
func TestALyricsSourceThatFailsHasNoneOrIsOff(t *testing.T) {
	down := &lyricist{err: errors.New("lrclib: 503")}
	f, k := finder(lrclib, down)
	if err := f.Fetch(t.Context(), uuid.NewV7()); err == nil || len(k.saved) != 0 {
		t.Errorf("a source that failed: %v, kept %v; want the fetch failed, nothing kept", err, k.saved)
	}

	none := &lyricist{}
	f, k = finder(lrclib, none)
	if err := f.Fetch(t.Context(), uuid.NewV7()); err != nil || k.saved[domain.SourceLRCLIB] != (answer{current: true}) {
		t.Errorf("a source with none: %v, kept %+v; want none kept", err, k.saved)
	}

	off := &lyricist{answer: "Timeless"}
	f, k = finder(nil, off)
	if err := f.Fetch(t.Context(), uuid.NewV7()); err != nil || off.asked != 0 {
		t.Errorf("a library with no lyrics source: %v, asked %d times; want none asked", err, off.asked)
	}
	if _, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7()); !errors.Is(err, library.ErrNoLyrics) || k.queued != 0 {
		t.Errorf("reading with no lyrics source: %v, %d queued; want none queued", err, k.queued)
	}
}
