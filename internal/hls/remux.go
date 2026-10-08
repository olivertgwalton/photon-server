package hls

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

const (
	// ahead is how far, in segments, a remux copying video runs past the furthest segment asked
	// for before it waits: ffmpeg then blocks on its pipe and costs nothing. aheadEncoding is a
	// remux encoding video's: an encode near realtime needs more room, as Plex's runs a minute or
	// more ahead.
	ahead         = 5
	aheadEncoding = 10
	// idleRun is how long a waiting run is kept with no segment asked for before its ffmpeg is
	// stopped, giving back its encoder to the node; a player that comes back starts another.
	idleRun = time.Minute
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

// errIdle is a run stopped for having had no segment asked of it for idleRun.
var errIdle = errors.New("hls: no segment asked for")

// Unlimited is a remuxer that encodes as many videos at once as it is asked to.
const Unlimited = 0

// Source is one file of a copy to remux: how to open it, its plan input, its video, and its audio
// if it has any.
type Source struct {
	Open  func() (*os.File, error)
	Part  Part
	Video domain.VideoPlan
	Audio *domain.AudioPlan
	// Styled is styled text drawn into its video (see VideoEncode's Burn): a stream of the file,
	// or a file beside the copy timed on the copy's timeline.
	Styled *SubtitleSource
}

// Remuxer runs the remuxes of copies played as HLS, one per playback, each writing its segments
// into a folder of its own under dir, and keeps the subtitles read out of parts under subtitles. It keeps the node's one account of transcode slots, its download conversions' as well
// as its own.
type Remuxer struct {
	tools     media.Tools
	dir       string
	subtitles string
	hw        Hardware
	limit     int
	idle      time.Duration
	log       *slog.Logger
	// segmentWait is how long each request for a segment waits for it to be made.
	segmentWait prometheus.Histogram

	mu          sync.Mutex
	sessions    map[uuid.UUID]*session
	conversions map[*conversion]struct{}
	extractions map[uuid.UUID]*extraction
	// changed is told, once for any number of changes, as a transcode slot is taken or given back.
	changed chan struct{}
}

// NewRemuxer runs remuxes with tools on hw, at most limit of them encoding video at once, or
// Unlimited. What is in dir is removed: no remux outlives its process, and one that stopped
// uncleanly left its segments.
func NewRemuxer(tools media.Tools, dir, subtitles string, hw Hardware, limit int, log *slog.Logger) (*Remuxer, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	for _, d := range []string{dir, subtitles} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return nil, err
		}
	}
	// No extraction outlives its process.
	abandoned, err := filepath.Glob(filepath.Join(subtitles, extracting+"*"))
	if err != nil {
		return nil, err
	}
	for _, a := range abandoned {
		if err := os.RemoveAll(a); err != nil {
			return nil, err
		}
	}
	return &Remuxer{
		tools: tools, dir: dir, subtitles: subtitles, hw: hw, limit: limit, idle: idleRun, log: log,
		sessions: map[uuid.UUID]*session{}, conversions: map[*conversion]struct{}{}, extractions: map[uuid.UUID]*extraction{},
		changed: make(chan struct{}, 1), segmentWait: newSegmentWait(),
	}, nil
}

// Copy is what a playback's HLS is made of: its parts in order, its text subtitles, how the
// master playlist describes its video, where on its timeline the player starts, and the
// container of its segments, fragmented MP4 where it is not said.
type Copy struct {
	Parts     []Source
	Subtitles []Subtitle
	Variant   Variant
	Start     time.Duration
	Segments  domain.SegmentFormat
}

// Variant is the video's one variant as the master playlist describes it: the bitrate it is sent
// at, its formats as RFC 6381 names them, its VIDEO-RANGE (SDR, PQ or HLG), its picture's size and
// its frame rate; any of the last four left out where it is not known.
type Variant struct {
	BandwidthKbps int
	Codecs        []string
	Range         string
	Width, Height int
	FrameRate     float64
}

// session is one playback's remux: its plan, the segments made so far, and the ffmpeg making more.
type session struct {
	dir       string
	root      *os.Root
	format    domain.SegmentFormat
	sources   []Source
	offsets   []time.Duration
	plan      []Segment
	playlists map[string]string
	subtitles []*subtitle

	mu       sync.Mutex
	ready    map[int]chan struct{}
	failed   map[int]error
	inits    map[int]chan struct{}
	run      *run
	furthest int
	// cued and cueFailed are as ready and failed, for every embedded subtitle's cues of a segment.
	// They are kept with the cues, never forgotten as segments are.
	cued      map[int]chan struct{}
	cueFailed map[int]error
}

// run is one ffmpeg producing the segments of one part from first onwards.
type run struct {
	cancel context.CancelFunc
	part   int
	first  int
	at     int // the next segment it will finish
	more   chan struct{}
}

// subtitle is a subtitle of a session: a file's cues read once, when first asked for, or a
// stream's as the runs read them, in order of when each is shown.
type subtitle struct {
	Subtitle
	mu   sync.Mutex
	cues []Cue
	read bool
}

// add keeps a cue a run read, once however many runs read it.
func (s *subtitle) add(c Cue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := sort.Search(len(s.cues), func(i int) bool { return s.cues[i].Start > c.Start })
	for j := i - 1; j >= 0 && s.cues[j].Start == c.Start; j-- {
		if s.cues[j] == c {
			return
		}
	}
	s.cues = slices.Insert(s.cues, i, c)
}

// Open starts the remux of a playback's copy at the segment holding c.Start, so it is under way
// while the player reads its playlists. Addresses in its playlists are relative to the playlists'
// own. A copy whose video is encoded is refused with ErrTranscodeLimit while playbacks hold every
// transcode slot; where a conversion holds one, the conversion is stopped and the playback has its
// slot.
func (r *Remuxer) Open(ctx context.Context, playback uuid.UUID, c Copy) error {
	parts := make([]Part, len(c.Parts))
	offsets := make([]time.Duration, len(c.Parts))
	var at time.Duration
	for i, s := range c.Parts {
		parts[i], offsets[i] = s.Part, at
		at += s.Part.Duration
	}
	s := &session{
		dir: filepath.Join(r.dir, playback.String()), format: cmp.Or(c.Segments, domain.SegmentsFMP4),
		sources: c.Parts, offsets: offsets, plan: Plan(parts),
		ready: map[int]chan struct{}{}, failed: map[int]error{}, inits: map[int]chan struct{}{},
		cued: map[int]chan struct{}{}, cueFailed: map[int]error{},
	}
	// MPEG-TS has no initialisation, and needs no later version than 3, which older players read.
	version, init := 7, initName
	switch s.format {
	case domain.SegmentsMPEGTS:
		version, init = 3, nil
	case domain.SegmentsFMP4:
	}
	s.playlists = map[string]string{
		MasterName: Master(c.Subtitles, c.Variant, version, videoName, subtitleName),
		videoName:  Playlist(s.plan, version, init, func(n int) string { return segmentName(s.format, n) }),
	}
	for n, sub := range c.Subtitles {
		s.subtitles = append(s.subtitles, &subtitle{Subtitle: sub})
		s.playlists[subtitleName(n)] = Playlist(s.plan, version, nil, func(k int) string { return subtitleSegmentName(n, k) })
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
		if err := s.remove(); err != nil {
			r.log.WarnContext(ctx, "remux folder not removed", slog.String("dir", s.dir), slog.Any("err", err))
		}
		return ErrTranscodeLimit
	}
	if len(s.plan) > 0 {
		first := 0
		for n, seg := range s.plan {
			if offsets[seg.Part]+seg.Start <= c.Start {
				first = n
			}
		}
		// Nothing else has the session yet to hold its lock against.
		r.start(ctx, s, first)
	}
	r.sessions[playback] = s
	if s.encodes() {
		r.change()
	}
	return nil
}

// encodes is whether a remux encodes video; every part of a copy is played to the same plan.
func (s *session) encodes() bool { return len(s.sources) > 0 && s.sources[0].Video.Encode != nil }

// EncodedOn is what a node encoding on accel encodes video with, or nothing where it is copied.
func EncodedOn(accel domain.Acceleration, video domain.VideoPlan) domain.Acceleration {
	if video.Encode == nil {
		return ""
	}
	return Hardware{Accel: accel}.encoding(video).Accel
}

// Close ends a playback's remux and removes its segments.
func (r *Remuxer) Close(playback uuid.UUID) {
	r.mu.Lock()
	s := r.sessions[playback]
	delete(r.sessions, playback)
	if s != nil && s.encodes() {
		r.change()
	}
	r.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.run != nil {
		s.run.cancel()
	}
	s.mu.Unlock()
	if err := s.remove(); err != nil {
		r.log.Warn("remux folder not removed", slog.String("dir", s.dir), slog.Any("err", err))
	}
}

// remove removes the session's folder and what is in it.
func (s *session) remove() error {
	return errors.Join(s.root.Close(), os.RemoveAll(s.dir))
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
