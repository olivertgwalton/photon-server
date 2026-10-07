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
	Keyframes(ctx context.Context, part uuid.UUID) (store.PartKeyframes, error)
	AskKeyframes(ctx context.Context, part uuid.UUID) error
}

type remuxer interface {
	Open(ctx context.Context, playback uuid.UUID, c hls.Copy) error
}

// Remuxes opens a playback's copy as HLS: each of its files cut at its keyframes where the video
// is copied and they are known, which are indexed in the background as a library is scanned; or
// every SegmentLength where it is encoded or none are known, as Jellyfin cuts a file it has no
// keyframes for. A play never waits on a file being read for them.
type Remuxes struct {
	parts partStore
	hls   remuxer
}

func NewRemuxes(parts partStore, h remuxer) *Remuxes {
	return &Remuxes{parts: parts, hls: h}
}

// Open starts a playback's HLS of a copy at start, in segments of the format asked for, carrying
// its video and audio as decided, and, unless a subtitle is drawn into the picture, its text
// subtitles, embedded and beside it, as WebVTT.
func (r *Remuxes) Open(ctx context.Context, playback uuid.UUID, c store.PlayCopy, video domain.VideoPlan, audio *domain.AudioPlan, segments domain.SegmentFormat, start time.Duration) error {
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
			known, err := r.keyframes(ctx, p.ID)
			if err != nil {
				return err
			}
			if len(known) > 0 {
				keyframes = known
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
	h := hls.Copy{Parts: sources, Variant: variant(c.Streams, video, audio, kbps), Start: start, Segments: segments}
	// A subtitle drawn into the picture is the only one offered, as Jellyfin's master playlist
	// has it: another turned on by a player would be drawn over it.
	if e := video.Encode; e != nil && e.Burn != nil {
		return r.hls.Open(ctx, playback, h)
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
	return r.hls.Open(ctx, playback, h)
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

// keyframes answers a part's keyframes where they are known. One its library finds but has not
// reached yet has its job moved to the front, so the next play is cut at them.
func (r *Remuxes) keyframes(ctx context.Context, part uuid.UUID) ([]time.Duration, error) {
	known, err := r.parts.Keyframes(ctx, part)
	if err != nil {
		return nil, err
	}
	if known.PtsMS == nil && known.Mode != domain.KeyframesOff {
		if err := r.parts.AskKeyframes(ctx, part); err != nil {
			return nil, err
		}
	}
	keyframes := make([]time.Duration, len(known.PtsMS))
	for k, ms := range known.PtsMS {
		keyframes[k] = time.Duration(ms) * time.Millisecond
	}
	return keyframes, nil
}

// openFile opens a file of a library the scanner recorded.
func openFile(ctx context.Context, where func(context.Context, uuid.UUID) (string, string, error), id uuid.UUID) (*os.File, error) {
	root, rel, err := where(ctx, id)
	if err != nil {
		return nil, err
	}
	return library.Open(root, rel)
}
