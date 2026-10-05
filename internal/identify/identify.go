// Package identify matches films and shows to TMDB.
package identify

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"uuid"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/jobs"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/tmdb"
)

var kinds = map[domain.ItemKind]tmdb.Kind{domain.ItemMovie: tmdb.Movie, domain.ItemShow: tmdb.Show}

// Handler matches a film or show and records what TMDB says about it and, for a show, its
// seasons and episodes. A title with no confident match is left as its files and NFO describe it.
func Handler(st *store.Store, c *tmdb.Client, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		sub, ok, err := st.IdentifySubject(ctx, id)
		if err != nil || !ok {
			return err
		}
		kind, ok := kinds[sub.Kind]
		if !ok || !slices.Contains(sub.Sources, domain.SourceTMDB) {
			return nil
		}
		match, err := resolve(ctx, c, kind, sub)
		if err != nil {
			return err
		}
		if match == 0 {
			log.InfoContext(ctx, "no confident match", slog.String("title", sub.Title), slog.Int("year", sub.Year))
			return nil
		}
		m, err := c.Details(ctx, kind, match)
		if err != nil {
			return err
		}
		seasons := map[int]domain.SeasonMetadata{}
		for _, n := range sub.Seasons {
			s, err := c.Season(ctx, match, n)
			if errors.Is(err, tmdb.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			seasons[n] = s
		}
		return st.SaveIdentity(ctx, id, m, seasons)
	}
}

// resolve finds the title's TMDB id: one it already carries, else one another provider's id leads
// to, else a confident search result. Zero is no match.
func resolve(ctx context.Context, c *tmdb.Client, kind tmdb.Kind, sub store.Subject) (int, error) {
	if id, err := strconv.Atoi(sub.IDs[domain.ProviderTMDB]); err == nil {
		return id, nil
	}
	for _, p := range []domain.Provider{domain.ProviderIMDb, domain.ProviderTVDB} {
		if v := sub.IDs[p]; v != "" {
			found, err := c.Find(ctx, kind, p, v)
			if err != nil {
				return 0, err
			}
			if len(found) > 0 {
				return found[0].ID, nil
			}
		}
	}
	found, err := c.Search(ctx, kind, sub.Title, sub.Year)
	if err != nil {
		return 0, err
	}
	// A year read from a folder is often the wrong one of several release dates; Jellyfin asks
	// again without it.
	if len(found) == 0 && sub.Year != 0 {
		if found, err = c.Search(ctx, kind, sub.Title, 0); err != nil {
			return 0, err
		}
	}
	return pick(found, sub.Title, sub.Year), nil
}

// pick takes the first result, in TMDB's order, whose title or original title is the one asked
// for and whose year is within one of the one asked for. Anything looser is left unmatched rather
// than risk a wrong match.
func pick(found []tmdb.Match, title string, year int) int {
	want := normalise(title)
	for _, m := range found {
		named := normalise(m.Title) == want || normalise(m.OriginalTitle) == want
		dated := year == 0 || m.Year == 0 || abs(m.Year-year) <= 1
		if named && dated {
			return m.ID
		}
	}
	return 0
}

var unmark = transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// normalise compares titles as a reader would: without case, accents or punctuation.
func normalise(s string) string {
	s, _, _ = transform.String(unmark, strings.ToLower(s))
	s = strings.ReplaceAll(s, "&", " and ")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func abs(n int) int {
	return max(n, -n)
}
