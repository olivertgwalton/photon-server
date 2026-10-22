package scan

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/library"
)

func TestSeriesAndSeasonOfAFolder(t *testing.T) {
	tests := []struct {
		rel, series string
		season      int // -1: the path names none
		episodes    bool
	}{
		{rel: ".", season: -1},
		{rel: "The Wire (2002)", series: "The Wire (2002)", season: -1, episodes: true},
		{rel: "The Wire (2002)/Season 2", series: "The Wire (2002)", season: 2, episodes: true},
		{rel: "The Wire (2002)/Specials", series: "The Wire (2002)", season: 0, episodes: true},
		{rel: "The Wire (2002)/Season 2/Disc 1", series: "The Wire (2002)", season: 2, episodes: true},
		{rel: "The Wire (2002)/Extras", series: "The Wire (2002)", season: -1},
		{rel: "The Wire (2002)/Season 1/Featurettes", series: "The Wire (2002)", season: 1},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			series := firstFolder(tt.rel)
			got := -1
			if season := seasonOf(series, tt.rel); season != nil {
				got = *season
			}
			if series != tt.series || got != tt.season || holdsEpisodes(series, tt.rel) != tt.episodes {
				t.Errorf("%q: series %q, season %d, holds episodes %v; want %q, %d, %v",
					tt.rel, series, got, holdsEpisodes(series, tt.rel), tt.series, tt.season, tt.episodes)
			}
		})
	}
}

func TestAMixedLibrarysShowIsAFolderThatSaysSo(t *testing.T) {
	tests := []struct {
		name   string
		folder library.Folder
		isShow bool
	}{
		{"a season folder", library.Folder{Path: "TV/The Wire (2002)", Folders: []string{"Season 1", "Extras"}}, true},
		{"a season folder named with its show", library.Folder{Path: "Seinfeld", Folders: []string{"Seinfeld Season 2"}}, true},
		{"a tvshow.nfo", library.Folder{Path: "Lost", Files: []library.File{{Name: "tvshow.nfo"}}}, true},
		{"episodes", library.Folder{Path: "Fleabag", Files: []library.File{{Name: "Fleabag S01E01.mkv"}, {Name: "Fleabag 1x02.mkv"}}}, true},
		{"episodes beside extras", library.Folder{Path: "Fleabag", Folders: []string{"Extras", "S2"}, Files: []library.File{{Name: "Fleabag S01E01.mkv"}, {Name: "Fleabag S01E02.mkv"}}}, true},
		{"episodes beside shows", library.Folder{Path: "TV", Folders: []string{"The Wire (2002)", "Lost"}, Files: []library.File{{Name: "Fleabag S01E01.mkv"}, {Name: "Fleabag S01E02.mkv"}}}, false},
		{"one episode", library.Folder{Path: "Fleabag", Files: []library.File{{Name: "Fleabag S01E01.mkv"}}}, false},
		{"numbered films", library.Folder{Path: "Saw", Files: []library.File{{Name: "Saw 2.mkv"}, {Name: "Saw 3.mkv"}}}, false},
		{"a bare number", library.Folder{Path: "1917 (2019)", Folders: []string{"1"}}, false},
		{"a film", library.Folder{Path: "Heat (1995)", Files: []library.File{{Name: "Heat (1995).mkv"}}}, false},
		{"the root", library.Folder{Path: ".", Files: []library.File{{Name: "tvshow.nfo"}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isShow(tt.folder); got != tt.isShow {
				t.Errorf("isShow(%+v) = %v, want %v", tt.folder, got, tt.isShow)
			}
		})
	}
}

type plannedEpisode struct {
	Title    string
	Season   int
	Episodes []int
	ByNumber bool
	Versions [][]string
}

func TestPlanEpisodes(t *testing.T) {
	two := 2
	tests := []struct {
		name   string
		folder []string
		season *int
		want   []plannedEpisode
		unread []string
	}{
		{
			name:   "canonical names, with two versions of one episode grouped",
			folder: []string{"The Wire S02E01 720p.mkv", "The Wire S02E01 1080p.mkv", "The Wire S02E02.mkv", "notes.txt"},
			season: &two,
			want: []plannedEpisode{
				{Title: "Episode 1", Season: 2, Episodes: []int{1}, ByNumber: true, Versions: [][]string{{"The Wire S02E01 720p.mkv"}, {"The Wire S02E01 1080p.mkv"}}},
				{Title: "Episode 2", Season: 2, Episodes: []int{2}, ByNumber: true, Versions: [][]string{{"The Wire S02E02.mkv"}}},
			},
		},
		{
			name:   "a file's season beats its folder's",
			folder: []string{"The Wire S01E05.mkv"},
			season: &two,
			want:   []plannedEpisode{{Title: "Episode 5", Season: 1, Episodes: []int{5}, ByNumber: true, Versions: [][]string{{"The Wire S01E05.mkv"}}}},
		},
		{
			name:   "a two-episode file is not a version of the first",
			folder: []string{"Buck Rogers S01E01.mkv", "Buck Rogers S01E01E02.mkv"},
			want: []plannedEpisode{
				{Title: "Episode 1", Season: 1, Episodes: []int{1}, ByNumber: true, Versions: [][]string{{"Buck Rogers S01E01.mkv"}}},
				{Title: "Episode 1", Season: 1, Episodes: []int{1, 2}, ByNumber: true, Versions: [][]string{{"Buck Rogers S01E01E02.mkv"}}},
			},
		},
		{
			name:   "bare numbers are never grouped",
			folder: []string{"Show - 01 [720p].mkv", "Show - 01 [1080p].mkv"},
			want: []plannedEpisode{
				{Title: "Episode 1", Season: 1, Episodes: []int{1}, Versions: [][]string{{"Show - 01 [720p].mkv"}}},
				{Title: "Episode 1", Season: 1, Episodes: []int{1}, Versions: [][]string{{"Show - 01 [1080p].mkv"}}},
			},
		},
		{
			name:   "a dated episode with no season takes its year",
			folder: []string{"The Daily Show 2018-03-24.mkv"},
			want:   []plannedEpisode{{Title: "2018-03-24", Season: 2018, ByNumber: true, Versions: [][]string{{"The Daily Show 2018-03-24.mkv"}}}},
		},
		{
			name:   "a video with no episode in its name is left out",
			folder: []string{"Behind the Music.mkv"},
			unread: []string{"Show/Behind the Music.mkv"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eps, unread := planEpisodes(folder("Show", tt.folder...), tt.season, "Show")
			var got []plannedEpisode
			for _, e := range eps {
				pe := plannedEpisode{Title: e.title, Season: e.season, Episodes: e.episodes, ByNumber: e.byNumber}
				for _, v := range e.versions {
					var names []string
					for _, p := range v.parts {
						names = append(names, p.Name)
					}
					pe.Versions = append(pe.Versions, names)
				}
				got = append(got, pe)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("episodes (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.unread, unread); diff != "" {
				t.Errorf("unread (-want +got):\n%s", diff)
			}
		})
	}
}
