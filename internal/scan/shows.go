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
	name     naming.Name
	// byNumber is true when the episode was read from a canonical form, so its numbers may join it
	// to an episode already known; a bare number never does.
	byNumber bool
	versions []copyPlan
}

// showFolder splits a folder of a shows library into its series folder and the season its path
// names, if any. Files at the library root and anything under an extras folder belong to no show.
func showFolder(rel string) (series string, season *int, ok bool) {
	if rel == "." {
		return "", nil, false
	}
	parts := strings.Split(rel, "/")
	series = parts[0]
	seriesName := naming.SeriesName(series).Title
	for _, p := range parts[1:] {
		if _, extras := naming.ExtraFolder(p); extras {
			return "", nil, false
		}
	}
	for _, p := range slices.Backward(parts[1:]) {
		if n, ok := naming.ParseSeason(p, seriesName); ok {
			return series, &n, true
		}
	}
	return series, nil, true
}

// planEpisodes reads one folder of a series. A file's own season wins over its folder's, and an
// episode named by date with no season takes its air year, as Plex files daily shows. Copies of an
// episode are grouped only on a canonical key, never on a bare number, so two "- 01" files stay
// two episodes rather than silently merging (Jellyfin #18081).
func planEpisodes(f library.Folder, folderSeason *int, series string) (episodes []episodePlan, unread []string) {
	byKey := map[string]int{}
	for _, c := range stacks(f.Files) {
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
			episodes: ep.Episodes, airDate: ep.AirDate, name: n,
			byNumber: ep.Confidence == naming.ConfidenceHigh,
			versions: []copyPlan{{parts: c, edition: n.Edition}},
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
