// Package identify matches films and shows to metadata providers.
package identify

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
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
	"github.com/olivertgwalton/photon-server/internal/tvdb"
)

var kinds = map[domain.ItemKind]tmdb.Kind{domain.ItemMovie: tmdb.Movie, domain.ItemShow: tmdb.Show}

// Handler matches a film or show on each provider its library takes, and records what each says
// about it and, for a show, its seasons and episodes; the library's order decides whose values
// stand. A title with no confident match is left as its files and NFO describe it.
func Handler(st *store.Store, movies *tmdb.Client, shows *tvdb.Client, log *slog.Logger) jobs.Handler {
	return func(ctx context.Context, id uuid.UUID) error {
		sub, ok, err := st.IdentifySubject(ctx, id)
		if err != nil || !ok {
			return err
		}
		kind, ok := kinds[sub.Kind]
		if !ok {
			return nil
		}
		// TMDB goes first whatever the order, as its match may give TVDB an id to find the show by.
		if slices.Contains(sub.Sources, domain.SourceTMDB) {
			if err := matchTMDB(ctx, st, movies, kind, id, &sub, log); err != nil {
				return err
			}
		}
		if kind == tmdb.Show && slices.Contains(sub.Sources, domain.SourceTVDB) {
			return matchTVDB(ctx, st, shows, id, &sub, log)
		}
		return nil
	}
}

func matchTMDB(ctx context.Context, st *store.Store, c *tmdb.Client, kind tmdb.Kind, id uuid.UUID, sub *store.Subject, log *slog.Logger) error {
	match, err := resolve(*sub, domain.ProviderTMDB, []domain.Provider{domain.ProviderIMDb, domain.ProviderTVDB},
		func(p domain.Provider, v string) ([]domain.Candidate, error) { return c.Find(ctx, kind, p, v) },
		func(title string, year int) ([]domain.Candidate, error) { return c.Search(ctx, kind, title, year) })
	if err != nil || match == 0 {
		unmatched(ctx, log, "tmdb", *sub, err)
		return err
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
	learn(sub, m.IDs)
	return st.SaveIdentity(ctx, id, domain.SourceTMDB, m, seasons)
}

func matchTVDB(ctx context.Context, st *store.Store, c *tvdb.Client, id uuid.UUID, sub *store.Subject, log *slog.Logger) error {
	match, err := resolve(*sub, domain.ProviderTVDB, []domain.Provider{domain.ProviderIMDb, domain.ProviderTMDB},
		func(_ domain.Provider, v string) ([]domain.Candidate, error) { return c.Find(ctx, v) },
		func(title string, year int) ([]domain.Candidate, error) { return c.Search(ctx, title, year) })
	if err != nil || match == 0 {
		unmatched(ctx, log, "tvdb", *sub, err)
		return err
	}
	m, err := c.Details(ctx, match)
	if err != nil {
		return err
	}
	seasons, err := c.Seasons(ctx, match, sub.Seasons)
	if err != nil {
		return err
	}
	learn(sub, m.IDs)
	return st.SaveIdentity(ctx, id, domain.SourceTVDB, m, seasons)
}

func unmatched(ctx context.Context, log *slog.Logger, provider string, sub store.Subject, err error) {
	if err == nil {
		log.InfoContext(ctx, "no confident match", slog.String("provider", provider),
			slog.String("title", sub.Title), slog.Int("year", sub.Year))
	}
}

// learn keeps the ids a match gave, for the next provider to find the title by.
func learn(sub *store.Subject, ids map[domain.Provider]string) {
	for p, v := range ids {
		if _, ok := sub.IDs[p]; !ok {
			sub.IDs[p] = v
		}
	}
}

// resolve finds a title's id on a provider: one it already carries, else one another provider's
// id leads to, else a confident search result. Zero is no match.
func resolve(sub store.Subject, own domain.Provider, others []domain.Provider,
	find func(domain.Provider, string) ([]domain.Candidate, error),
	search func(string, int) ([]domain.Candidate, error),
) (int, error) {
	if id, err := strconv.Atoi(sub.IDs[own]); err == nil {
		return id, nil
	}
	for _, p := range others {
		if v := sub.IDs[p]; v != "" {
			found, err := find(p, v)
			if err != nil {
				return 0, err
			}
			if len(found) > 0 {
				return found[0].ID, nil
			}
		}
	}
	found, err := search(sub.Title, sub.Year)
	if err != nil {
		return 0, err
	}
	// A year read from a folder is often the wrong one of several release dates; Jellyfin asks
	// again without it.
	if len(found) == 0 && sub.Year != 0 {
		if found, err = search(sub.Title, 0); err != nil {
			return 0, err
		}
	}
	return pick(found, sub.Title, sub.Year), nil
}

// pick takes the first result, in the provider's order, whose title or original title is the one asked
// for and whose year is within one of the one asked for. Anything looser is left unmatched rather
// than risk a wrong match.
func pick(found []domain.Candidate, title string, year int) int {
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

// disambiguated is the year a provider adds to tell two titles of one name apart: TVDB's
// "Doctor Who (2005)".
var disambiguated = regexp.MustCompile(`\s*\(\d{4}\)$`)

// normalise compares titles as a reader would: without case, accents, punctuation or a year
// added to tell them apart.
func normalise(s string) string {
	s = disambiguated.ReplaceAllString(strings.TrimSpace(s), "")
	s, _, _ = transform.String(unmark, strings.ToLower(s))
	s = strings.ReplaceAll(s, "&", " and ")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func abs(n int) int {
	return max(n, -n)
}
