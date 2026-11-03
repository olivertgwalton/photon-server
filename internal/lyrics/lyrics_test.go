package lyrics

import (
	"context"
	"errors"
	"log/slog"
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

type kept struct {
	sources []domain.FieldSource
	saved   map[domain.FieldSource]string
}

func (k *kept) LyricsAsk(context.Context, uuid.UUID) (store.LyricsAsk, error) {
	return store.LyricsAsk{Query: domain.LyricsQuery{Artist: "The Weeknd", Title: "Timeless"}, Sources: k.sources}, nil
}

func (k *kept) KeptLyrics(_ context.Context, _ uuid.UUID, s domain.FieldSource) (string, bool, error) {
	text, ok := k.saved[s]
	return text, ok, nil
}

func (k *kept) KeepLyrics(_ context.Context, _ uuid.UUID, s domain.FieldSource, text string) error {
	k.saved[s] = text
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
	k := &kept{sources: sources, saved: map[domain.FieldSource]string{}}
	return Finder{Parts: noFile{}, Store: k, Providers: registry{l}, Log: slog.New(slog.DiscardHandler)}, k
}

// A track with no lyrics of its own takes its library's lyrics source's, which is asked once:
// what it found, or that it found none, is kept.
func TestATracksLyricsAreAskedOfItsSourceOnce(t *testing.T) {
	l := &lyricist{answer: "[00:01.00]Timeless"}
	f, k := finder([]domain.FieldSource{domain.SourceLRCLIB}, l)
	for range 2 {
		got, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7())
		if err != nil || got.Timing != domain.TimedByLine || got.Lines[0].Text != "Timeless" {
			t.Fatalf("lyrics: %+v, %v", got, err)
		}
	}
	if l.asked != 1 || k.saved[domain.SourceLRCLIB] != "[00:01.00]Timeless" {
		t.Errorf("asked %d times, kept %q; want once, and its answer kept", l.asked, k.saved[domain.SourceLRCLIB])
	}

	none := &lyricist{}
	f, k = finder([]domain.FieldSource{domain.SourceLRCLIB}, none)
	for range 2 {
		if _, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7()); !errors.Is(err, library.ErrNoLyrics) {
			t.Fatalf("a source with none: %v, want %v", err, library.ErrNoLyrics)
		}
	}
	if text, ok := k.saved[domain.SourceLRCLIB]; none.asked != 1 || !ok || text != "" {
		t.Errorf("asked %d times, kept %q %v; want once, and none kept", none.asked, text, ok)
	}
}

// A source that cannot be reached keeps nothing, so it is asked again; a library that ranks no
// lyrics source asks none.
func TestALyricsSourceThatFailsOrIsOffIsNotKept(t *testing.T) {
	down := &lyricist{err: errors.New("lrclib: 503")}
	f, k := finder([]domain.FieldSource{domain.SourceLRCLIB}, down)
	for range 2 {
		if _, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7()); !errors.Is(err, library.ErrNoLyrics) {
			t.Fatalf("a source that failed: %v", err)
		}
	}
	if _, ok := k.saved[domain.SourceLRCLIB]; down.asked != 2 || ok {
		t.Errorf("asked %d times, kept %v; want asked each time, nothing kept", down.asked, ok)
	}

	off := &lyricist{answer: "Timeless"}
	f, _ = finder(nil, off)
	if _, err := f.Find(t.Context(), uuid.NewV7(), uuid.NewV7()); !errors.Is(err, library.ErrNoLyrics) || off.asked != 0 {
		t.Errorf("a library with no lyrics source: %v, asked %d times; want none asked", err, off.asked)
	}
}
