package hls

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// fakeFFmpeg writes what jellyfin-ffmpeg wrote for the fixture, from its start whatever it is asked:
// the remuxer has to drop what comes before the segment it wants, as after a real seek's landing
// on an earlier keyframe.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	return writing(t, "testdata/fragments.mp4")
}

// writing is an ffmpeg that writes a fixture whatever it is asked.
func writing(t *testing.T, name string) string {
	t.Helper()
	fixture, err := filepath.Abs(name)
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
		frag, err := s.next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err == nil {
			err = s.write(io.Discard, frag)
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, frag.shown.Round(100*time.Millisecond))
	}
}

func TestEachSegmentIsExactlyWhatThePlaylistSays(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	open := func() (*os.File, error) { return os.Open("testdata/fragments.mp4") }
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{{
		Open: open, Part: Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	playlist, err := r.Resource(t.Context(), playback, "video.m3u8")
	if err != nil || playlist.Type != "application/vnd.apple.mpegurl" {
		t.Fatal(playlist, err)
	}
	if strings.Count(playlist.Text, "#EXTINF:6.000000,") != 5 {
		t.Fatalf("playlist =\n%s\nwant five six-second segments", playlist.Text)
	}
	for _, name := range []string{"x.m4s", "sub-1.vtt", "initx.mp4", "0.ts"} {
		if _, err := r.Resource(t.Context(), playback, name); !errors.Is(err, ErrNoRemux) {
			t.Errorf("%s: %v, want ErrNoRemux", name, err)
		}
	}

	// Two players at once, one from the start and one jumping to the fourth segment.
	var wg sync.WaitGroup
	for _, n := range []int{0, 3} {
		wg.Go(func() {
			seg, err := r.Resource(t.Context(), playback, strconv.Itoa(n)+".m4s")
			if err != nil {
				t.Error(err)
				return
			}
			init, err := r.Resource(t.Context(), playback, "init0.mp4")
			if err != nil {
				t.Error(err)
				return
			}
			want := []time.Duration{time.Duration(6*n) * time.Second, time.Duration(6*n+2) * time.Second, time.Duration(6*n+4) * time.Second}
			if got := shownIn(t, init.File, seg.File); len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
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

// A long film played through keeps only its last minute of segments on disk; one asked for again
// is made again.
func TestSegmentsFarBehindThePlayerAreRemoved(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, dir, t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	part := Source{
		Open:  func() (*os.File, error) { return os.Open("testdata/fragments.mp4") },
		Part:  Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}
	// Five parts of five segments each.
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{part, part, part, part, part}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(playback) })
	get := func(n int) {
		t.Helper()
		f, err := r.Segment(t.Context(), playback, n)
		if err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
		_ = f.Close()
	}
	for n := range 25 {
		get(n)
	}
	kept := func(n int) bool {
		_, err := os.Stat(filepath.Join(dir, playback.String(), segmentName(domain.SegmentsFMP4, n)))
		return err == nil
	}
	if kept(0) || !kept(24-behind) || !kept(24) {
		t.Errorf("after segment 24: segment 0 kept %t, %d kept %t, 24 kept %t; want only the last %d kept",
			kept(0), 24-behind, kept(24-behind), kept(24), behind)
	}
	get(0)
	if !kept(0) {
		t.Error("segment 0 asked for again was not made again")
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
	if _, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, dir, subtitles, Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stopped process's segments are still there: %v", err)
	}
	if _, err := os.Stat(read); err != nil {
		t.Errorf("the subtitles read before are gone: %v", err)
	}
}

// A file whose header says a minute and which holds thirty seconds: a player asking past its end
// is told at once, not left waiting.
func TestASegmentPastAShortFilesEndFails(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 30 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{{
		Open:  func() (*os.File, error) { return os.Open("testdata/fragments.mp4") },
		Part:  Part{Duration: time.Minute, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(playback) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, n := range []int{5, 7} {
		f, err := r.Segment(ctx, playback, n)
		if err == nil {
			_ = f.Close()
		}
		if !errors.Is(err, errEnded) {
			t.Errorf("segment %d, past the file's end: %v, want it refused at once", n, err)
		}
	}
}

// args is the remuxer's whole say over what ffmpeg makes of a file.
func TestArgsCarryWhatWasDecided(t *testing.T) {
	for _, tc := range []struct {
		name  string
		video domain.VideoPlan
		audio *domain.AudioPlan
		layer *styledLayer
		want  []string
	}{
		{"HEVC is hvc1 for Apple's players", domain.VideoPlan{Codec: "hevc"}, nil, nil, []string{"-tag:v hvc1"}},
		{"Dolby Vision kept is dvh1", domain.VideoPlan{Codec: "hevc", DolbyVision: domain.DolbyVisionKeep}, nil, nil, []string{"-tag:v dvh1"}},
		{
			"Dolby Vision stripped leaves its base layer",
			domain.VideoPlan{Codec: "hevc", DolbyVision: domain.DolbyVisionStrip},
			nil,
			nil,
			[]string{"-tag:v hvc1", "-bsf:v dovi_rpu=strip=1"},
		},
		{
			"encoded video is H.264 with a keyframe every segment, tone mapped",
			domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 8000, ToneMap: true}},
			nil,
			nil,
			[]string{"-vf scale=1280:720,tonemapx=", "-c:v libx264", "-maxrate 8000k -bufsize 16000k", "-force_key_frames expr:gte(t,n_forced*6)"},
		},
		{
			"a picture subtitle is drawn in, in software, both scaled to the size encoded",
			domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000, Burn: new(3)}},
			nil,
			nil,
			[]string{"-filter_complex [0:0]scale=1280:720,format=yuv420p[main];[0:3]scale=1280:720[sub];[main][sub]overlay=eof_action=pass:repeatlast=0,format=yuv420p[v] -map [v]", "-c:v libx264"},
		},
		{
			"styled text is drawn in by libass after the picture is scaled, with the fonts the file carries",
			domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000, Burn: new(3)}},
			nil,
			&styledLayer{file: "/cache/part/3.ass", fonts: "/cache/part/fonts"},
			[]string{"-map 0:0 -vf scale=1280:720,format=yuv420p,subtitles=f=/cache/part/3.ass:fontsdir=/cache/part/fonts -c:v libx264"},
		},
		{
			"a styled file beside a copy is drawn on the copy's timeline into its second part, read in its charset",
			domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000, BurnFile: new(uuid.New())}},
			nil,
			&styledLayer{file: "/hls/p/styled.ass", charset: "CP1251", offset: 45 * time.Minute},
			[]string{"-vf scale=1280:720,format=yuv420p,setpts=PTS+2700.000000/TB,subtitles=f=/hls/p/styled.ass:charenc=CP1251,setpts=PTS-2700.000000/TB"},
		},
		{"audio asked for is copied", domain.VideoPlan{Stream: 0, Codec: "h264"}, &domain.AudioPlan{Stream: 2}, nil, []string{"-map 0:0 -c:v copy -map 0:2 -c:a copy"}},
		{
			"audio encoded",
			domain.VideoPlan{Codec: "h264"},
			&domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "eac3", Channels: 6, BitrateKbps: 640}},
			nil,
			[]string{"-map 0:1 -c:a eac3 -ac 6 -b:a 640k"},
		},
		{
			"audio mixed down to stereo is made louder",
			domain.VideoPlan{Codec: "h264"},
			&domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 2, BitrateKbps: 256, Boost: 2}},
			nil,
			[]string{"-map 0:1 -af volume=2 -c:a aac -ac 2 -b:a 256k"},
		},
	} {
		got := strings.Join(args(Hardware{Accel: domain.AccelSoftware}, 12*time.Second, tc.video, tc.audio, tc.layer, domain.SegmentsFMP4), " ")
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

// unplayed is a file a test never plays: the run its remux starts with ends at once.
func unplayed() (*os.File, error) { return nil, os.ErrNotExist }

// transcode is a minute of video encoded to H.264.
var transcode = Copy{Parts: []Source{{
	Open:  unplayed,
	Part:  Part{Duration: time.Minute, Keyframes: Forced(time.Minute)},
	Video: domain.VideoPlan{Codec: "hevc", Encode: &domain.VideoEncode{Codec: "h264", Width: 1280, Height: 720, BitrateKbps: 4000}},
}}}

func TestTranscodesAtOnceNeverPassTheLimit(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 3, slog.New(slog.DiscardHandler))
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
			err := r.Open(t.Context(), id, transcode)
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

	remux := Copy{Parts: []Source{{Open: unplayed, Part: transcode.Parts[0].Part, Video: domain.VideoPlan{Codec: "h264"}}}}
	if err := r.Open(t.Context(), uuid.NewV7(), remux); err != nil {
		t.Errorf("a remux at the limit: %v, want it opened, as its video is copied", err)
	}
	r.Close(opened[0])
	if err := r.Open(t.Context(), uuid.NewV7(), transcode); err != nil {
		t.Errorf("a transcode after one closed: %v, want its slot", err)
	}
	if err := r.Open(t.Context(), uuid.NewV7(), transcode); !errors.Is(err, ErrTranscodeLimit) {
		t.Errorf("one more: %v, want ErrTranscodeLimit", err)
	}
}

func TestPlaybacksTakeTheirSlotsFromConversions(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 3, slog.New(slog.DiscardHandler))
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
			err := r.Open(t.Context(), uuid.NewV7(), transcode)
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
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, 1, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	play := uuid.NewV7()
	if err := r.Open(t.Context(), play, transcode); err != nil {
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
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}}, dir, t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 120 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open(src) }, Part: Part{Duration: 240 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	kept := func(n int) bool {
		_, err := os.Stat(filepath.Join(dir, playback.String(), segmentName(domain.SegmentsFMP4, n)))
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

// A player has a part's initialisation as soon as ffmpeg has written it, not once a whole segment
// has been made after it.
func TestAnInitIsAnsweredBeforeAnySegmentIsMade(t *testing.T) {
	fixture, err := filepath.Abs("testdata/fragments.mp4")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	_, init, err := readInit(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	// An encoder slow to make its first segment: it writes the initialisation, then nothing.
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nhead -c " + strconv.Itoa(len(init)) + " '" + fixture + "'\nexec sleep 60\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open(fixture) }, Part: Part{Duration: 30 * time.Second, Keyframes: Forced(30 * time.Second)},
		Video: domain.VideoPlan{Codec: "h264"},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	f, err := r.Init(ctx, playback, 0)
	if err != nil {
		t.Fatalf("init: %v, want it answered with no segment made", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(got, init) {
		t.Errorf("init holds %d bytes (%v), want the %d ffmpeg wrote", len(got), err, len(init))
	}
}

// A playback opened partway in is being made from there before its player asks for anything.
func TestARemuxIsUnderWayWhereThePlayerStarts(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: fakeFFmpeg(t)}}, dir, t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Start: 13 * time.Second, Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open("testdata/fragments.mp4") }, Part: Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	kept := func(n int) bool {
		_, err := os.Stat(filepath.Join(dir, playback.String(), segmentName(domain.SegmentsFMP4, n)))
		return err == nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for !kept(2) {
		if time.Now().After(deadline) {
			t.Fatal("segment 2, holding 0:13, was not made before it was asked for")
		}
		<-time.After(10 * time.Millisecond)
	}
	if kept(0) {
		t.Error("segment 0 was made for a player starting at 0:13")
	}
}

// A player gone without a word gives back its run's ffmpeg once it has asked for nothing for a
// while, keeping its playback; asking again starts another.
func TestARunNobodyAsksOfStopsUntilAskedAgain(t *testing.T) {
	ffmpeg := tool(t, "ffmpeg", "PHOTON_FFMPEG")
	src := filepath.Join(t.TempDir(), "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24", "-t", "240",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", src)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r.idle = 200 * time.Millisecond
	var keyframes []time.Duration
	for k := range 120 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open(src) }, Part: Part{Duration: 240 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	s, err := r.session(playback)
	if err != nil {
		t.Fatal(err)
	}
	running := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.run != nil
	}
	get := func(n int) {
		t.Helper()
		f, err := r.Segment(t.Context(), playback, n)
		if err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
		_ = f.Close()
	}
	get(0)
	deadline := time.Now().Add(10 * time.Second)
	for running() {
		if time.Now().After(deadline) {
			t.Fatal("the run is still going with nothing asked of it")
		}
		<-time.After(10 * time.Millisecond)
	}
	if !r.Has(playback) {
		t.Fatal("the playback went with its run")
	}
	get(1)
	get(20)
}

// tsKeyframes answers when each keyframe of an MPEG-TS segment read by itself is shown, checking
// it begins with the PAT and PMT and a keyframe, as a player starting there needs.
func tsKeyframes(t *testing.T, segment *os.File) []time.Duration {
	t.Helper()
	defer segment.Close()
	b, err := io.ReadAll(segment)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 3*tsPacket || pid(b) != patPID {
		t.Fatalf("a segment of %d bytes begins with PID %d, want the PAT", len(b), pid(b))
	}
	s := readTS(bytes.NewReader(b), 0)
	var out []time.Duration
	for {
		frag, err := s.next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err == nil {
			err = s.write(io.Discard, frag)
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(out) == 0 && (pid(b[tsPacket:]) != s.pmt || pid(b[2*tsPacket:]) != s.video) {
			t.Errorf("a segment begins with PIDs %d, %d, %d; want the PAT, the PMT and the keyframe", pid(b), pid(b[tsPacket:]), pid(b[2*tsPacket:]))
		}
		out = append(out, frag.shown)
	}
}

func TestMPEGTSSegmentsAreExactlyWhatThePlaylistSays(t *testing.T) {
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: writing(t, "testdata/segments.ts")}}, t.TempDir(), t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 15 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Segments: domain.SegmentsMPEGTS, Subtitles: []Subtitle{{Language: "en"}}, Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open("testdata/segments.ts") }, Part: Part{Duration: 30 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"}, Audio: &domain.AudioPlan{Stream: 1},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	for name, want := range map[string][]string{
		"main.m3u8":  {"#EXT-X-VERSION:3\n", "SUBTITLES=\"subs\"", "URI=\"sub0.m3u8\""},
		"video.m3u8": {"#EXT-X-VERSION:3\n", "#EXTINF:6.000000,\n0.ts\n", "#EXTINF:6.000000,\n4.ts\n"},
		"sub0.m3u8":  {"#EXT-X-VERSION:3\n", "sub0-4.vtt"},
	} {
		playlist, err := r.Playlist(playback, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(playlist, w) || strings.Contains(playlist, "EXT-X-MAP") {
				t.Errorf("%s =\n%s\nwant %q in it, and no EXT-X-MAP", name, playlist, w)
			}
		}
	}

	var wg sync.WaitGroup
	for _, n := range []int{0, 3} {
		wg.Go(func() {
			seg, err := r.Segment(t.Context(), playback, n)
			if err != nil {
				t.Error(err)
				return
			}
			want := []time.Duration{time.Duration(6*n) * time.Second, time.Duration(6*n+2) * time.Second, time.Duration(6*n+4) * time.Second}
			if got := tsKeyframes(t, seg); !slices.Equal(got, want) {
				t.Errorf("segment %d holds keyframes shown at %v, want %v", n, got, want)
			}
		})
	}
	wg.Wait()
	if f, err := r.Init(t.Context(), playback, 0); !errors.Is(err, ErrNoRemux) {
		_ = f.Close()
		t.Errorf("an MPEG-TS playback's init: %v, want ErrNoRemux", err)
	}
}

// A film remuxed by a real ffmpeg into MPEG-TS: every segment is read by ffprobe alone, starting
// on a keyframe where the plan starts it, and the segments one after another play the whole film.
func TestMPEGTSSegmentsEachPlayAlone(t *testing.T) {
	ffmpeg, ffprobe := tool(t, "ffmpeg", "PHOTON_FFMPEG"), tool(t, "ffprobe", "PHOTON_FFPROBE")
	src := filepath.Join(t.TempDir(), "film.mkv")
	made := exec.CommandContext(t.Context(), ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "20", "-c:v", "libx264", "-preset", "ultrafast", "-bf", "2", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0",
		"-c:a", "flac", src)
	if out, err := made.CombinedOutput(); err != nil {
		t.Fatalf("making the film: %v: %s", err, out)
	}
	dir := t.TempDir()
	r, err := NewRemuxer(media.Tools{FFmpeg: media.Tool{Path: ffmpeg}}, dir, t.TempDir(), Hardware{Accel: domain.AccelSoftware}, Unlimited, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var keyframes []time.Duration
	for k := range 10 {
		keyframes = append(keyframes, time.Duration(2*k)*time.Second)
	}
	playback := uuid.NewV7()
	if err := r.Open(t.Context(), playback, Copy{Segments: domain.SegmentsMPEGTS, Parts: []Source{{
		Open: func() (*os.File, error) { return os.Open(src) }, Part: Part{Duration: 20 * time.Second, Keyframes: keyframes},
		Video: domain.VideoPlan{Codec: "h264"},
		Audio: &domain.AudioPlan{Stream: 1, Encode: &domain.AudioEncode{Codec: "aac", Channels: 1, BitrateKbps: 64}},
	}}}); err != nil {
		t.Fatal(err)
	}
	defer r.Close(playback)
	whole := filepath.Join(t.TempDir(), "whole.ts")
	for n, start := range []time.Duration{0, 6 * time.Second, 12 * time.Second, 18 * time.Second} {
		f, err := r.Segment(t.Context(), playback, n)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		alone := filepath.Join(t.TempDir(), segmentName(domain.SegmentsMPEGTS, n))
		if err := os.WriteFile(alone, b, 0o600); err != nil {
			t.Fatal(err)
		}
		all, err := os.OpenFile(whole, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = all.Write(b)
			err = cmp.Or(err, all.Close())
		}
		if err != nil {
			t.Fatal(err)
		}
		probe := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-select_streams", "v", "-read_intervals", "%+#1",
			"-show_entries", "packet=pts_time,flags:stream=codec_name", "-of", "csv=p=0", alone)
		out, err := probe.Output()
		if err != nil {
			t.Fatalf("segment %d alone: %v", n, err)
		}
		want := fmt.Sprintf("%.6f,K", (start + clockOffset).Seconds())
		if got := string(out); !strings.HasPrefix(got, want) || !strings.Contains(got, "h264") {
			t.Errorf("segment %d alone begins %q, want H.264 from a keyframe at %s", n, got, want)
		}
	}
	probe := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-show_entries", "format=duration:stream=codec_name", "-of", "csv=p=0", whole)
	out, err := probe.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(out))
	seconds, err := strconv.ParseFloat(got[len(got)-1], 64)
	if err != nil || math.Abs(seconds-20) > 0.1 || !slices.Contains(got, "h264") || !slices.Contains(got, "aac") {
		t.Errorf("the segments one after another: %q, want twenty seconds of H.264 and AAC", got)
	}
}
