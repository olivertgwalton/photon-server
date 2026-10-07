package analysis

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fingerprinter is media.Tools.Fingerprint, or a test's stand-in.
type fingerprinter func(ctx context.Context, f *os.File, from, length time.Duration) ([]uint32, error)

// shader is media.Tools.Shades, or a test's stand-in.
type shader func(ctx context.Context, f *os.File, from time.Duration) ([]media.Shade, error)

// Markers finds the intro and credits a season's episodes share by comparing their sound, as
// Plex's intro detection and Jellyfin's Intro Skipper do: each copy's first part for the intro,
// its last for the credits. A season with nothing not yet compared is passed over. A film, which
// has no other episode to compare with, has its credits found by its picture (see filmCredits).
func Markers(st *store.Store, fingerprint fingerprinter, shades shader) jobs.Handler {
	return func(ctx context.Context, season uuid.UUID) error {
		ends, err := st.FilmEnds(ctx, season)
		if err != nil {
			return err
		}
		if len(ends) > 0 {
			return filmMarkers(ctx, st, shades, ends)
		}
		parts, err := st.SeasonParts(ctx, season)
		if err != nil || !slices.ContainsFunc(parts, func(p store.SeasonPart) bool { return !p.Fingerprinted }) {
			return err
		}
		var intros, credits []sound
		var introParts, creditParts, compared []uuid.UUID
		for rest := parts; len(rest) > 0; {
			n := 1
			for n < len(rest) && rest[n].Version == rest[0].Version {
				n++
			}
			copyParts := rest[:n]
			rest = rest[n:]
			first, last := copyParts[0], copyParts[n-1]
			p, err := take(ctx, fingerprint, first, domain.MarkerIntro)
			// What is not compared is compared once the server has an FFmpeg that can.
			if errors.Is(err, media.ErrNoChromaprint) {
				return nil
			}
			if err != nil {
				return err
			}
			intros, introParts = append(intros, p), append(introParts, first.ID)
			if p, err = take(ctx, fingerprint, last, domain.MarkerCredits); err != nil {
				return err
			}
			credits, creditParts = append(credits, p), append(creditParts, last.ID)
			for _, p := range copyParts {
				compared = append(compared, p.ID)
			}
		}
		found := map[uuid.UUID][]domain.Marker{}
		for i, m := range shared(domain.MarkerIntro, intros) {
			if m != nil {
				found[introParts[i]] = append(found[introParts[i]], *m)
			}
		}
		for i, m := range shared(domain.MarkerCredits, credits) {
			if m != nil {
				found[creditParts[i]] = append(found[creditParts[i]], *m)
			}
		}
		return st.SaveFoundMarkers(ctx, domain.MarkerByFingerprint, compared, found)
	}
}

// take fingerprints the window of a part where kind would be.
func take(ctx context.Context, fingerprint fingerprinter, part store.SeasonPart, kind domain.MarkerKind) (sound, error) {
	f, err := library.Open(part.Root, part.RelPath)
	if err != nil {
		return sound{}, err
	}
	defer f.Close()
	from, span := window(kind, part.Duration)
	points, err := fingerprint(ctx, f, from, span)
	return sound{episode: part.Episode, from: from, length: part.Duration, points: points}, err
}

// filmMarkers finds the credits at the end of each copy of a film not yet read.
func filmMarkers(ctx context.Context, st *store.Store, shades shader, ends []store.FilmEnd) error {
	found := map[uuid.UUID][]domain.Marker{}
	var read []uuid.UUID
	for _, end := range ends {
		if end.Read {
			continue
		}
		f, err := library.Open(end.Root, end.RelPath)
		if err != nil {
			return err
		}
		got, err := shades(ctx, f, max(end.Duration-domain.MarkerCredits.Longest(), 0))
		_ = f.Close()
		if err != nil {
			return err
		}
		if m := filmCredits(got, end.Duration); m != nil {
			found[end.ID] = []domain.Marker{*m}
		}
		read = append(read, end.ID)
	}
	if len(read) == 0 {
		return nil
	}
	return st.SaveFoundMarkers(ctx, domain.MarkerByBlackFrames, read, found)
}

// How a film's credits are found in its picture, as Intro Skipper's keyframe analysis.
const (
	// darkFrame is how much of a frame is black, in percent, for it to be taken for credits; it
	// rises with the film's own floor, the black of its least black frames (a letterbox's bars),
	// counted to floorCap. Lettering on black leaves most of a frame black.
	darkFrame = 85
	floorCap  = 30
	// tinted is the saturation at which a dark frame is a scene's, not credits' black.
	tinted = 10
	// sceneGap is the longest stretch of other frames inside one dark scene.
	sceneGap = 20 * time.Second
	// lettering is the contrast a frame with text on black has.
	lettering = 60
)

// filmCredits finds a film's credits in the shades of its last part's end: from the first dark
// scene with lettering in most of its frames, at least MarkerShortest long, to the last; to the
// part's end where that is within sceneGap of it. A scene after the credits is not in them.
func filmCredits(shades []media.Shade, end time.Duration) *domain.Marker {
	if len(shades) == 0 {
		return nil
	}
	blacks := make([]int, len(shades))
	for i, s := range shades {
		blacks[i] = s.Black
	}
	slices.Sort(blacks)
	floor := min(blacks[len(blacks)/100], floorCap)
	threshold := darkFrame*(100-floor)/100 + floor
	dark := func(s media.Shade) bool { return s.Black >= threshold && s.Saturation < tinted }

	var first, last *scene
	for _, sc := range scenes(shades, dark) {
		if sc.to-sc.from >= domain.MarkerShortest && sc.lettered*2 > sc.frames {
			if first == nil {
				first = &sc
			}
			last = &sc
		}
	}
	if first == nil {
		return nil
	}
	to := last.to
	if end-to <= sceneGap {
		to = end
	}
	return &domain.Marker{Kind: domain.MarkerCredits, StartMS: first.from.Milliseconds(), EndMS: to.Milliseconds()}
}

// scene is a run of dark keyframes, from the first to the last, how many there are and how many
// are lettered.
type scene struct {
	from, to         time.Duration
	frames, lettered int
}

// scenes are the runs of dark keyframes in shades, the other frames inside each never longer than
// sceneGap.
func scenes(shades []media.Shade, dark func(media.Shade) bool) []scene {
	var out []scene
	for _, s := range shades {
		if !dark(s) {
			continue
		}
		if n := len(out); n == 0 || s.At-out[n-1].to > sceneGap {
			out = append(out, scene{from: s.At})
		}
		sc := &out[len(out)-1]
		sc.to = s.At
		sc.frames++
		if s.Contrast >= lettering {
			sc.lettered++
		}
	}
	return out
}
