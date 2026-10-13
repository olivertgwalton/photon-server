package naming

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	seasonToken    = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\]])s(\d{1,4})(?:$|[ ._\-\[\]])`)
	seasonKeywords = `시즌|シーズン|сезон|season|sæson|saison|staffel|series|stagione|säsong|seizoen|seasong|sezon|sezona|sezóna|sezonul|série|séria|serie|seria|temporada|kausi`
	seasonAfterKW  = regexp.MustCompile(`(?i)^(?:` + seasonKeywords + `)\s*(\d+)(?:\s*$|\s*[(\[]|\s+[^eE\d])`)
	seasonBeforeKW = regexp.MustCompile(`(?i)^(\d+)(?:st|nd|rd|th|\.)?\s*(?:` + seasonKeywords + `)\s*$`)
)

// ParseSeason reads a folder under a series: "Season 1", "S01", "3.Staffel", "Specials", or a
// bare number. A folder that names an episode is not a season.
func ParseSeason(folder, series string) (int, bool) {
	if _, ok := parseSxxEyy(folder, ""); ok {
		return 0, false
	}
	if m := seasonToken.FindStringSubmatch(folder); m != nil && validSeason(atoi(m[1])) {
		return atoi(m[1]), true
	}
	name := withoutSeries(folder, series)
	if strings.EqualFold(name, "specials") {
		return 0, true
	}
	if n, err := strconv.Atoi(name); err == nil && validSeason(n) {
		return n, true
	}
	return seasonInWords(name)
}

// SaysSeason is whether a folder under one named series names a season in words, "Season 2" or
// "3. Staffel", as Jellyfin asks of a folder in a library of films and shows before it takes the
// folder above for a show's: a bare number, "S02" or "Specials" could as well be a film's.
func SaysSeason(folder, series string) bool {
	if _, ok := parseSxxEyy(folder, ""); ok {
		return false
	}
	_, ok := seasonInWords(withoutSeries(folder, series))
	return ok
}

// withoutSeries is a season folder's name with its series' name before it left off.
func withoutSeries(folder, series string) string {
	name := strings.TrimSpace(folder)
	if s := strings.TrimSpace(series); s != "" && len(name) > len(s) && strings.EqualFold(name[:len(s)], s) {
		name = strings.TrimLeft(name[len(s):], " ._-")
	}
	return name
}

func seasonInWords(name string) (int, bool) {
	for _, re := range []*regexp.Regexp{seasonAfterKW, seasonBeforeKW} {
		if m := re.FindStringSubmatch(name); m != nil && validSeason(atoi(m[1])) {
			return atoi(m[1]), true
		}
	}
	return 0, false
}

var trailingSeason = regexp.MustCompile(`(?i)\s+s(?:eason)?\s*\d{1,4}$`)

// SeriesName reads a series folder: "The.Show.S01" and "Bunker.S03.1080p.WEB-DL" name their series
// before the season.
func SeriesName(folder string) Name {
	n := CleanName(folder)
	n.Title = trailingSeason.ReplaceAllString(n.Title, "")
	return n
}
