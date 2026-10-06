//go:build integration

package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

func TestTheLongestCopyOnDiskPlaysUnlessOneIsAskedFor(t *testing.T) {
	s := migrated(t)
	ctx := t.Context()
	lib, err := s.AddLibrary(ctx, "Films", domain.LibraryMovies, "/srv/films")
	if err != nil {
		t.Fatal(err)
	}
	part := func(rel string, d time.Duration) Part {
		return Part{RelPath: rel, Size: 1, ModTime: time.Unix(0, 0), Facts: &media.Facts{Duration: d}}
	}
	theatricalPart := part("L/theatrical.mkv", 3*time.Hour)
	theatricalPart.Facts.Container = "matroska,webm"
	video := media.Stream{
		Index: 0, Kind: domain.StreamVideo, Codec: "hevc", Profile: "Main 10", Width: 3840, Height: 2160, BitDepth: 10,
		Level: 153, Range: domain.RangeDV, DolbyVision: &media.DolbyVision{Profile: 8, Level: 6, Compatibility: 1},
	}
	theatricalPart.Facts.Streams = []media.Stream{video, {Index: 1, Kind: domain.StreamAudio, Codec: "truehd", Channels: 8}}
	film := Film{Title: "Lawrence", Folder: "L", Copies: []Copy{
		{ContentKey: []byte("cut"), Label: "theatrical", Parts: []Part{theatricalPart}, Subtitles: []Subtitle{
			{RelPath: "L/theatrical.en.sdh.srt", Size: 1, ModTime: time.Unix(0, 0), Codec: "subrip", Language: language.English, HearingImpaired: true},
		}},
		{ContentKey: []byte("long"), Label: "restored", Parts: []Part{part("L/r1.mkv", 2*time.Hour), part("L/r2.mkv", 2*time.Hour)}},
	}}
	if _, err := s.SaveFolder(ctx, lib.ID, "L", []byte("v1"), []Film{film}, nil); err != nil {
		t.Fatal(err)
	}
	item, err := s.q.Item.WithContext(ctx).Take()
	if err != nil {
		t.Fatal(err)
	}
	longest, err := s.Playable(ctx, uuid.UUID{}, uuid.UUID(item.ID), uuid.UUID{})
	if err != nil || len(longest.Parts) != 2 || longest.Parts[1].OffsetMS != (2*time.Hour).Milliseconds() {
		t.Fatalf("Playable = %+v, %v; want the four-hour copy's two parts on one timeline", longest, err)
	}
	v := s.q.Version
	theatrical, err := v.WithContext(ctx).Where(v.Label.Eq("theatrical")).Take()
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Playable(ctx, uuid.UUID{}, uuid.UUID(item.ID), uuid.UUID(theatrical.ID))
	if err != nil || got.Version != uuid.UUID(theatrical.ID) || len(got.Parts) != 1 || got.Container != "matroska,webm" {
		t.Errorf("asking for the theatrical cut: %+v, %v", got, err)
	}
	if len(got.Subtitles) != 1 || got.Subtitles[0].Language != language.English || !got.Subtitles[0].HearingImpaired {
		t.Errorf("its subtitles: %+v, want the English SDH file", got.Subtitles)
	} else if root, rel, err := s.SubtitleFile(ctx, got.Subtitles[0].ID); err != nil || root != "/srv/films" || rel != "L/theatrical.en.sdh.srt" {
		t.Errorf("SubtitleFile = %q %q %v, want the file in the library", root, rel, err)
	} else if sub, err := s.Subtitle(ctx, got.Subtitles[0].ID); err != nil || !reflect.DeepEqual(sub, got.Subtitles[0]) {
		t.Errorf("Subtitle = %+v %v, want it as the copy lists it", sub, err)
	}
	if len(got.Streams) != 2 || !reflect.DeepEqual(got.Streams[0], video) || got.Streams[1].Channels != 8 {
		t.Errorf("its streams: %+v, want the Dolby Vision video as probed and TrueHD 7.1", got.Streams)
	}
	if _, err := s.FinishScan(ctx, lib.ID, []string{"L"}, []string{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Playable(ctx, uuid.UUID{}, uuid.UUID(item.ID), uuid.UUID{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("with every copy's files gone: %v, want ErrNotFound", err)
	}
}
