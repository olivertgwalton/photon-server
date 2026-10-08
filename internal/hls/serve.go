package hls

import (
	"context"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

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
