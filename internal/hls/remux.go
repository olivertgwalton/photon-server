package hls

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

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

// conversion is a download's conversion holding a transcode slot until it ends or a playback
// takes the slot.
type conversion struct{ stop context.CancelCauseFunc }

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
	abandoned, _ := filepath.Glob(filepath.Join(subtitles, extracting+"*"))
	for _, a := range abandoned {
		_ = os.RemoveAll(a)
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
		_ = root.Close()
		_ = os.RemoveAll(s.dir)
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
	r.change()
	return held, func() {
		r.mu.Lock()
		if _, ok := r.conversions[c]; ok {
			delete(r.conversions, c)
			r.change()
		}
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

// SetLimit changes how many videos may be encoded at once, or Unlimited. Those encoding beyond a
// lower limit go on; none is begun until there is room under it.
func (r *Remuxer) SetLimit(limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit != r.limit {
		r.limit = limit
		r.change()
	}
}

// Changes is told as a transcode slot is taken or given back, once for any number since it was
// last read.
func (r *Remuxer) Changes() <-chan struct{} { return r.changed }

// change tells Changes, without waiting for it to be read.
func (r *Remuxer) change() {
	select {
	case r.changed <- struct{}{}:
	default:
	}
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

// SubtitleSegment answers segment n of subtitle track as WebVTT. A file's cues are read the first
// time it is asked for; a stream's come with the run that remuxes the segment, which is started
// there if no run will reach it.
func (r *Remuxer) SubtitleSegment(ctx context.Context, playback uuid.UUID, track, n int) (string, error) {
	s, err := r.session(playback)
	if err != nil {
		return "", err
	}
	if track < 0 || track >= len(s.subtitles) || n < 0 || n >= len(s.plan) {
		return "", ErrNoRemux
	}
	sub := s.subtitles[track]
	if sub.File != nil {
		sub.mu.Lock()
		read := sub.read
		sub.mu.Unlock()
		if !read {
			cues, err := r.convert(ctx, *sub.File)
			if err != nil {
				return "", err
			}
			sub.mu.Lock()
			sub.cues, sub.read = cues, true
			sub.mu.Unlock()
		}
	} else if err := r.cuesOf(ctx, s, n); err != nil {
		return "", err
	}
	seg := s.plan[n]
	sub.mu.Lock()
	defer sub.mu.Unlock()
	return writeVTT(sub.cues, s.offsets[seg.Part], seg.Start, seg.End), nil
}

// cuesOf waits until the embedded subtitles' cues of segment n are read. A run reads them from
// where it starts; one that started after n, or stopped before it, will not reach them.
func (r *Remuxer) cuesOf(ctx context.Context, s *session, n int) error {
	s.mu.Lock()
	wait := waiter(s.cued, n)
	if !closed(wait) {
		if s.run == nil || s.run.part != s.plan[n].Part || n < s.run.first || n > s.run.at+jump {
			r.start(ctx, s, n)
		}
		// They are whole once the segment after is made (see cut).
		s.furthest = max(s.furthest, n+1)
		select {
		case s.run.more <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wait:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cueFailed[n]; err != nil {
		delete(s.cueFailed, n)
		delete(s.cued, n)
		return err
	}
	return nil
}

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

// Init opens a part's initialisation, waiting for it to be made: a run of the part writes it
// before any segment, so one is started at the part's first segment where none runs. Only
// fragmented MP4 has one.
func (r *Remuxer) Init(ctx context.Context, playback uuid.UUID, part int) (*os.File, error) {
	s, err := r.session(playback)
	if err != nil {
		return nil, err
	}
	if part < 0 || part >= len(s.sources) || s.format != domain.SegmentsFMP4 {
		return nil, ErrNoRemux
	}
	s.mu.Lock()
	wait := waiter(s.inits, part)
	if !closed(wait) && (s.run == nil || s.run.part != part) {
		r.start(ctx, s, slices.IndexFunc(s.plan, func(seg Segment) bool { return seg.Part == part }))
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-wait:
	}
	// A run that failed before writing it leaves none, and the next one asked for starts again.
	return s.root.Open(initName(part))
}

// MasterName is the playlist a player is given.
const MasterName = "main.m3u8"

// segmentTypes are the media types of HLS segments, by their extensions.
var segmentTypes = map[string]string{".m4s": "video/iso.segment", ".ts": "video/mp2t"}

// Resource is one of a playback's HLS files: a playlist or a WebVTT segment as Text, or a media
// file to serve as it is, and its type.
type Resource struct {
	Text string
	File *os.File
	Type string
}

// Resource answers a playback's HLS file by the name its playlists give it, ErrNoRemux for one it
// has not.
func (r *Remuxer) Resource(ctx context.Context, playback uuid.UUID, name string) (Resource, error) {
	switch {
	case strings.HasSuffix(name, ".m3u8"):
		text, err := r.Playlist(playback, name)
		return Resource{Text: text, Type: "application/vnd.apple.mpegurl"}, err
	case strings.HasPrefix(name, "sub") && strings.HasSuffix(name, ".vtt"):
		track, n, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(name, "sub"), ".vtt"), "-")
		t, terr := strconv.Atoi(track)
		k, kerr := strconv.Atoi(n)
		if !ok || terr != nil || kerr != nil {
			return Resource{}, ErrNoRemux
		}
		text, err := r.SubtitleSegment(ctx, playback, t, k)
		return Resource{Text: text, Type: "text/vtt; charset=utf-8"}, err
	case strings.HasPrefix(name, "init") && strings.HasSuffix(name, ".mp4"):
		part, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "init"), ".mp4"))
		if err != nil {
			return Resource{}, ErrNoRemux
		}
		f, err := r.Init(ctx, playback, part)
		return Resource{File: f, Type: "video/mp4"}, err
	case segmentTypes[path.Ext(name)] != "":
		// A segment is asked for by the name its session's format gives it: an MPEG-TS remux has
		// no .m4s, nor a fragmented-MP4 one a .ts.
		n, err := strconv.Atoi(strings.TrimSuffix(name, path.Ext(name)))
		s, serr := r.session(playback)
		if err != nil || serr != nil || name != segmentName(s.format, n) {
			return Resource{}, ErrNoRemux
		}
		f, err := r.Segment(ctx, playback, n)
		return Resource{File: f, Type: segmentTypes[path.Ext(name)]}, err
	}
	return Resource{}, ErrNoRemux
}

const videoName = "video.m3u8"

func initName(part int) string      { return "init" + strconv.Itoa(part) + ".mp4" }
func subtitleName(track int) string { return "sub" + strconv.Itoa(track) + ".m3u8" }

func segmentName(f domain.SegmentFormat, n int) string {
	switch f {
	case domain.SegmentsMPEGTS:
		return strconv.Itoa(n) + ".ts"
	case domain.SegmentsFMP4:
	}
	return strconv.Itoa(n) + ".m4s"
}

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
	asked := time.Now()
	s.mu.Lock()
	s.furthest = max(s.furthest, n)
	s.forget(n - behind)
	wait := waiter(s.ready, n)
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
	r.segmentWait.Observe(time.Since(asked).Seconds())
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failed[n]; err != nil {
		delete(s.failed, n)
		delete(s.ready, n)
		return nil, err
	}
	return s.root.Open(segmentName(s.format, n))
}

// waiter answers the channel closed when what waiting names n of is made: a segment, a part's
// initialisation, or a segment's cues; the session's lock is held.
func waiter(waiting map[int]chan struct{}, n int) chan struct{} {
	c, ok := waiting[n]
	if !ok {
		c = make(chan struct{})
		waiting[n] = c
	}
	return c
}

// forget removes the segments made before segment n; the session's lock is held. One asked for
// again is made again.
func (s *session) forget(n int) {
	for m, c := range s.ready {
		if m < n && closed(c) && s.failed[m] == nil {
			_ = s.root.Remove(segmentName(s.format, m))
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
	run := &run{cancel: cancel, part: s.plan[n].Part, first: n, at: n, more: make(chan struct{}, 1)}
	s.run = run
	go func() {
		err := r.produce(ctx, s, run)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.run == run {
			s.run = nil
		}
		if errors.Is(err, errIdle) {
			return
		}
		// The file was read to its end, and the readers with it.
		if err == nil && run.at > run.first {
			if c := waiter(s.cued, run.at-1); !closed(c) {
				close(c)
			}
		}
		// A file shorter than it says ends before the part's last segments.
		if err == nil && run.at < len(s.plan) && s.plan[run.at].Part == run.part {
			err = fmt.Errorf("%w: segment %d", errEnded, run.at)
		}
		if err != nil && ctx.Err() == nil {
			r.log.WarnContext(ctx, "remux failed", slog.String("dir", s.dir), slog.Any("err", err))
		}
		// Whoever waits on a segment this run would have made learns it will not be.
		if err != nil && ctx.Err() == nil {
			if c, ok := s.inits[run.part]; ok && !closed(c) {
				close(c)
				delete(s.inits, run.part)
			}
			for m := run.at; m < len(s.plan) && s.plan[m].Part == run.part; m++ {
				if c, ok := s.ready[m]; ok && !closed(c) {
					s.failed[m] = err
					close(c)
				}
			}
			for m := max(run.first, run.at-1); m < len(s.plan) && s.plan[m].Part == run.part; m++ {
				if c, ok := s.cued[m]; ok && !closed(c) {
					s.cueFailed[m] = err
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
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	var layer *styledLayer
	if src.Styled != nil {
		if layer, err = r.styled(ctx, s, *src.Styled, s.offsets[run.part]); err != nil {
			return err
		}
	}
	// Each embedded subtitle is written to a pipe of its own, read as ffmpeg reaches its cues.
	var streams []int
	files := []*os.File{f}
	var reads []*os.File
	defer func() {
		for _, p := range reads {
			_ = p.Close()
		}
	}()
	for _, sub := range s.subtitles {
		if sub.Stream == nil {
			continue
		}
		read, write, err := os.Pipe()
		if err != nil {
			return err
		}
		reads = append(reads, read)
		files = append(files, write)
		streams = append(streams, *sub.Stream)
	}
	cmd := media.NewCommand(ctx, media.Foreground, files, r.tools.FFmpeg.Path, args(r.hw, start, src.Video, src.Audio, layer, s.format, streams)...)
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	// ffmpeg holds the pipes' ends it writes to, and a reader learns they are closed once it ends.
	for _, w := range files[1:] {
		_ = w.Close()
	}
	if err != nil {
		return err
	}
	var readers errgroup.Group
	k := 0
	for _, sub := range s.subtitles {
		if sub.Stream == nil {
			continue
		}
		read, offset := reads[k], s.offsets[run.part]
		k++
		readers.Go(func() error {
			err := readVTT(read, func(c Cue) {
				c.Start += offset
				c.End += offset
				c.Settings, c.Text = "", fromSubRip(c.Text)
				sub.add(c)
			})
			// What ffmpeg writes on is not read now, so it fails at once rather than blocking.
			_ = read.Close()
			return err
		})
	}
	err = r.cut(ctx, s, run, out)
	if err != nil {
		stop(err)
	}
	// Nothing reads what ffmpeg writes now, so a write it is blocked on fails at once rather than
	// holding it past the grace a stop gives it.
	_ = out.Close()
	err = cmp.Or(err, cmd.Err(cmd.Wait()))
	return cmp.Or(err, readers.Wait())
}

// fdInput starts an ffmpeg run that reads the file media.NewCommand passes it as descriptor 3.
func fdInput() []string {
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "fd", "-fd", "3"}
}

// args copies or encodes a file's video and its audio into fragmented MP4 or MPEG-TS on stdout,
// from start, on the file's own clock (see clockOffset). Copied video starts at the keyframe at
// start; encoded video makes one there and every SegmentLength after, on hw, with styled text
// drawn in where there is a layer of it. Each of the subtitle streams is written to descriptors 4
// onwards, a cue at a time, on the file's clock: as SubRip, whose muxer ends a cue as it writes
// it, where WebVTT's ends one only as it begins the next.
func args(hw Hardware, start time.Duration, video domain.VideoPlan, audio *domain.AudioPlan, layer *styledLayer, f domain.SegmentFormat, subtitles []int) []string {
	a := fdInput()
	hw = hw.encoding(video)
	if video.Encode != nil {
		a = append(a, hw.inputArgs(video.Codec, *video.Encode)...)
	}
	a = append(a, "-ss", strconv.FormatFloat(start.Seconds(), 'f', 6, 64), "-copyts", "-i", "fd:")
	a = append(a, streamArgs(hw, video, audio, layer)...)
	// The MP4 muxer writes a file's chapters as a text track, which Apple's players refuse a
	// segment for.
	a = append(a, "-map_chapters", "-1")
	switch f {
	case domain.SegmentsMPEGTS:
		// Without mpegts_copyts the muxer moves every timestamp on by its delay.
		a = append(a, "-f", "mpegts", "-mpegts_copyts", "1")
	case domain.SegmentsFMP4:
		// Dolby Vision's configuration, TrueHD and DTS are experimental in FFmpeg's MP4 muxer.
		a = append(a,
			"-strict", "experimental",
			"-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof+delay_moov+frag_discont+skip_trailer",
			"-use_editlist", "0",
		)
	}
	a = append(a,
		"-avoid_negative_ts", "disabled",
		"-output_ts_offset", strconv.FormatFloat(clockOffset.Seconds(), 'f', 0, 64),
		"-fflags", "+bitexact", "-",
	)
	for i, n := range subtitles {
		a = append(a, "-map", "0:"+strconv.Itoa(n), "-c:s", "subrip", "-f", "srt", "-flush_packets", "1", "pipe:"+strconv.Itoa(4+i))
	}
	return a
}

// streamArgs maps the input's video and audio, each copied or encoded as planned, video on hw.
func streamArgs(hw Hardware, video domain.VideoPlan, audio *domain.AudioPlan, layer *styledLayer) []string {
	var a []string
	in := "0:" + strconv.Itoa(video.Stream)
	switch e := video.Encode; {
	case e != nil && layer != nil:
		// Drawn by libass after the picture is scaled and tone mapped, as Jellyfin's is, at the
		// size encoded, so it is as sharp as it can be.
		filter, encoder := hw.videoArgs(*e, video.Codec)
		a = append(append(a, "-map", in, "-vf", filter+","+layer.filter()), encoder...)
	case e != nil && e.Burn != nil:
		// As Jellyfin's: picture and subtitle each scaled to the size encoded, the picture tone
		// mapped first, so the subtitle is drawn as it was authored.
		filter, encoder := hw.videoArgs(*e, video.Codec)
		size := strconv.Itoa(e.Width) + ":" + strconv.Itoa(e.Height)
		format := "yuv420p"
		if e.Range == domain.RangeHDR10 || e.Range == domain.RangeHLG {
			format = "yuv420p10le"
		}
		graph := "[" + in + "]" + filter + "[main];[0:" + strconv.Itoa(*e.Burn) + "]scale=" + size + "[sub];" +
			"[main][sub]overlay=eof_action=pass:repeatlast=0,format=" + format + "[v]"
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

// fragments is ffmpeg's output read as fragments that each begin on a video keyframe.
type fragments interface {
	next() (fragment, error)
	write(w io.Writer, f fragment) error
}

// cut reads ffmpeg's output and keeps the plan's segments of the run's part from run.at onwards.
// ffmpeg's seek lands on the keyframe at or before the one asked for, so fragments before the
// segment's start are dropped; every fragment starts on a keyframe, and so does every segment.
func (r *Remuxer) cut(ctx context.Context, s *session, run *run, out io.Reader) error {
	var st fragments
	switch s.format {
	case domain.SegmentsMPEGTS:
		st = readTS(out, s.plan[run.at].Start)
	case domain.SegmentsFMP4:
		fmp4, init, err := readInit(out)
		if err != nil {
			return err
		}
		s.mu.Lock()
		if c := waiter(s.inits, run.part); !closed(c) {
			if err = keep(s.root, initName(run.part), init); err == nil {
				close(c)
			}
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
		st = fmp4
	}
	// A frame sits a few milliseconds off the keyframe index: both are rounded.
	const slack = 5 * time.Millisecond
	first := run.at
	n := first
	// The segment being written, under a name no player asks for until it is whole.
	var segment *os.File
	defer func() {
		if segment != nil {
			_ = segment.Close()
			_ = s.root.Remove(segmentName(s.format, n) + ".part")
		}
	}()
	finish := func() error {
		err := segment.Close()
		segment = nil
		if err == nil {
			err = s.root.Rename(segmentName(s.format, n)+".part", segmentName(s.format, n))
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		if c := waiter(s.ready, n); !closed(c) {
			close(c)
		}
		// ffmpeg wrote the fragment that finished this segment once it had read past it, and a
		// subtitle's cues sit in the file where they are shown, so every cue shown before the
		// segment ends has been read. The previous segment's are taken as whole, not this one's,
		// leaving a segment's time for the threads ffmpeg writes each output on to keep up.
		if n > run.first {
			if c := waiter(s.cued, n-1); !closed(c) {
				close(c)
			}
		}
		n++
		run.at = n
		s.mu.Unlock()
		return nil
	}
	for {
		if err := r.throttle(ctx, s, run, n); err != nil {
			return err
		}
		frag, err := st.next()
		if errors.Is(err, io.EOF) {
			if segment != nil {
				return finish()
			}
			return nil
		}
		if err != nil {
			return err
		}
		if n == first && segment == nil && frag.shown < s.plan[first].Start-slack {
			if err := st.write(io.Discard, frag); err != nil {
				return err
			}
			continue
		}
		for n < len(s.plan) && s.plan[n].Part == run.part && frag.shown >= s.plan[n].End-slack && segment != nil {
			if err := finish(); err != nil {
				return err
			}
		}
		if n >= len(s.plan) || s.plan[n].Part != run.part {
			return nil
		}
		if segment == nil {
			if segment, err = s.root.Create(segmentName(s.format, n) + ".part"); err != nil {
				return err
			}
		}
		if err := st.write(segment, frag); err != nil {
			return err
		}
	}
}

// throttle waits while the run is far enough ahead of what has been asked for, and gives up with
// errIdle once nothing has been asked for r.idle.
func (r *Remuxer) throttle(ctx context.Context, s *session, run *run, n int) error {
	lead := ahead
	if s.encodes() {
		lead = aheadEncoding
	}
	for {
		s.mu.Lock()
		far := n > s.furthest+lead
		s.mu.Unlock()
		// A run replaced stops at its next fragment, rather than writing on beside the run that
		// replaced it.
		if !far {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-run.more:
		case <-time.After(r.idle):
			return errIdle
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
