package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"uuid"
)

const (
	// ahead is how far the remux runs past the furthest segment asked for before it waits: ffmpeg
	// then blocks on its pipe and costs nothing.
	ahead = 5
	// jump is how far beyond the remux's place a request may be before it restarts there rather
	// than waits, as Jellyfin's does about 24 seconds out.
	jump = 4
	// idle is how long a remux nobody asks anything of lives on.
	idle = 2 * time.Minute
)

// ErrNoRemux is a remux that has ended, or never was.
var ErrNoRemux = errors.New("hls: no such remux")

// Source is one file of a copy to remux: how to open it, its plan input, and the audio to keep.
type Source struct {
	Open func() (*os.File, error)
	Part Part
	// Audio is the file's stream to copy as the audio, by its index in the file; nil is its first
	// audio stream.
	Audio *int
}

// Remuxer runs the remuxes of copies played as HLS, one per playback, each writing its segments
// into a folder of its own under dir.
type Remuxer struct {
	ffmpeg string
	dir    string
	log    *slog.Logger

	mu       sync.Mutex
	sessions map[uuid.UUID]*session
}

func NewRemuxer(ffmpeg, dir string, log *slog.Logger) (*Remuxer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Remuxer{ffmpeg: ffmpeg, dir: dir, log: log, sessions: map[uuid.UUID]*session{}}, nil
}

// session is one playback's remux: its plan, the segments made so far, and the ffmpeg making more.
type session struct {
	dir      string
	root     *os.Root
	sources  []Source
	plan     []Segment
	playlist string

	mu       sync.Mutex
	ready    map[int]chan struct{}
	failed   map[int]error
	inits    map[int]bool
	run      *run
	furthest int
	touched  time.Time
}

// run is one ffmpeg producing the segments of one part from first onwards.
type run struct {
	cancel context.CancelFunc
	part   int
	at     int // the next segment it will finish
	more   chan struct{}
}

// Open starts the remux of a playback's copy; nothing is run until a segment is asked for.
// Addresses in its playlist are relative to the playlist's own.
func (r *Remuxer) Open(playback uuid.UUID, sources []Source) error {
	parts := make([]Part, len(sources))
	for i, s := range sources {
		parts[i] = s.Part
	}
	s := &session{
		dir: filepath.Join(r.dir, playback.String()), sources: sources, plan: Plan(parts),
		ready: map[int]chan struct{}{}, failed: map[int]error{}, inits: map[int]bool{}, touched: time.Now(),
	}
	s.playlist = Playlist(s.plan, initName, segmentName)
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	s.root = root
	r.mu.Lock()
	r.sessions[playback] = s
	r.mu.Unlock()
	return nil
}

// Playlist answers a playback's media playlist.
func (r *Remuxer) Playlist(playback uuid.UUID) (string, error) {
	s, err := r.session(playback)
	if err != nil {
		return "", err
	}
	return s.playlist, nil
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

// Sweep closes every remux nobody has asked anything of for a while: a player that went away.
func (r *Remuxer) Sweep() {
	r.mu.Lock()
	var stale []uuid.UUID
	for id, s := range r.sessions {
		s.mu.Lock()
		if time.Since(s.touched) > idle {
			stale = append(stale, id)
		}
		s.mu.Unlock()
	}
	r.mu.Unlock()
	for _, id := range stale {
		r.Close(id)
	}
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

func initName(part int) string { return "init" + strconv.Itoa(part) + ".mp4" }
func segmentName(n int) string { return strconv.Itoa(n) + ".m4s" }

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
	s.touched = time.Now()
	s.furthest = max(s.furthest, n)
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

// start replaces the session's ffmpeg with one starting at segment n; the session's lock is held.
// The run outlives the request that asked for it.
func (r *Remuxer) start(ctx context.Context, s *session, n int) {
	if s.run != nil {
		s.run.cancel()
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &run{cancel: cancel, part: s.plan[n].Part, at: n, more: make(chan struct{}, 1)}
	s.run = run
	go func() {
		err := r.produce(ctx, s, run)
		if err != nil && ctx.Err() == nil {
			r.log.WarnContext(ctx, "remux failed", slog.String("dir", s.dir), slog.Any("err", err))
		}
		s.mu.Lock()
		defer s.mu.Unlock()
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
	cmd := exec.CommandContext(ctx, r.ffmpeg, args(start, src.Audio)...) //nolint:gosec // the configured ffmpeg; every argument is built here
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

// args copies a file's first video and one audio stream into fragmented MP4 on stdout,
// from the keyframe at start, on the file's own clock (see clockOffset).
func args(start time.Duration, audio *int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-protocol_whitelist", "fd", "-fd", "3",
		"-ss", strconv.FormatFloat(start.Seconds(), 'f', 6, 64), "-copyts", "-i", "fd:",
		"-map", "0:v:0", "-map", audioMap(audio), "-c", "copy",
		"-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof+delay_moov+frag_discont+skip_trailer",
		"-use_editlist", "0", "-avoid_negative_ts", "disabled",
		"-output_ts_offset", strconv.FormatFloat(clockOffset.Seconds(), 'f', 0, 64),
		"-fflags", "+bitexact", "-",
	}
}

func audioMap(stream *int) string {
	if stream == nil {
		return "0:a:0?"
	}
	return "0:" + strconv.Itoa(*stream)
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
