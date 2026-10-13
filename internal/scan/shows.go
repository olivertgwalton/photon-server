package scan

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/library"
	"github.com/olivertgwalton/photon-server/internal/naming"
)

// episodePlan is one episode a folder holds, with every copy of it.
type episodePlan struct {
	season   int
	episodes []int
	airDate  time.Time
	title    string
	name     naming.Name
	// byNumber is true when the episode was read from a canonical form, so its numbers may join it
	// to an episode already known; a bare number never does.
	byNumber bool
	versions []copyPlan
}

// holdsEpisodes reports whether a folder of a series can hold episodes: nothing under an extras
// folder below the series' own, and nothing in no series.
func holdsEpisodes(series, rel string) bool {
	if series == "" {
		return false
	}
	for _, p := range below(series, rel) {
		if _, extras := naming.ExtraFolder(p); extras {
			return false
		}
	}
	return true
}

// planEpisodes reads one folder of a series. A file's own season wins over its folder's, and an
// episode named by date with no season takes its air year, as Plex files daily shows. Copies of an
// episode are grouped only on a canonical key, never on a bare number, so two "- 01" files stay
// two episodes rather than silently merging (Jellyfin #18081).
func planEpisodes(f library.Folder, folderSeason *int, series string) (episodes []episodePlan, unread []string) {
	byKey := map[string]int{}
	copies := stacks(f.Files)
	for _, c := range copies {
		s := stem(c[0].Name)
		if p, ok := naming.StackPart(s); ok {
			s = p.Base
		}
		ep, ok := naming.ParseEpisode(s, series)
		if !ok {
			unread = append(unread, path.Join(f.Path, c[0].Name))
			continue
		}
		n := naming.CleanName(s)
		plan := episodePlan{
			episodes: ep.Episodes, airDate: ep.AirDate, title: episodeTitle(ep), name: n,
			byNumber: ep.Confidence == naming.ConfidenceHigh,
			versions: []copyPlan{{parts: c, edition: n.Edition, subtitles: subtitlesFor(f.Files, copyStem(c), "", len(copies) == 1)}},
		}
		switch {
		case ep.Season != nil:
			plan.season = *ep.Season
		case folderSeason != nil:
			plan.season = *folderSeason
		case !ep.AirDate.IsZero():
			plan.season = ep.AirDate.Year()
		default:
			plan.season = 1
		}
		if plan.byNumber {
			key := episodeKey(plan)
			if i, seen := byKey[key]; seen {
				episodes[i].versions = append(episodes[i].versions, plan.versions...)
				continue
			}
			byKey[key] = len(episodes)
		}
		episodes = append(episodes, plan)
	}
	return episodes, unread
}

func episodeKey(p episodePlan) string {
	var b strings.Builder
	b.WriteString(p.airDate.Format(time.DateOnly))
	for _, e := range append([]int{p.season}, p.episodes...) {
		b.WriteByte('/')
		b.WriteString(strconv.Itoa(e))
	}
	return b.String()
}

// episodeTitle is what an episode is called until metadata names it: its name's own title, else
// its number or air date, as Plex calls one it has not matched. Never the file's whole name.
func episodeTitle(ep naming.Episode) string {
	switch {
	case ep.Title != "":
		return ep.Title
	case len(ep.Episodes) > 0:
		return "Episode " + strconv.Itoa(ep.Episodes[0])
	}
	return ep.AirDate.Format(time.DateOnly)
}

// seasonOf is the season the path of a folder of a series names below the series' own folder,
// extras folders included.
func seasonOf(series, rel string) *int {
	name := naming.SeriesName(path.Base(series)).Title
	for _, p := range slices.Backward(below(series, rel)) {
		if n, ok := naming.ParseSeason(p, name); ok {
			return &n
		}
	}
	return nil
}

// below are the names of the folders on the way from series down to rel, rel's last.
func below(series, rel string) []string {
	if series == "" || rel == series {
		return nil
	}
	return strings.Split(strings.TrimPrefix(rel, series+"/"), "/")
}

// firstFolder is the folder of a shows library's series a folder is under: the one below the root
// it is in.
func firstFolder(rel string) string {
	if rel == "." {
		return ""
	}
	first, _, _ := strings.Cut(rel, "/")
	return first
}

// leastEpisodes is how many videos named as episodes of a season make a folder of a mixed library
// a show's with no season folder or tvshow.nfo: Emby takes a folder of one for a film's, whose
// name may be odd.
const leastEpisodes = 2

// isShow is whether a folder of a mixed library is a show's own, as Jellyfin and Emby tell one: a
// folder below the root holding a tvshow.nfo, a folder that names a season in words, or videos
// named as episodes of a season and no folder but a season's or extras'. A folder of shows with a
// show's loose episodes beside them is a folder of shows, not a show of seasons named for each.
func isShow(f library.Folder) bool {
	if f.Path == "." {
		return false
	}
	series := naming.SeriesName(path.Base(f.Path)).Title
	if slices.ContainsFunc(f.Folders, func(sub string) bool { return naming.SaysSeason(sub, series) }) {
		return true
	}
	episodes := 0
	for _, file := range f.Files {
		if strings.EqualFold(file.Name, "tvshow.nfo") {
			return true
		}
		if !naming.IsVideo(file.Name) {
			continue
		}
		if ep, ok := naming.ParseEpisode(stem(file.Name), series); ok && ep.Confidence == naming.ConfidenceHigh && ep.Season != nil {
			episodes++
		}
	}
	others := slices.ContainsFunc(f.Folders, func(sub string) bool {
		_, extras := naming.ExtraFolder(sub)
		_, season := naming.ParseSeason(sub, series)
		return !extras && !season
	})
	return episodes >= leastEpisodes && !others
}
