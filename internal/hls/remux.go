package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

const (
	// ahead is how far the remux runs past the furthest segment asked for before it waits: ffmpeg
	// then blocks on its pipe and costs nothing.
	ahead = 5
	// jump is how far beyond the remux's place a request may be before it restarts there rather
	// than waits, as Jellyfin's does about 24 seconds out.
	jump = 4
	// behind is how many segments are kept before the one last asked for, a minute's: a player
	// seeking back further has them made again, and a long film never fills the disk.
	behind = 10
)

// ErrNoRemux is a remux that has ended, or never was.
var ErrNoRemux = errors.New("hls: no such remux")

// ErrTranscodeLimit is a remux that would encode video on a remuxer already encoding its limit.
var ErrTranscodeLimit = errors.New("hls: at the limit of transcodes at once")

// ErrPreempted is a conversion stopped so a playback could have its transcode slot.
var ErrPreempted = errors.New("hls: conversion stopped for a playback")

// errEnded is a file that ends before the segments its length promised.
var errEnded = errors.New("hls: the file ends early")

// Unlimited is a remuxer that encodes as many videos at once as it is asked to.
const Unlimited = 0

// Source is one file of a copy to remux: how to open it, its plan input, its video, and its audio
// if it has any.
type Source struct {
	Open  func() (*os.File, error)
	Part  Part
	Video domain.VideoPlan
	Audio *domain.AudioPlan
}

// Remuxer runs the remuxes of copies played as HLS, one per playback, each writing its segments
// into a folder of its own under dir, and keeps the text streams read out of parts under
// subtitles. It keeps the node's one account of transcode slots, its download conversions' as well
// as its own.
type Remuxer struct {
	ffmpeg    string
	dir       string
	subtitles string
	hw        Hardware
	limit     int
	log       *slog.Logger

	mu          sync.Mutex
	sessions    map[uuid.UUID]*session
	conversions map[*conversion]struct{}
	extractions map[uuid.UUID]*extraction
}

// conversion is a download's conversion holding a transcode slot until it ends or a playback
// takes the slot.
type conversion struct{ stop context.CancelCauseFunc }

// NewRemuxer runs remuxes on hw, at most limit of them encoding video at once, or Unlimited. What
// is in dir is removed: no remux outlives its process, and one that stopped uncleanly left its
// segments.
func NewRemuxer(ffmpeg, dir, subtitles string, hw Hardware, limit int, log *slog.Logger) (*Remuxer, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	for _, d := range []string{dir, subtitles} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, err
		}
	}
	// No extraction outlives its process.
	abandoned, _ := filepath.Glob(filepath.Join(subtitles, extracting+"*"))
	for _, a := range abandoned {
		_ = os.RemoveAll(a)
	}
	return &Remuxer{
		ffmpeg: ffmpeg, dir: dir, subtitles: subtitles, hw: hw, limit: limit, log: log,
		sessions: map[uuid.UUID]*session{}, conversions: map[*conversion]struct{}{}, extractions: map[uuid.UUID]*extraction{},
	}, nil
}

// Copy is what a playback's HLS is made of: its parts in order, its text subtitles, and how the
// master playlist describes its video.
type Copy struct {
	Parts     []Source
	Subtitles []Subtitle
	Variant   Variant
}

// Variant is the video's one variant as the master playlist describes it: the bitrate it is sent
// at, its formats as RFC 6381 names them, and its VIDEO-RANGE (SDR, PQ or HLG); either of the last
// two left out where it is not known.
type Variant struct {
	BandwidthKbps int
	Codecs        []string
	Range         string
}

// session is one playback's remux: its plan, the segments made so far, and the ffmpeg making more.
type session struct {
	dir       string
	root      *os.Root
	sources   []Source
	offsets   []time.Duration
	plan      []Segment
	playlists map[string]string
	subtitles []*subtitle

	mu       sync.Mutex
	ready    map[int]chan struct{}
	failed   map[int]error
	inits    map[int]bool
	run      *run
	furthest int
}

// run is one ffmpeg producing the segments of one part from first onwards.
type run struct {
	cancel context.CancelFunc
	part   int
	at     int // the next segment it will finish
	more   chan struct{}
}

// subtitle is a subtitle of a session, its cues read once, when first asked for.
type subtitle struct {
	Subtitle
	mu   sync.Mutex
	cues []Cue
	read bool
}

// textStreams are the streams of part the session carries as subtitles.
func (s *session) textStreams(part uuid.UUID) []int {
	var streams []int
	for _, sub := range s.subtitles {
		for _, src := range sub.Sources {
			if src.Stream != nil && src.Part == part {
				streams = append(streams, *src.Stream)
			}
		}
	}
	return streams
}

// Open starts the remux of a playback's copy; nothing is run until a segment is asked for.
// Addresses in its playlists are relative to the playlists' own. A copy whose video is encoded is
// refused with ErrTranscodeLimit while playbacks hold every transcode slot; where a conversion
// holds one, the conversion is stopped and the playback has its slot.
func (r *Remuxer) Open(playback uuid.UUID, c Copy) error {
	parts := make([]Part, len(c.Parts))
	offsets := make([]time.Duration, len(c.Parts))
	var at time.Duration
	for i, s := range c.Parts {
		parts[i], offsets[i] = s.Part, at
		at += s.Part.Duration
	}
	s := &session{
		dir: filepath.Join(r.dir, playback.String()), sources: c.Parts, offsets: offsets, plan: Plan(parts),
		ready: map[int]chan struct{}{}, failed: map[int]error{}, inits: map[int]bool{},
	}
	s.playlists = map[string]string{
		MasterName: Master(c.Subtitles, c.Variant, videoName, subtitleName),
		videoName:  Playlist(s.plan, initName, segmentName),
	}
	for n, sub := range c.Subtitles {
		s.subtitles = append(s.subtitles, &subtitle{Subtitle: sub})
		s.playlists[subtitleName(n)] = Playlist(s.plan, nil, func(k int) string { return subtitleSegmentName(n, k) })
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	s.root = root
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.encodes() && r.full() && !r.preempt() {
		_ = root.Close()
		_ = os.RemoveAll(s.dir)
		return ErrTranscodeLimit
	}
	r.sessions[playback] = s
	return nil
}

// HoldConversion gives a download's conversion a transcode slot for as long as no playback needs
// it: ok is false where every slot is held, and held is cancelled with ErrPreempted when a playback
// takes the slot. release gives the slot back once the conversion ends.
func (r *Remuxer) HoldConversion(ctx context.Context) (held context.Context, release func(), ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full() {
		return nil, nil, false
	}
	held, stop := context.WithCancelCause(ctx)
	c := &conversion{stop: stop}
	r.conversions[c] = struct{}{}
	return held, func() {
		r.mu.Lock()
		delete(r.conversions, c)
		r.mu.Unlock()
		stop(nil)
	}, true
}

// preempt stops a conversion holding a slot, answering whether there was one; the caller holds
// r.mu. Its slot is free as this returns, not once its ffmpeg has gone.
func (r *Remuxer) preempt() bool {
	for c := range r.conversions {
		delete(r.conversions, c)
		c.stop(ErrPreempted)
		return true
	}
	return false
}

// Transcodes answers how many videos this node is encoding, how many of those are conversions,
// and the most that may be at once.
func (r *Remuxer) Transcodes() (active, conversions, limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.transcodes(), len(r.conversions), r.limit
}

// full is whether every transcode slot is held; the caller holds r.mu.
func (r *Remuxer) full() bool { return r.limit != Unlimited && r.transcodes() >= r.limit }

// transcodes counts the remuxes encoding video and the conversions; the caller holds r.mu. The
// sessions are the one account of what playback is encoding, as a remux leaves them however its
// playback ends, or where it is never opened.
func (r *Remuxer) transcodes() int {
	n := len(r.conversions)
	for _, s := range r.sessions {
		if s.encodes() {
			n++
		}
	}
	return n
}

// encodes is whether a remux encodes video; every part of a copy is played to the same plan.
func (s *session) encodes() bool { return len(s.sources) > 0 && s.sources[0].Video.Encode != nil }

// Has reports whether this remuxer runs a playback's remux.
func (r *Remuxer) Has(playback uuid.UUID) bool {
	_, err := r.session(playback)
	return err == nil
}

// Playlist answers one of a playback's playlists by name: its master, MasterName, and those the
// master names.
func (r *Remuxer) Playlist(playback uuid.UUID, name string) (string, error) {
	s, err := r.session(playback)
	if err != nil {
		return "", err
	}
	p, ok := s.playlists[name]
	if !ok {
		return "", ErrNoRemux
	}
	return p, nil
}

// SubtitleSegment answers segment n of subtitle track as WebVTT, reading the track's cues the
// first time it is asked for. A request given up while they are read leaves them being read.
func (r *Remuxer) SubtitleSegment(ctx context.Context, playback uuid.UUID, track, n int) (string, error) {
	s, err := r.session(playback)
	if err != nil {
		return "", err
	}
	if track < 0 || track >= len(s.subtitles) || n < 0 || n >= len(s.plan) {
		return "", ErrNoRemux
	}
	sub := s.subtitles[track]
	sub.mu.Lock()
	cues, read := sub.cues, sub.read
	sub.mu.Unlock()
	if !read {
		for _, src := range sub.Sources {
			c, err := r.cues(ctx, src, s.textStreams(src.Part))
			if err != nil {
				return "", err
			}
			cues = append(cues, c...)
		}
		sub.mu.Lock()
		sub.cues, sub.read = cues, true
		sub.mu.Unlock()
	}
	seg := s.plan[n]
	return writeVTT(cues, s.offsets[seg.Part], seg.Start, seg.End), nil
}

// Encoder answers the device video planned so is encoded on, or nothing where it is copied.
func (r *Remuxer) Encoder(video domain.VideoPlan) domain.Acceleration {
	if video.Encode == nil {
		return ""
	}
	return r.hw.encoding(video).Accel
}

// Close ends a playback's remux and removes its segments.
func (r *Remuxer) Close(playback uuid.UUID) {
	r.mu.Lock()
	s := r.sessions[playback]
	delete(r.sessions, playback)
	r.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.run != nil {
		s.run.cancel()
	}
	s.mu.Unlock()
	_ = s.root.Close()
	_ = os.RemoveAll(s.dir)
}

// Playbacks answers the playbacks this remuxer runs a remux of.
func (r *Remuxer) Playbacks() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Collect(maps.Keys(r.sessions))
}

func (r *Remuxer) session(playback uuid.UUID) (*session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[playback]
	if !ok {
		return nil, ErrNoRemux
	}
	return s, nil
}

// Init opens a part's initialisation, waiting for it to be made.
func (r *Remuxer) Init(ctx context.Context, playback uuid.UUID, part int) (*os.File, error) {
	s, err := r.session(playback)
	if err != nil {
		return nil, err
	}
	if part < 0 || part >= len(s.sources) {
		return nil, ErrNoRemux
	}
	first := 0
	for n, seg := range s.plan {
		if seg.Part == part {
			first = n
			break
		}
	}
	s.mu.Lock()
	made := s.inits[part]
	s.mu.Unlock()
	if !made {
		// The initialisation is written before any segment, so the part's first one brings it.
		f, err := r.Segment(ctx, playback, first)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
	}
	return s.root.Open(initName(part))
}

// MasterName is the playlist a player is given.
const MasterName = "main.m3u8"

const videoName = "video.m3u8"

func initName(part int) string      { return "init" + strconv.Itoa(part) + ".mp4" }
func segmentName(n int) string      { return strconv.Itoa(n) + ".m4s" }
func subtitleName(track int) string { return "sub" + strconv.Itoa(track) + ".m3u8" }
func subtitleSegmentName(track, n int) string {
	return "sub" + strconv.Itoa(track) + "-" + strconv.Itoa(n) + ".vtt"
}

// Segment opens segment n, starting or moving the remux to make it if need be, and waiting until
// it is whole.
func (r *Remuxer) Segment(ctx context.Context, playback uuid.UUID, n int) (*os.File, error) {
	s, err := r.session(playback)
	if err != nil {
		return nil, err
	}
	if n < 0 || n >= len(s.plan) {
		return nil, ErrNoRemux
	}
	s.mu.Lock()
	s.furthest = max(s.furthest, n)
	s.forget(n - behind)
	wait := s.waiter(n)
	select {
	case <-wait:
	default:
		if s.run == nil || s.run.part != s.plan[n].Part || n < s.run.at || n > s.run.at+jump {
			r.start(ctx, s, n)
		}
	}
	if s.run != nil {
		select {
		case s.run.more <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-wait:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failed[n]; err != nil {
		delete(s.failed, n)
		delete(s.ready, n)
		return nil, err
	}
	return s.root.Open(segmentName(n))
}

// waiter answers the channel closed when segment n is made; the session's lock is held.
func (s *session) waiter(n int) chan struct{} {
	c, ok := s.ready[n]
	if !ok {
		c = make(chan struct{})
		s.ready[n] = c
	}
	return c
}

// forget removes the segments made before segment n; the session's lock is held. One asked for
// again is made again.
func (s *session) forget(n int) {
	for m, c := range s.ready {
		if m < n && closed(c) && s.failed[m] == nil {
			_ = s.root.Remove(segmentName(m))
			delete(s.ready, m)
		}
	}
}

// start replaces the session's ffmpeg with one starting at segment n; the session's lock is held.
// The run outlives the request that asked for it.
func (r *Remuxer) start(ctx context.Context, s *session, n int) {
	if s.run != nil {
		s.run.cancel()
	}
	// The player is where it asked the run to start, wherever it had been: after a seek back the
	// run waits ahead of that, not ahead of the furthest it ever asked for.
	s.furthest = n
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &run{cancel: cancel, part: s.plan[n].Part, at: n, more: make(chan struct{}, 1)}
	s.run = run
	go func() {
		err := r.produce(ctx, s, run)
		s.mu.Lock()
		defer s.mu.Unlock()
		// A file shorter than it says ends before the part's last segments.
		if err == nil && run.at < len(s.plan) && s.plan[run.at].Part == run.part {
			err = fmt.Errorf("%w: segment %d", errEnded, run.at)
		}
		if err != nil && ctx.Err() == nil {
			r.log.WarnContext(ctx, "remux failed", slog.String("dir", s.dir), slog.Any("err", err))
		}
		if s.run == run {
			s.run = nil
		}
		// Whoever waits on a segment this run would have made learns it will not be.
		if err != nil && ctx.Err() == nil {
			for m := run.at; m < len(s.plan) && s.plan[m].Part == run.part; m++ {
				if c, ok := s.ready[m]; ok && !closed(c) {
					s.failed[m] = err
					close(c)
				}
			}
		}
	}()
}

func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// produce runs ffmpeg from the start of segment run.at, cuts what it writes into the plan's
// segments and keeps each as it is finished.
func (r *Remuxer) produce(ctx context.Context, s *session, run *run) error {
	src := s.sources[run.part]
	f, err := src.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	start := s.plan[run.at].Start
	cmd := exec.CommandContext(ctx, r.ffmpeg, args(r.hw, start, src.Video, src.Audio)...) //nolint:gosec // the configured ffmpeg; every argument is built here
	cmd.ExtraFiles = []*os.File{f}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	err = r.cut(ctx, s, run, out)
	if err != nil {
		_ = cmd.Process.Kill()
	}
	if werr := cmd.Wait(); err == nil && werr != nil && ctx.Err() == nil {
		err = fmt.Errorf("ffmpeg: %w: %s", werr, stderr)
	}
	return err
}

// args copies or encodes a file's video and its audio into fragmented MP4 on stdout, from start,
// on the file's own clock (see clockOffset). Copied video starts at the keyframe at start; encoded
// video makes one there and every SegmentLength after, on hw.
func args(hw Hardware, start time.Duration, video domain.VideoPlan, audio *domain.AudioPlan) []string {
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "fd", "-fd", "3"}
	hw = hw.encoding(video)
	if video.Encode != nil {
		a = append(a, hw.inputArgs(video.Codec, *video.Encode)...)
	}
	a = append(a, "-ss", strconv.FormatFloat(start.Seconds(), 'f', 6, 64), "-copyts", "-i", "fd:")
	a = append(a, streamArgs(hw, video, audio)...)
	// Dolby Vision's configuration, TrueHD and DTS are experimental in FFmpeg's MP4 muxer.
	return append(a,
		"-strict", "experimental",
		"-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof+delay_moov+frag_discont+skip_trailer",
		"-use_editlist", "0", "-avoid_negative_ts", "disabled",
		"-output_ts_offset", strconv.FormatFloat(clockOffset.Seconds(), 'f', 0, 64),
		"-fflags", "+bitexact", "-",
	)
}

// streamArgs maps the input's video and audio, each copied or encoded as planned, video on hw.
func streamArgs(hw Hardware, video domain.VideoPlan, audio *domain.AudioPlan) []string {
	var a []string
	in := "0:" + strconv.Itoa(video.Stream)
	switch e := video.Encode; {
	case e != nil && e.Burn != nil:
		// As Jellyfin's: picture and subtitle each scaled to the size encoded, the picture tone
		// mapped first, so the subtitle is drawn as it was authored.
		filter, encoder := hw.videoArgs(*e, video.Codec)
		size := strconv.Itoa(e.Width) + ":" + strconv.Itoa(e.Height)
		graph := "[" + in + "]" + filter + "[main];[0:" + strconv.Itoa(*e.Burn) + "]scale=" + size + "[sub];" +
			"[main][sub]overlay=eof_action=pass:repeatlast=0,format=yuv420p[v]"
		a = append(append(a, "-filter_complex", graph, "-map", "[v]"), encoder...)
	case e != nil:
		filter, encoder := hw.videoArgs(*e, video.Codec)
		a = append(append(a, "-map", in, "-vf", filter), encoder...)
	// Apple's players take HEVC only as hvc1, and Dolby Vision as dvh1.
	case video.Codec == "hevc" && video.DolbyVision == domain.DolbyVisionKeep:
		a = append(a, "-map", in, "-c:v", "copy", "-tag:v", "dvh1")
	case video.Codec == "hevc":
		a = append(a, "-map", in, "-c:v", "copy", "-tag:v", "hvc1")
	default:
		a = append(a, "-map", in, "-c:v", "copy")
	}
	if video.DolbyVision == domain.DolbyVisionStrip {
		a = append(a, "-bsf:v", "dovi_rpu=strip=1")
	}
	if audio != nil {
		a = append(a, "-map", "0:"+strconv.Itoa(audio.Stream))
		if e := audio.Encode; e != nil {
			if e.Boost > 0 {
				a = append(a, "-af", "volume="+strconv.FormatFloat(e.Boost, 'f', -1, 64))
			}
			a = append(a, "-c:a", e.Codec, "-ac", strconv.Itoa(e.Channels), "-b:a", strconv.Itoa(e.BitrateKbps)+"k")
		} else {
			a = append(a, "-c:a", "copy")
		}
	}
	return a
}

// cut reads ffmpeg's output and keeps the plan's segments of the run's part from run.at onwards.
// ffmpeg's seek lands on the keyframe at or before the one asked for, so fragments before the
// segment's start are dropped; every fragment starts on a keyframe, and so does every segment.
func (r *Remuxer) cut(ctx context.Context, s *session, run *run, out io.Reader) error {
	st, init, err := readInit(out)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if !s.inits[run.part] {
		err = keep(s.root, initName(run.part), init)
		s.inits[run.part] = err == nil
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	// A frame sits a few milliseconds off the keyframe index: both are rounded.
	const slack = 5 * time.Millisecond
	first := run.at
	n := first
	var segment []byte
	finish := func() error {
		if err := keep(s.root, segmentName(n), segment); err != nil {
			return err
		}
		s.mu.Lock()
		if c := s.waiter(n); !closed(c) {
			close(c)
		}
		n++
		run.at = n
		s.mu.Unlock()
		segment = nil
		return nil
	}
	for {
		if err := r.throttle(ctx, s, run, n); err != nil {
			return err
		}
		frag, shown, err := st.next()
		if errors.Is(err, io.EOF) {
			if segment != nil {
				return finish()
			}
			return nil
		}
		if err != nil {
			return err
		}
		if n == first && segment == nil && shown < s.plan[first].Start-slack {
			continue
		}
		for n < len(s.plan) && s.plan[n].Part == run.part && shown >= s.plan[n].End-slack && segment != nil {
			if err := finish(); err != nil {
				return err
			}
		}
		if n >= len(s.plan) || s.plan[n].Part != run.part {
			return nil
		}
		segment = append(segment, frag...)
	}
}

// throttle waits while the run is far enough ahead of what has been asked for.
func (r *Remuxer) throttle(ctx context.Context, s *session, run *run, n int) error {
	for {
		s.mu.Lock()
		far := n > s.furthest+ahead
		s.mu.Unlock()
		if !far {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-run.more:
		}
	}
}

// keep writes a file whole before anyone can read it.
func keep(root *os.Root, name string, data []byte) error {
	part := name + ".part"
	if err := root.WriteFile(part, data, 0o600); err != nil {
		return err
	}
	return root.Rename(part, name)
}

// tail keeps the end of what ffmpeg says, for its error.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2048 {
		t.b = t.b[len(t.b)-2048:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.b) }
