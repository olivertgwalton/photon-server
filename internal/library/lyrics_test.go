package library

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/testtool"
)

type place domain.Place

func (p place) PartPlace(context.Context, uuid.UUID) (domain.Place, error) {
	return domain.Place(p), nil
}

// A track's LRC file beside it gives its lyrics before its own tags do, and a track with neither
// has none.
func TestATracksLyricsAreItsLRCFileElseItsTags(t *testing.T) {
	dir := t.TempDir()
	testtool.Audio(t, filepath.Join(dir, "01 Timeless.flac"), map[string][]string{"LYRICS": {"From the tags"}}, nil)
	testtool.Audio(t, filepath.Join(dir, "02 Cry for Me.flac"), map[string][]string{"LYRICS": {"From the tags"}}, nil)
	testtool.Audio(t, filepath.Join(dir, "03 Untold.flac"), nil, nil)
	write(t, dir, map[string]string{"02 Cry for Me.lrc": "[00:01.00]From the file\n"})
	lyrics := func(rel string) (media.Lyrics, error) {
		return Parts{Places: place{Media: domain.MediaFolder, Root: dir, Rel: rel}}.Lyrics(t.Context(), uuid.NewV7())
	}

	got, err := lyrics("01 Timeless.flac")
	if err != nil || got.Synced || got.Lines[0].Text != "From the tags" {
		t.Errorf("Timeless: %+v, %v; want its tags' lyrics", got, err)
	}
	got, err = lyrics("02 Cry for Me.flac")
	if err != nil || !got.Synced || got.Lines[0] != (media.LyricLine{Start: time.Second, Text: "From the file"}) {
		t.Errorf("Cry for Me: %+v, %v; want its LRC file's", got, err)
	}
	if _, err := lyrics("03 Untold.flac"); !errors.Is(err, ErrNoLyrics) {
		t.Errorf("Untold: err = %v, want %v", err, ErrNoLyrics)
	}
	if _, err := lyrics("../outside.flac"); err == nil {
		t.Error("a part outside the library was read")
	}
}
