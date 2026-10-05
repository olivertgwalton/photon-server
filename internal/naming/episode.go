package naming

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Confidence string

const (
	// ConfidenceHigh is a canonical form (S01E02, 1x02, an air date): safe to group as versions.
	ConfidenceHigh Confidence = "high"
	// ConfidenceMedium is a bare number read as an episode: playable, never grouped as versions.
	ConfidenceMedium Confidence = "medium"
)

type Episode struct {
	// Season is nil when the name does not say; the season folder or season 1 decides. Season 0 is
	// specials.
	Season     *int
	Episodes   []int
	AirDate    time.Time
	Confidence Confidence
	Rule       string
}

type episodeRule struct {
	name       string
	confidence Confidence
	parse      func(stem, series string) (Episode, bool)
}

// episodeRules are tried in order; canonical forms come before the bare numbers that would
// otherwise read a canonical name's digits as an episode.
var episodeRules = []episodeRule{
	{"sxxeyy", ConfidenceHigh, parseSxxEyy},
	{"nxnn", ConfidenceHigh, parseNxNN},
	{"season_episode_words", ConfidenceHigh, parseSeasonEpisodeWords},
	{"air_date", ConfidenceHigh, parseAirDate},
	{"episode_word", ConfidenceHigh, parseEpisodeWord},
	{"e_token", ConfidenceHigh, parseEToken},
	{"dash_number", ConfidenceMedium, parseDashNumber},
	{"bracket_number", ConfidenceMedium, parseBracketNumber},
	{"series_then_season_episode_digits", ConfidenceMedium, parseSeriesDigits},
	{"leading_number", ConfidenceMedium, parseLeadingNumber},
	{"dash_number_inside", ConfidenceMedium, parseInnerDashNumber},
	{"trailing_number", ConfidenceMedium, parseTrailingNumber},
}

// ParseEpisode reads an episode's file name. series is the series folder's name, which a few
// bare-number forms need to tell the series' own words from the episode number.
func ParseEpisode(stem, series string) (Episode, bool) {
	for _, r := range episodeRules {
		if ep, ok := r.parse(stem, series); ok {
			ep.Confidence = r.confidence
			ep.Rule = r.name
			return ep, true
		}
	}
	return Episode{}, false
}

// A season of 200–1927 or past 2500 is a resolution or a year, as in "Special (1920x1080)".
func validSeason(n int) bool {
	return n < 200 || (n > 1927 && n <= 2500)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

var (
	sxxeyy       = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(\d{1,4})[ ._-]*e(\d{1,4})`)
	nxnn         = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s?(\d{1,4})x(\d{1,3})(?:[^0-9]|$)`)
	continuation = regexp.MustCompile(`(?i)^(?:\s*-\s*|-?)(?:s\d{1,4})?[ex]?(\d{1,4})`)
)

// moreEpisodes reads the episodes that continue a canonical match: E01E02, E23-E24-E26, 02x03-04.
// A number followed by p, i or another digit is a resolution, not an episode, and a number lower
// than the first belongs to the title ("S03E21 - E2").
func moreEpisodes(first int, rest string) []int {
	last := first
	for {
		m := continuation.FindStringSubmatchIndex(rest)
		if m == nil || m[1] == 0 {
			break
		}
		if !strings.ContainsAny(rest[:m[2]], "-eExX") {
			break
		}
		if next := rest[m[1]:]; next != "" && strings.ContainsRune("0123456789pPiI", rune(next[0])) {
			break
		}
		n := atoi(rest[m[2]:m[3]])
		if n <= last {
			break
		}
		last = n
		rest = rest[m[1]:]
	}
	eps := make([]int, 0, last-first+1)
	for n := first; n <= last; n++ {
		eps = append(eps, n)
	}
	return eps
}

func parseSxxEyy(stem, _ string) (Episode, bool) {
	m := sxxeyy.FindStringSubmatchIndex(stem)
	if m == nil || !validSeason(atoi(stem[m[2]:m[3]])) {
		return Episode{}, false
	}
	first := atoi(stem[m[4]:m[5]])
	return Episode{Season: new(atoi(stem[m[2]:m[3]])), Episodes: moreEpisodes(first, stem[m[5]:])}, true
}

func parseNxNN(stem, _ string) (Episode, bool) {
	m := nxnn.FindStringSubmatchIndex(stem)
	if m == nil || !validSeason(atoi(stem[m[2]:m[3]])) {
		return Episode{}, false
	}
	first := atoi(stem[m[4]:m[5]])
	return Episode{Season: new(atoi(stem[m[2]:m[3]])), Episodes: moreEpisodes(first, stem[m[5]:])}, true
}

var seasonEpisodeWords = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])s(?:eason)?\s*(\d{1,4})\s+e(?:pisode)?\s*(\d{1,4})`)

func parseSeasonEpisodeWords(stem, _ string) (Episode, bool) {
	m := seasonEpisodeWords.FindStringSubmatch(stem)
	if m == nil || !validSeason(atoi(m[1])) {
		return Episode{}, false
	}
	return Episode{Season: new(atoi(m[1])), Episodes: []int{atoi(m[2])}}, true
}

var (
	yearFirstDate = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)\d{2})[._ -](\d{2})[._ -](\d{2})(?:[^0-9]|$)`)
	dayFirstDate  = regexp.MustCompile(`(?:^|[^0-9])(\d{2})[._ -](\d{2})[._ -]((?:19|20)\d{2})(?:[^0-9]|$)`)
)

func date(y, m, d int) (time.Time, bool) {
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t, t.Year() == y && int(t.Month()) == m && t.Day() == d
}

// A day-first date that is not a valid day-first date is read month-first, never guessed.
func parseAirDate(stem, _ string) (Episode, bool) {
	if m := yearFirstDate.FindStringSubmatch(stem); m != nil {
		if t, ok := date(atoi(m[1]), atoi(m[2]), atoi(m[3])); ok {
			return Episode{AirDate: t}, true
		}
	}
	if m := dayFirstDate.FindStringSubmatch(stem); m != nil {
		if t, ok := date(atoi(m[3]), atoi(m[2]), atoi(m[1])); ok {
			return Episode{AirDate: t}, true
		}
		if t, ok := date(atoi(m[3]), atoi(m[1]), atoi(m[2])); ok {
			return Episode{AirDate: t}, true
		}
	}
	return Episode{}, false
}

var episodeWord = regexp.MustCompile(`(?i)(?:^|[^a-z])episode\s*(\d{1,4})(?:-(\d{1,4}))?`)

func parseEpisodeWord(stem, _ string) (Episode, bool) {
	m := episodeWord.FindStringSubmatch(stem)
	if m == nil {
		return Episode{}, false
	}
	first := atoi(m[1])
	if m[2] != "" && atoi(m[2]) > first {
		return Episode{Episodes: moreEpisodes(first, "-"+m[2])}, true
	}
	return Episode{Episodes: []int{first}}, true
}

var eToken = regexp.MustCompile(`(?i)^ep?(\d{1,4})$`)

func parseEToken(stem, _ string) (Episode, bool) {
	for _, t := range tokenize(stem) {
		if m := eToken.FindStringSubmatch(t.text); m != nil {
			return Episode{Episodes: []int{atoi(m[1])}}, true
		}
	}
	return Episode{}, false
}

var dashNumber = regexp.MustCompile(`\s-\s(\d{1,4})\s*(?:\[[^\]]*\]\s*|\([^)]*\)\s*)*$`)

// "Show - 101 [720p]": an absolute number ending the name, as anime is released.
func parseDashNumber(stem, _ string) (Episode, bool) {
	m := dashNumber.FindStringSubmatch(stem)
	if m == nil || isYear(m[1]) {
		return Episode{}, false
	}
	return Episode{Episodes: []int{atoi(m[1])}}, true
}

var bracketNumber = regexp.MustCompile(`\[(\d{1,4})\]`)

// "[Group][Series][21][1080p]".
func parseBracketNumber(stem, _ string) (Episode, bool) {
	m := bracketNumber.FindStringSubmatch(stem)
	if m == nil || isYear(m[1]) {
		return Episode{}, false
	}
	return Episode{Episodes: []int{atoi(m[1])}}, true
}

// "Seinfeld 0807 The Checks": season and episode run together right after the series' name. Only
// with a leading zero, so "One Piece 1001" stays an absolute number.
func parseSeriesDigits(stem, series string) (Episode, bool) {
	words := tokenize(series)
	toks := tokenize(stem)
	if len(words) == 0 || len(toks) <= len(words) {
		return Episode{}, false
	}
	for i, w := range words {
		if !strings.EqualFold(w.text, toks[i].text) {
			return Episode{}, false
		}
	}
	d := toks[len(words)].text
	if len(d) != 4 || d[0] != '0' || strings.Trim(d, "0123456789") != "" {
		return Episode{}, false
	}
	return Episode{Season: new(atoi(d[:len(d)-2])), Episodes: []int{atoi(d[len(d)-2:])}}, true
}

var leadingNumber = regexp.MustCompile(`^(\d{1,3})(?:-(\d{2,3}))?(?:\s*-\s*|[.\s]|$)`)

// "01 - Pilot", "01.Pilot", "02-04 - Title" under a season folder.
func parseLeadingNumber(stem, _ string) (Episode, bool) {
	m := leadingNumber.FindStringSubmatch(stem)
	if m == nil {
		return Episode{}, false
	}
	first := atoi(m[1])
	if m[2] != "" && atoi(m[2]) > first {
		return Episode{Episodes: moreEpisodes(first, "-"+m[2])}, true
	}
	return Episode{Episodes: []int{first}}, true
}

var innerDashNumber = regexp.MustCompile(`\s-\s(\d{1,3})\s-\s`)

// "Show - 01 - Title".
func parseInnerDashNumber(stem, _ string) (Episode, bool) {
	m := innerDashNumber.FindStringSubmatch(stem)
	if m == nil {
		return Episode{}, false
	}
	return Episode{Episodes: []int{atoi(m[1])}}, true
}

// "One Piece 1001": a last token of digits after the series' own words.
func parseTrailingNumber(stem, _ string) (Episode, bool) {
	toks := tokenize(stem)
	if len(toks) < 2 {
		return Episode{}, false
	}
	d := toks[len(toks)-1].text
	if len(d) > 4 || isYear(d) || strings.Trim(d, "0123456789") != "" {
		return Episode{}, false
	}
	return Episode{Episodes: []int{atoi(d)}}, true
}
