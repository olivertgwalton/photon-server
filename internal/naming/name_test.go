package naming

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCleanName(t *testing.T) {
	tests := []struct {
		stem string
		want Name
	}{
		// Jellyfin's expectations.
		{"The Wolf of Wall Street 2001 (2013)", Name{Title: "The Wolf of Wall Street 2001", Year: 2013}},
		{"300 (2006)", Name{Title: "300", Year: 2006}},
		{"300 2001 (2006)", Name{Title: "300 2001", Year: 2006}},
		{"Arrival.2016.2160p.Blu-Ray.HEVC", Name{Title: "Arrival", Year: 2016}},
		{"3.Days.to.Kill.2014.720p.BluRay.x264.YIFY", Name{Title: "3 Days to Kill", Year: 2014}},
		{"curse.of.chucky.2013.stv.unrated.multi.1080p.bluray.x264-rough", Name{Title: "curse of chucky", Year: 2013}},
		{"My Movie (1997) - GreatestReleaseGroup 2019", Name{Title: "My Movie", Year: 1997}},
		{"Rain Man 1988 REMASTERED 1080p BluRay x264 AAC - Ozlem", Name{Title: "Rain Man", Year: 1988}},
		{"My Movie 2013-12-09", Name{Title: "My Movie 2013-12-09"}},
		{"My Movie 2013-12-09 2013", Name{Title: "My Movie 2013-12-09", Year: 2013}},
		{"St. Vincent (2014)", Name{Title: "St. Vincent", Year: 2014}},
		{"[rec]", Name{Title: "[rec]"}},
		{"Run lola run (lola rennt) (2009)", Name{Title: "Run lola run (lola rennt)", Year: 2009}},
		{"[HorribleSubs] Made in Abyss - 13 [720p]", Name{Title: "Made in Abyss"}},
		{"Crouching.Tiger.Hidden.Dragon.4K.UltraHD.HDR.BDrip-HDC", Name{Title: "Crouching Tiger Hidden Dragon"}},
		{"Last.Call.for.Nowhere.WEB-DL.1080p", Name{Title: "Last Call for Nowhere"}},
		{"Super movie Multi", Name{Title: "Super movie"}},
		{"480 Super movie [tmdbid=12345]", Name{Title: "480 Super movie", IDs: IDs{TMDB: "12345"}}},
		{"Marvel's.Agents.of.S.H.I.E.L.D.", Name{Title: "Marvel's Agents of S.H.I.E.L.D."}},

		// Titles that are, or contain, numbers and years.
		{"1917 (2019)", Name{Title: "1917", Year: 2019}},
		{"1917", Name{Title: "1917"}},
		{"2012", Name{Title: "2012"}},
		{"2001 A Space Odyssey (1968)", Name{Title: "2001 A Space Odyssey", Year: 1968}},
		{"Blade Runner 2049 (2017)", Name{Title: "Blade Runner 2049", Year: 2017}},
		{"2 Fast 2 Furious (2003)", Name{Title: "2 Fast 2 Furious", Year: 2003}},
		{"8½ (1963)", Name{Title: "8½", Year: 1963}},
		{"9-1-1", Name{Title: "9-1-1"}},

		// A real word that is also a release tag survives when the year ends the title.
		{"The Limited (2016) 1080p", Name{Title: "The Limited", Year: 2016}},
		{"Robin Hood [Multi-Subs] [2018]", Name{Title: "Robin Hood [Multi-Subs]", Year: 2018}},

		// Provider ids and editions, in Jellyfin's and Plex's forms.
		{"The Matrix (1999) [imdbid-tt0133093]", Name{Title: "The Matrix", Year: 1999, IDs: IDs{IMDb: "tt0133093"}}},
		{"The Matrix (1999) {tmdb-603}", Name{Title: "The Matrix", Year: 1999, IDs: IDs{TMDB: "603"}}},
		{"Lost (2004) [tvdbid-73739]", Name{Title: "Lost", Year: 2004, IDs: IDs{TVDB: "73739"}}},
		{"Blade Runner (1982) {edition-Final Cut} [tmdb=78]", Name{Title: "Blade Runner", Year: 1982, IDs: IDs{TMDB: "78"}, Edition: "Final Cut"}},
	}
	for _, tt := range tests {
		t.Run(tt.stem, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, CleanName(tt.stem)); diff != "" {
				t.Errorf("CleanName(%q) (-want +got):\n%s", tt.stem, diff)
			}
		})
	}
}

func FuzzCleanName(f *testing.F) {
	for _, s := range []string{"The Matrix (1999) {tmdb-603}", "[a] b - 1 [720p]", "S.H.I.E.L.D.", "((", "{edition-}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, stem string) {
		n := CleanName(stem)
		if n.Year != 0 && (n.Year < 1900 || n.Year > 2099) {
			t.Errorf("CleanName(%q).Year = %d", stem, n.Year)
		}
	})
}
