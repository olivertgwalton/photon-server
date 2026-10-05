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
	"github.com/olivertgwalton/photon-server/internal/media"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// fingerprinter is media.Tools.Fingerprint, or a test's stand-in.
type fingerprinter func(ctx context.Context, f *os.File, from, length time.Duration) ([]uint32, error)

// Markers finds the intro and credits a season's episodes share by comparing their sound, as
// Plex's intro detection and Jellyfin's Intro Skipper do: each copy's first part for the intro,
// its last for the credits. A season with nothing not yet compared is passed over.
func Markers(st *store.Store, fingerprint fingerprinter) jobs.Handler {
	return func(ctx context.Context, season uuid.UUID) error {
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
		return st.SaveFingerprintMarkers(ctx, compared, found)
	}
}

// take fingerprints the window of a part where kind would be.
func take(ctx context.Context, fingerprint fingerprinter, part store.SeasonPart, kind domain.MarkerKind) (sound, error) {
	r, err := os.OpenRoot(part.Root)
	if err != nil {
		return sound{}, err
	}
	defer r.Close()
	f, err := r.Open(part.RelPath)
	if err != nil {
		return sound{}, err
	}
	defer f.Close()
	from, span := window(kind, part.Duration)
	points, err := fingerprint(ctx, f, from, span)
	return sound{episode: part.Episode, from: from, length: part.Duration, points: points}, err
}
