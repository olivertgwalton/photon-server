package naming

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Name struct {
	Title   string
	Year    int
	IDs     IDs
	Edition string
}

type IDs struct {
	TMDB string
	IMDb string
	TVDB string
}

var (
	// Jellyfin's [tmdbid-603] and [tmdb=603], Plex's {tmdb-603}, in any bracket.
	idTag      = regexp.MustCompile(`(?i)[\[({](tmdb|imdb|tvdb)(?:id)?[-=]\s*([^\])}]+?)\s*[\])}]`)
	editionTag = regexp.MustCompile(`(?i)\{edition-([^}]{1,32})\}`)
	leadGroup  = regexp.MustCompile(`^\s*\[[^\]]+\]\s*`)
	trailEpNum = regexp.MustCompile(`\s+-\s+\d+\s*$`)
)

// CleanName reads a stem: a file name without its extension, or a folder name.
func CleanName(stem string) Name {
	var n Name
	stem = idTag.ReplaceAllStringFunc(stem, func(tag string) string {
		m := idTag.FindStringSubmatch(tag)
		switch strings.ToLower(m[1]) {
		case "tmdb":
			n.IDs.TMDB = m[2]
		case "imdb":
			n.IDs.IMDb = m[2]
		case "tvdb":
			n.IDs.TVDB = m[2]
		}
		return " "
	})
	stem = editionTag.ReplaceAllStringFunc(stem, func(tag string) string {
		n.Edition = strings.TrimSpace(editionTag.FindStringSubmatch(tag)[1])
		return " "
	})
	if rest := leadGroup.ReplaceAllString(stem, ""); strings.TrimSpace(rest) != "" {
		stem = rest
	}

	toks := tokenize(stem)
	end := len(stem)
	if i := yearToken(toks); i >= 0 {
		n.Year, _ = strconv.Atoi(toks[i].text)
		end = toks[i].start
	} else if i := releaseToken(toks); i > 0 {
		end = toks[i].start
	}
	title := strings.TrimRight(stem[:end], " _-([{,")
	title = trailEpNum.ReplaceAllString(title, "")
	n.Title = spaced(title)
	return n
}

type token struct {
	text       string
	start, end int
	bracketed  bool
}

func isSeparator(r rune) bool {
	return strings.ContainsRune(" ._-[](){},", r)
}

func tokenize(s string) []token {
	var toks []token
	depth := 0
	start := -1
	for i, r := range s {
		if !isSeparator(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			toks = append(toks, token{text: s[start:i], start: start, end: i, bracketed: depth > 0})
			start = -1
		}
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth = max(depth-1, 0)
		}
	}
	if start >= 0 {
		toks = append(toks, token{text: s[start:], start: start, end: len(s), bracketed: depth > 0})
	}
	return toks
}

func isYear(s string) bool {
	if len(s) != 4 || (s[:2] != "19" && s[:2] != "20") {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

func isTwoDigits(s string) bool {
	return len(s) == 2 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9'
}

// yearToken picks the release year: the last bracketed year, else the last bare one. A year that
// opens a date (2013-12-09) is a date, and the first token is the title, so 1917 and 2012 keep
// their names.
func yearToken(toks []token) int {
	found := -1
	for i := 1; i < len(toks); i++ {
		if !isYear(toks[i].text) {
			continue
		}
		if i+2 < len(toks) && isTwoDigits(toks[i+1].text) && isTwoDigits(toks[i+2].text) {
			continue
		}
		if found < 0 || toks[i].bracketed || !toks[found].bracketed {
			found = i
		}
	}
	return found
}

// releaseTags are the words a release name appends after the title. Some are real words (dc, se,
// multi, limited), which is why a year, when there is one, ends the title before any of these.
var releaseTags = []string{
	"3d", "sbs", "tab", "hsbs", "htab", "mvc", "hdr", "hdc", "uhd", "ultrahd", "4k",
	"ac3", "dts", "aac", "custom", "dc", "divx", "divx5", "dsr", "dsrip", "dutch", "dvd",
	"dvdrip", "dvdscr", "dvdscreener", "screener", "dvdivx", "cam", "fragment", "fs", "hdtv",
	"hdrip", "hdtvrip", "internal", "limited", "multi", "subs", "ntsc", "ogg", "ogm", "pal",
	"pdtv", "proper", "repack", "rerip", "retail", "r5", "bd5", "bd", "se", "svcd", "swedish",
	"german", "read.nfo", "nfofix", "unrated", "ws", "web-dl", "webrip", "telesync", "ts",
	"telecine", "tc", "brrip", "bdrip", "480p", "480i", "576p", "576i", "720p", "720i", "1080p",
	"1080i", "2160p", "hrhd", "hrhdtv", "hddvd", "bluray", "blu-ray", "x264", "x265", "h264",
	"h265", "hevc", "xvid", "xvidvd", "xxx", "remastered",
	"cd1", "cd2", "cd3", "cd4", "cd5", "cd6", "cd7", "cd8", "cd9",
}

// releaseToken is the first token, or hyphen- or dot-joined pair, that is a release tag.
func releaseToken(toks []token) int {
	for i, t := range toks {
		forms := []string{strings.ToLower(t.text)}
		if i+1 < len(toks) {
			next := strings.ToLower(toks[i+1].text)
			forms = append(forms, forms[0]+"-"+next, forms[0]+"."+next)
		}
		for _, f := range forms {
			if slices.Contains(releaseTags, f) {
				return i
			}
		}
	}
	return -1
}

// spaced turns a dotted release name into words. Only a name with no spaces is dotted, and a dot
// between single letters stays, so S.H.I.E.L.D. keeps its dots.
func spaced(s string) string {
	if !strings.ContainsRune(s, ' ') {
		var b strings.Builder
		runs := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' })
		for i, run := range runs {
			if i > 0 {
				if utf8.RuneCountInString(runs[i-1]) == 1 && utf8.RuneCountInString(run) == 1 {
					b.WriteByte('.')
				} else {
					b.WriteByte(' ')
				}
			}
			b.WriteString(run)
		}
		if strings.HasSuffix(s, ".") && len(runs) > 0 && utf8.RuneCountInString(runs[len(runs)-1]) == 1 {
			b.WriteByte('.')
		}
		s = b.String()
	}
	return strings.Join(strings.Fields(s), " ")
}
