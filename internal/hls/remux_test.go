package hls

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
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
	r, err := NewRemuxer(fakeFFmpeg(t), t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	open := func() (*os.File, error) { return os.Open("testdata/fragments.mp4") }
	playback := uuid.NewV7()
	if err := r.Open(playback, Copy{Parts: []Source{{
		Open: open, Part: Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	playlist, err := r.Playlist(playback, "video.m3u8")
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

// A process stopped uncleanly leaves its playbacks' segments; the next one starts without them,
// and keeps the subtitles it read before.
func TestARemuxerStartsWithNothingLeftOver(t *testing.T) {
	dir, subtitles := t.TempDir(), t.TempDir()
	stale := filepath.Join(dir, uuid.NewV7().String())
	if err := os.MkdirAll(stale, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "0.m4s"), []byte("segment"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := filepath.Join(subtitles, uuid.NewV7().String())
	if err := os.WriteFile(read, []byte("WEBVTT"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRemuxer(fakeFFmpeg(t), dir, subtitles, Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stopped process's segments are still there: %v", err)
	}
	if _, err := os.Stat(read); err != nil {
		t.Errorf("the subtitles read before are gone: %v", err)
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
		{
			"encoded video is H.264 with a keyframe every segment, tone mapped",
			domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 8000, ToneMap: true}},
			nil,
			[]string{"-vf scale=1280:720,tonemapx=", "-c:v libx264", "-maxrate 8000k -bufsize 16000k", "-force_key_frames expr:gte(t,n_forced*6)"},
		},
		{
			"a picture subtitle is drawn in, in software, both scaled to the size encoded",
			domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000, Burn: new(3)}},
			nil,
			[]string{"-filter_complex [0:0]scale=1280:720,format=yuv420p[main];[0:3]scale=1280:720[sub];[main][sub]overlay=eof_action=pass:repeatlast=0,format=yuv420p[v] -map [v]", "-c:v libx264"},
		},
		{"audio asked for is copied", domain.VideoPlan{Stream: 0, Codec: "h264"}, &domain.AudioPlan{Stream: 2}, []string{"-map 0:0 -c:v copy -map 0:2 -c:a copy"}},
		{
			"audio encoded",
			domain.VideoPlan{Codec: "h264"},
			&domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "eac3", Channels: 6, BitrateKbps: 640}},
			[]string{"-map 0:1 -c:a eac3 -ac 6 -b:a 640k"},
		},
		{
			"audio mixed down to stereo is made louder",
			domain.VideoPlan{Codec: "h264"},
			&domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 256, Boost: 2}},
			[]string{"-map 0:1 -af volume=2 -c:a aac -ac 2 -b:a 256k"},
		},
	} {
		got := strings.Join(args(Hardware{Accel: domain.AccelSoftware}, 12*time.Second, tc.video, tc.audio), " ")
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

// transcode is a minute of video encoded to H.264.
var transcode = Copy{Parts: []Source{{
	Part:  Part{Duration: time.Minute, Keyframes: Forced(time.Minute)},
	Video: domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000}},
}}}

func TestTranscodesAtOnceNeverPassTheLimit(t *testing.T) {
	r, err := NewRemuxer(fakeFFmpeg(t), t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 3, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		opened  []uuid.UUID
		refused int
	)
	for range 20 {
		wg.Go(func() {
			id := uuid.NewV7()
			err := r.Open(id, transcode)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				opened = append(opened, id)
			case errors.Is(err, ErrTranscodeLimit):
				refused++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if active, _, limit := r.Transcodes(); len(opened) != 3 || refused != 17 || active != 3 || limit != 3 {
		t.Fatalf("20 at once: %d opened, %d refused, %d/%d active; want 3 and 17", len(opened), refused, active, limit)
	}

	remux := Copy{Parts: []Source{{Part: transcode.Parts[0].Part, Video: domain.VideoPlan{Codec: "h264"}}}}
	if err := r.Open(uuid.NewV7(), remux); err != nil {
		t.Errorf("a remux at the limit: %v, want it opened, as its video is copied", err)
	}
	r.Close(opened[0])
	if err := r.Open(uuid.NewV7(), transcode); err != nil {
		t.Errorf("a transcode after one closed: %v, want its slot", err)
	}
	if err := r.Open(uuid.NewV7(), transcode); !errors.Is(err, ErrTranscodeLimit) {
		t.Errorf("one more: %v, want ErrTranscodeLimit", err)
	}
}

func TestPlaybacksTakeTheirSlotsFromConversions(t *testing.T) {
	r, err := NewRemuxer(fakeFFmpeg(t), t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 3, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		plays   int
		refused int
		held    []context.Context
	)
	for n := range 30 {
		wg.Go(func() {
			if n%3 == 0 {
				ctx, _, ok := r.HoldConversion(t.Context())
				if ok {
					mu.Lock()
					held = append(held, ctx)
					mu.Unlock()
				}
				return
			}
			err := r.Open(uuid.NewV7(), transcode)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				plays++
			case errors.Is(err, ErrTranscodeLimit):
				refused++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	converting := 0
	for _, ctx := range held {
		if context.Cause(ctx) == nil {
			converting++
		} else if !errors.Is(context.Cause(ctx), ErrPreempted) {
			t.Errorf("a conversion ended with %v, want ErrPreempted", context.Cause(ctx))
		}
	}
	if active, conversions, _ := r.Transcodes(); plays != 3 || refused != 17 || converting != 0 || active != 3 || conversions != 0 {
		t.Fatalf("20 plays and 10 conversions at once: %d played, %d refused, %d still converting, %d active (%d conversions); want 3 played, the rest refused, every conversion stopped",
			plays, refused, converting, active, conversions)
	}
	if _, _, ok := r.HoldConversion(t.Context()); ok {
		t.Error("a conversion with playbacks holding every slot was given one")
	}
}

func TestAConversionWaitsForAFreeSlot(t *testing.T) {
	r, err := NewRemuxer(fakeFFmpeg(t), t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 1, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	play := uuid.NewV7()
	if err := r.Open(play, transcode); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.HoldConversion(t.Context()); ok {
		t.Fatal("a conversion beside a playback holding the only slot was given it")
	}
	r.Close(play)
	_, release, ok := r.HoldConversion(t.Context())
	if !ok {
		t.Fatal("a conversion on an idle node had no slot")
	}
	if _, _, ok := r.HoldConversion(t.Context()); ok {
		t.Error("a second conversion was given the slot the first holds")
	}
	release()
	if active, _, _ := r.Transcodes(); active != 0 {
		t.Errorf("%d transcodes once the conversion ended, want none", active)
	}
}

// A player that resumes far in and seeks back to the start has the remux wait a few segments
// ahead of it again, as Jellyfin's throttling does, rather than run on to where it had been.
func TestARemuxWaitsAheadOfAPlayerThatSeeksBack(t *testing.T) {
	ffmpeg := tool(t, "ffmpeg", "PHOTON_FFMPEG")
	src := filepath.Join(t.TempDir(), "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24", "-t", "240",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", src)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	dir := t.TempDir()
	r, err := NewRemuxer(ffmpeg, dir, t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 120 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(playback, Copy{Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open(src) }, Part: Part{Duration: 240 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	kept := func(n int) bool {
		_, err := os.Stat(filepath.Join(dir, playback.String(), segmentName(n)))
		return err == nil
	}
	for _, n := range []int{30, 2} {
		f, err := r.Segment(t.Context(), playback, n)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	for !kept(2 + ahead) {
		<-time.After(10 * time.Millisecond)
	}
	// The remux copies a segment in well under this; one not throttled would be far along.
	<-time.After(time.Second)
	for n := 2 + ahead + 2; n < 30; n++ {
		if kept(n) {
			t.Fatalf("segment %d was made with the player at 2; want the remux waiting %d ahead of it", n, ahead)
		}
	}
}
