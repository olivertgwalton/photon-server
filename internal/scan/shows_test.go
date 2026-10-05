package scan

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestShowFolder(t *testing.T) {
	tests := []struct {
		rel, series string
		season      int // -1: the path names none
		ok          bool
	}{
		{rel: ".", ok: false},
		{rel: "The Wire (2002)", series: "The Wire (2002)", season: -1, ok: true},
		{rel: "The Wire (2002)/Season 2", series: "The Wire (2002)", season: 2, ok: true},
		{rel: "The Wire (2002)/Specials", series: "The Wire (2002)", season: 0, ok: true},
		{rel: "The Wire (2002)/Season 2/Disc 1", series: "The Wire (2002)", season: 2, ok: true},
		{rel: "The Wire (2002)/Extras", ok: false},
		{rel: "The Wire (2002)/Season 1/Featurettes", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			series, season, ok := showFolder(tt.rel)
			got := -1
			if season != nil {
				got = *season
			}
			if ok != tt.ok || (ok && (series != tt.series || got != tt.season)) {
				t.Errorf("showFolder(%q) = %q, %d, %v; want %q, %d, %v", tt.rel, series, got, ok, tt.series, tt.season, tt.ok)
			}
		})
	}
}

type plannedEpisode struct {
	Season   int
	Episodes []int
	ByNumber bool
	Copies   [][]string
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
			name:   "canonical names, with two copies of one episode grouped",
			folder: []string{"The Wire S02E01 720p.mkv", "The Wire S02E01 1080p.mkv", "The Wire S02E02.mkv", "notes.txt"},
			season: &two,
			want: []plannedEpisode{
				{Season: 2, Episodes: []int{1}, ByNumber: true, Copies: [][]string{{"The Wire S02E01 720p.mkv"}, {"The Wire S02E01 1080p.mkv"}}},
				{Season: 2, Episodes: []int{2}, ByNumber: true, Copies: [][]string{{"The Wire S02E02.mkv"}}},
			},
		},
		{
			name:   "a file's season beats its folder's",
			folder: []string{"The Wire S01E05.mkv"},
			season: &two,
			want:   []plannedEpisode{{Season: 1, Episodes: []int{5}, ByNumber: true, Copies: [][]string{{"The Wire S01E05.mkv"}}}},
		},
		{
			name:   "a two-episode file is not a copy of the first",
			folder: []string{"Buck Rogers S01E01.mkv", "Buck Rogers S01E01E02.mkv"},
			want: []plannedEpisode{
				{Season: 1, Episodes: []int{1}, ByNumber: true, Copies: [][]string{{"Buck Rogers S01E01.mkv"}}},
				{Season: 1, Episodes: []int{1, 2}, ByNumber: true, Copies: [][]string{{"Buck Rogers S01E01E02.mkv"}}},
			},
		},
		{
			name:   "bare numbers are never grouped",
			folder: []string{"Show - 01 [720p].mkv", "Show - 01 [1080p].mkv"},
			want: []plannedEpisode{
				{Season: 1, Episodes: []int{1}, Copies: [][]string{{"Show - 01 [720p].mkv"}}},
				{Season: 1, Episodes: []int{1}, Copies: [][]string{{"Show - 01 [1080p].mkv"}}},
			},
		},
		{
			name:   "a dated episode with no season takes its year",
			folder: []string{"The Daily Show 2018-03-24.mkv"},
			want:   []plannedEpisode{{Season: 2018, ByNumber: true, Copies: [][]string{{"The Daily Show 2018-03-24.mkv"}}}},
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
				pe := plannedEpisode{Season: e.season, Episodes: e.episodes, ByNumber: e.byNumber}
				for _, v := range e.versions {
					var names []string
					for _, p := range v.parts {
						names = append(names, p.Name)
					}
					pe.Copies = append(pe.Copies, names)
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
