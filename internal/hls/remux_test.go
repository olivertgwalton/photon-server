package hls

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// fakeFFmpeg writes what jellyfin-ffmpeg wrote for the fixture, from its start whatever it is asked:
// the remuxer has to drop what comes before the segment it wants, as after a real seek's landing
// on an earlier keyframe.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	fixture, err := filepath.Abs("testdata/fragments.mp4")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec cat '"+fixture+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// shownIn answers when each fragment of a segment is shown, read back through its part's init.
func shownIn(t *testing.T, init, segment *os.File) []time.Duration {
	t.Helper()
	defer init.Close()
	defer segment.Close()
	a, err := io.ReadAll(init)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(segment)
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := readInit(bytes.NewReader(append(a, b...)))
	if err != nil {
		t.Fatal(err)
	}
	var out []time.Duration
	for {
		_, at, err := s.next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, at.Round(100*time.Millisecond))
	}
}

func TestEachSegmentIsExactlyWhatThePlaylistSays(t *testing.T) {
	r, err := NewRemuxer(fakeFFmpeg(t), t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	open := func() (*os.File, error) { return os.Open("testdata/fragments.mp4") }
	playback := uuid.NewV7()
	if err := r.Open(playback, []Source{{
		Open: open, Part: Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	playlist, err := r.Playlist(playback)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(playlist, "#EXTINF:6.000000,") != 5 {
		t.Fatalf("playlist =\n%s\nwant five six-second segments", playlist)
	}

	// Two players at once, one from the start and one jumping to the fourth segment.
	var wg sync.WaitGroup
	for _, n := range []int{0, 3} {
		wg.Go(func() {
			seg, err := r.Segment(t.Context(), playback, n)
			if err != nil {
				t.Error(err)
				return
			}
			init, err := r.Init(t.Context(), playback, 0)
			if err != nil {
				t.Error(err)
				return
			}
			want := []time.Duration{time.Duration(6*n) * time.Second, time.Duration(6*n+2) * time.Second, time.Duration(6*n+4) * time.Second}
			if got := shownIn(t, init, seg); len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
				t.Errorf("segment %d holds fragments shown at %v, want %v", n, got, want)
			}
		})
	}
	wg.Wait()

	r.Close(playback)
	if f, err := r.Segment(t.Context(), playback, 1); !errors.Is(err, ErrNoRemux) {
		_ = f.Close()
		t.Errorf("after closing: %v, want ErrNoRemux", err)
	}
}

// args is the remuxer's whole say over what ffmpeg makes of a file.
func TestArgsCarryWhatWasDecided(t *testing.T) {
	for _, tc := range []struct {
		name  string
		video domain.VideoPlan
		audio *domain.AudioPlan
		want  []string
	}{
		{"HEVC is hvc1 for Apple's players", domain.VideoPlan{Codec: "hevc"}, nil, []string{"-tag:v hvc1"}},
		{"Dolby Vision kept is dvh1", domain.VideoPlan{Codec: "hevc", DolbyVision: domain.DolbyVisionKeep}, nil, []string{"-tag:v dvh1"}},
		{
			"Dolby Vision stripped leaves its base layer",
			domain.VideoPlan{Codec: "hevc", DolbyVision: domain.DolbyVisionStrip},
			nil,
			[]string{"-tag:v hvc1", "-bsf:v dovi_rpu=strip=1"},
		},
		{"audio asked for is copied", domain.VideoPlan{Stream: 0, Codec: "h264"}, &domain.AudioPlan{Stream: 2}, []string{"-map 0:0 -c:v copy -map 0:2 -c:a copy"}},
		{
			"audio encoded",
			domain.VideoPlan{Codec: "h264"},
			&domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "eac3", Channels: 6, BitrateKbps: 640}},
			[]string{"-map 0:1 -c:a eac3 -ac 6 -b:a 640k"},
		},
	} {
		got := strings.Join(args(12*time.Second, tc.video, tc.audio), " ")
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", tc.name, got, w)
			}
		}
		if tc.audio == nil && strings.Contains(got, "-c:a") {
			t.Errorf("%s: %q has audio, and there is none", tc.name, got)
		}
	}
}
