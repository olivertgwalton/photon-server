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
// id leads to, else a confident search result. Empty is no match.
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

// pick takes the first result, in the provider's order, whose title or original title is the one asked
// for and whose year is within one of the one asked for. Anything looser is left unmatched rather
// than risk a wrong match.
func pick(found []domain.Candidate, title string, year int) string {
	want := normalise(title)
	for _, m := range found {
		named := normalise(m.Title) == want || normalise(m.OriginalTitle) == want
		dated := year == 0 || m.Year == 0 || abs(m.Year-year) <= 1
		if named && dated {
			return m.ID
		}
	}
	return ""
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
	s, _, _ = transform.String(unmark(), strings.ToLower(s))
	s = strings.ReplaceAll(s, "&", " and ")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

func abs(n int) int {
	return max(n, -n)
}
