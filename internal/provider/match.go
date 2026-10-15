package provider

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Resolve finds a title's id on a provider: one it already carries, else one another provider's
// id leads to, else the search result that best matches its name and year. Empty is no match.
func Resolve(h Hints, own domain.Provider, others []domain.Provider,
	find func(domain.Provider, string) ([]domain.Candidate, error),
	search func(string, int) ([]domain.Candidate, error),
) (string, error) {
	if id := h.IDs[own]; id != "" {
		if _, err := strconv.Atoi(id); err == nil {
			return id, nil
		}
	}
	for _, p := range others {
		if v := h.IDs[p]; v != "" {
			found, err := find(p, v)
			if err != nil {
				return "", err
			}
			if len(found) > 0 {
				return found[0].ID, nil
			}
		}
	}
	found, err := search(h.Title, h.Year)
	if err != nil {
		return "", err
	}
	// A year read from a folder is often the wrong one of several release dates; Jellyfin asks
	// again without it.
	if len(found) == 0 && h.Year != 0 {
		if found, err = search(h.Title, 0); err != nil {
			return "", err
		}
	}
	return pick(found, h.Title, h.Year), nil
}

// How a search result is scored against the name and year asked for, as Jellyfin's TMDB provider
// scores them: its title or original title the same, or the name asked for followed by more words;
// its year within one, as release dates straddle a new year.
const (
	titleExact  = 8
	titlePrefix = 4
	yearNear    = 2
)

// pick takes the search result that best matches the name and year asked for, the provider's own
// first where none scores higher, as Jellyfin does: a provider's search knows titles by names the
// result does not carry, as TMDB's finds "Sousou no Frieren" by its romanised Japanese title.
func pick(found []domain.Candidate, title string, year int) string {
	if len(found) == 0 {
		return ""
	}
	want := normalise(title)
	best, bestScore := found[0], 0
	for _, m := range found {
		score := max(scoreTitle(want, m.Title), scoreTitle(want, m.OriginalTitle))
		if year != 0 && m.Year != 0 && abs(m.Year-year) <= 1 {
			score += yearNear
		}
		// Strictly higher, so a tie keeps the provider's earlier result.
		if score > bestScore {
			best, bestScore = m, score
		}
	}
	return best.ID
}

func scoreTitle(want, title string) int {
	have := normalise(title)
	switch {
	case have == want:
		return titleExact
	// Whole words only, or "Wall" would half match "Wall Street".
	case want != "" && strings.HasPrefix(have, want+" "):
		return titlePrefix
	}
	return 0
}

// unmark is made per call: a chained transformer keeps state, so one shared by two identify jobs
// panics.
func unmark() transform.Transformer {
	return transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
}

// disambiguated is the year a provider adds to tell two titles of one name apart: TVDB's
// "Doctor Who (2005)".
var disambiguated = regexp.MustCompile(`\s*\(\d{4}\)$`)

// normalise compares titles as a reader would: without case, accents, punctuation or a year
// added to tell them apart.
func normalise(s string) string {
	s = disambiguated.ReplaceAllString(strings.TrimSpace(s), "")
	s = strings.ToLower(s)
	if unmarked, _, err := transform.String(unmark(), s); err == nil {
		s = unmarked
	}
	s = strings.ReplaceAll(s, "&", " and ")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func abs(n int) int {
	return max(n, -n)
}
