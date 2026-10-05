package playback

import (
	"cmp"
	"context"
	"os"
	"time"
	"uuid"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type partStore interface {
	PartFile(ctx context.Context, part uuid.UUID) (root, rel string, err error)
	SubtitleFile(ctx context.Context, id uuid.UUID) (root, rel string, err error)
	Keyframes(ctx context.Context, part uuid.UUID) ([]int64, bool, error)
	SaveKeyframes(ctx context.Context, part uuid.UUID, ptsMS []int64) error
}

type keyframer interface {
	Keyframes(ctx context.Context, f *os.File, mode domain.KeyframeMode) ([]int64, error)
}

type remuxer interface {
	Open(playback uuid.UUID, c hls.Copy) error
	Close(playback uuid.UUID)
}

// Remuxes opens a playback's copy as HLS: each of its files cut at its keyframes where the video
// is copied, which are indexed in the background as a library is scanned and here, before playing,
// for a file not reached yet; or every SegmentLength where it is encoded.
type Remuxes struct {
	parts  partStore
	frames keyframer
	hls    remuxer
}

func NewRemuxes(parts partStore, frames keyframer, h remuxer) *Remuxes {
	return &Remuxes{parts: parts, frames: frames, hls: h}
}

// Open starts a playback's HLS of a copy, carrying its video and audio as decided, and, unless a
// subtitle is drawn into the picture, its text subtitles, embedded and beside it, as WebVTT.
func (r *Remuxes) Open(ctx context.Context, playback uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan) error {
	// The remux opens its files long after this request has been answered.
	opening := context.WithoutCancel(ctx)
	sources := make([]hls.Source, len(c.Parts))
	opens := make([]func() (*os.File, error), len(c.Parts))
	for i, p := range c.Parts {
		open := func() (*os.File, error) { return openFile(opening, r.parts.PartFile, p.ID) }
		opens[i] = open
		duration := time.Duration(p.DurationMS) * time.Millisecond
		keyframes := hls.Forced(duration)
		if video.Encode == nil {
			var err error
			if keyframes, err = r.keyframes(ctx, p.ID, open); err != nil {
				return err
			}
		}
		sources[i] = hls.Source{
			Open: open, Video: video, Audio: audio,
			Part: hls.Part{Duration: duration, Keyframes: keyframes},
		}
	}
	kbps := c.BitrateKbps
	if e := video.Encode; e != nil {
		kbps = e.BitrateKbps
		if audio != nil && audio.Encode != nil {
			kbps += audio.Encode.BitrateKbps
		}
	}
	h := hls.Copy{Parts: sources, Variant: variant(c.Streams, video, audio, kbps)}
	// A subtitle drawn into the picture is the only one offered, as Jellyfin's master playlist
	// has it: another turned on by a player would be drawn over it.
	if e := video.Encode; e != nil && e.Burn != nil {
		return r.hls.Open(playback, h)
	}
	// The parts of a copy are cut from one master, so each holds the first's streams.
	for _, st := range c.Streams {
		if st.Kind != domain.StreamSubtitle || !hls.TextSubtitle(st.Codec) {
			continue
		}
		sub := subtitle(st.Title, st.Language, st.Default, st.Forced, st.HearingImpaired)
		for i, p := range c.Parts {
			sub.Sources = append(sub.Sources, hls.SubtitleSource{
				Open: opens[i], Stream: &st.Index, Part: p.ID, Offset: time.Duration(p.OffsetMS) * time.Millisecond,
			})
		}
		h.Subtitles = append(h.Subtitles, sub)
	}
	for _, f := range c.Subtitles {
		if !hls.TextSubtitle(f.Codec) {
			continue
		}
		sub := subtitle(f.Title, f.Language, f.Default, f.Forced, f.HearingImpaired)
		sub.Sources = []hls.SubtitleSource{{Open: func() (*os.File, error) { return openFile(opening, r.parts.SubtitleFile, f.ID) }, Language: sub.Language}}
		h.Subtitles = append(h.Subtitles, sub)
	}
	return r.hls.Open(playback, h)
}

// subtitle names a subtitle by its title, else its language in English.
func subtitle(title string, lang language.Tag, def, forced, sdh bool) hls.Subtitle {
	s := hls.Subtitle{Name: title, Default: def, Forced: forced, HearingImpaired: sdh}
	if lang != language.Und {
		s.Language = lang.String()
		s.Name = cmp.Or(title, display.English.Tags().Name(lang))
	}
	return s
}

func (r *Remuxes) Close(playback uuid.UUID) { r.hls.Close(playback) }

// keyframes answers a part's keyframes, indexing them if the scan has not reached it yet.
func (r *Remuxes) keyframes(ctx context.Context, part uuid.UUID, open func() (*os.File, error)) ([]time.Duration, error) {
	pts, ok, err := r.parts.Keyframes(ctx, part)
	if err != nil {
		return nil, err
	}
	if !ok {
		if pts, err = r.index(ctx, part, open); err != nil {
			return nil, err
		}
	}
	keyframes := make([]time.Duration, len(pts))
	for k, ms := range pts {
		keyframes[k] = time.Duration(ms) * time.Millisecond
	}
	return keyframes, nil
}

func (r *Remuxes) index(ctx context.Context, part uuid.UUID, open func() (*os.File, error)) ([]int64, error) {
	f, err := open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pts, err := r.frames.Keyframes(ctx, f, domain.KeyframesFull)
	if err != nil {
		return nil, err
	}
	return pts, r.parts.SaveKeyframes(ctx, part, pts)
}

// openFile opens a file of a library the scanner recorded.
func openFile(ctx context.Context, where func(context.Context, uuid.UUID) (string, string, error), id uuid.UUID) (*os.File, error) {
	root, rel, err := where(ctx, id)
	if err != nil {
		return nil, err
	}
	return library.Open(root, rel)
}
