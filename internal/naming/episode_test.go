package naming

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestParseEpisode(t *testing.T) {
	tests := []struct {
		stem, series string
		want         Episode // Rule and Title are not compared here; Confidence is.
		notEpisode   bool
	}{
		{stem: "Running Man S2017E368", want: Episode{Season: new(2017), Episodes: []int{368}, Confidence: ConfidenceHigh}},
		{stem: "S003 E009", want: Episode{Season: new(3), Episodes: []int{9}, Confidence: ConfidenceHigh}},
		{stem: "The Series Season 3 Episode 9 - The title", want: Episode{Season: new(3), Episodes: []int{9}, Confidence: ConfidenceHigh}},
		{stem: "Episode 21 - 94 Meetings", want: Episode{Episodes: []int{21}, Confidence: ConfidenceHigh}},
		{stem: "The.Legend.of.Condor.Heroes.2017.E07.V2.web-dl.1080p.h264.aac-hdctv", want: Episode{Episodes: []int{7}, Confidence: ConfidenceHigh}},
		{stem: "The Daily Show 25x22 - [WEBDL-720p][AAC 2.0][x264] Noah Baumbach-TBS", want: Episode{Season: new(25), Episodes: []int{22}, Confidence: ConfidenceHigh}},
		{stem: "Series Special (1920x1080)", notEpisode: true},

		// Several episodes in one file.
		{stem: "Show S01E23-E24-E26", want: Episode{Season: new(1), Episodes: []int{23, 24, 25, 26}, Confidence: ConfidenceHigh}},
		{stem: "Show 02x03-04-15", want: Episode{Season: new(2), Episodes: []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, Confidence: ConfidenceHigh}},
		{stem: "MOONLIGHTING_s01e01-e04", want: Episode{Season: new(1), Episodes: []int{1, 2, 3, 4}, Confidence: ConfidenceHigh}},
		{stem: "buck.rogers.s01e01e02.alternate.cut", want: Episode{Season: new(1), Episodes: []int{1, 2}, Confidence: ConfidenceHigh}},
		{stem: "series-s09e14-1080p", want: Episode{Season: new(9), Episodes: []int{14}, Confidence: ConfidenceHigh}},
		{stem: "S01E01 The 6-10 to Lubbock", want: Episode{Season: new(1), Episodes: []int{1}, Confidence: ConfidenceHigh}},
		{stem: "S05E23 11-59 [HDTV-1080p]", want: Episode{Season: new(5), Episodes: []int{23}, Confidence: ConfidenceHigh}},
		{stem: "Star Trek Enterprise (2001) - S03E21 - E2 (1080p BluRay x265)", want: Episode{Season: new(3), Episodes: []int{21}, Confidence: ConfidenceHigh}},
		{stem: "02-04 - blah 14 blah", want: Episode{Episodes: []int{2, 3, 4}, Confidence: ConfidenceMedium}},

		// Air dates.
		{stem: "anything_1996.11.14", want: Episode{AirDate: day(1996, 11, 14), Confidence: ConfidenceHigh}},
		{stem: "anything_14.11.1996", want: Episode{AirDate: day(1996, 11, 14), Confidence: ConfidenceHigh}},
		{stem: "anything_11.14.1996", want: Episode{AirDate: day(1996, 11, 14), Confidence: ConfidenceHigh}},
		{stem: "ABC News 2018_03_24_19_00_00", want: Episode{AirDate: day(2018, 3, 24), Confidence: ConfidenceHigh}},
		{stem: "Jeopardy 2023 07 14 HDTV x264 AC3", want: Episode{AirDate: day(2023, 7, 14), Confidence: ConfidenceHigh}},
		{stem: "james.corden.2017.04.20.anne.hathaway.720p.hdtv.x264-crooks", want: Episode{AirDate: day(2017, 4, 20), Confidence: ConfidenceHigh}},
		{stem: "A Daily Show - (2015-01-15) - Episode Name", want: Episode{AirDate: day(2015, 1, 15), Confidence: ConfidenceHigh}},

		// Bare numbers: playable, never grouped as versions.
		{stem: "7 - 12 Angry Men", want: Episode{Episodes: []int{7}, Confidence: ConfidenceMedium}},
		{stem: "Show Name - 1234 [720p]", want: Episode{Episodes: []int{1234}, Confidence: ConfidenceMedium}},
		{stem: "[HorribleSubs] Log Horizon 2 - 03 [720p]", want: Episode{Episodes: []int{3}, Confidence: ConfidenceMedium}},
		{stem: "[BBT-RMX] Ranma ½ - 154 [50AC421A]", want: Episode{Episodes: []int{154}, Confidence: ConfidenceMedium}},
		{stem: "Case Closed - 317", want: Episode{Episodes: []int{317}, Confidence: ConfidenceMedium}},
		{stem: "[VCB-Studio] Re Zero kara Hajimeru Isekai Seikatsu [21][Ma10p_1080p][x265_flac]", want: Episode{Episodes: []int{21}, Confidence: ConfidenceMedium}},
		{stem: "[CASO&Sumisora][Oda_Nobuna_no_Yabou][04][BDRIP][1920x1080][H264_AAC]", want: Episode{Episodes: []int{4}, Confidence: ConfidenceMedium}},
		{stem: "One Piece 1001", series: "One Piece", want: Episode{Episodes: []int{1001}, Confidence: ConfidenceMedium}},
		{stem: "Seinfeld 0807 The Checks", series: "Seinfeld", want: Episode{Season: new(8), Episodes: []int{7}, Confidence: ConfidenceMedium}},
		{stem: "01 - Pilot", want: Episode{Episodes: []int{1}, Confidence: ConfidenceMedium}},
		{stem: "Show - 01 - Pilot", want: Episode{Episodes: []int{1}, Confidence: ConfidenceMedium}},
	}
	for _, tt := range tests {
		t.Run(tt.stem, func(t *testing.T) {
			got, ok := ParseEpisode(tt.stem, tt.series)
			if tt.notEpisode {
				if ok {
					t.Errorf("ParseEpisode(%q) = %+v, want no episode", tt.stem, got)
				}
				return
			}
			if !ok {
				t.Fatalf("ParseEpisode(%q): no episode", tt.stem)
			}
			got.Rule, got.Title = "", ""
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseEpisode(%q) (-want +got):\n%s", tt.stem, diff)
			}
		})
	}
}

func FuzzParseEpisode(f *testing.F) {
	for _, s := range []string{"Show S01E23-E24-E26", "02x03-04-15", "a - 1 - b", "Seinfeld 0807", "x 2018_03_24"} {
		f.Add(s, "Seinfeld")
	}
	f.Fuzz(func(t *testing.T, stem, series string) {
		ep, ok := ParseEpisode(stem, series)
		if !ok {
			return
		}
		for i := 1; i < len(ep.Episodes); i++ {
			if ep.Episodes[i] <= ep.Episodes[i-1] {
				t.Fatalf("ParseEpisode(%q).Episodes = %v, not ascending", stem, ep.Episodes)
			}
		}
	})
}

func TestSpecialsAreSeasonZero(t *testing.T) {
	ep, ok := ParseEpisode("Doctor Who S00E01 The Christmas Invasion", "")
	if !ok || ep.Season == nil || *ep.Season != 0 {
		t.Errorf("ParseEpisode(S00E01) season = %v, want 0", ep.Season)
	}
	ep, _ = ParseEpisode("01 - Pilot", "")
	if ep.Season != nil {
		t.Errorf("ParseEpisode(01 - Pilot) season = %d, want none", *ep.Season)
	}
}

func TestEpisodeTitles(t *testing.T) {
	for stem, want := range map[string]string{
		"Severance.S01E01.Good.News.About.Hell.2160p.WEB-DL": "Good News About Hell",
		"The Wire S02E01 - Ebb Tide":                         "Ebb Tide",
		"The Wire S02E01 - 720p":                             "",
		"The Wire S02E01":                                    "",
		"Show S01E23-E24-E26 - Finale":                       "Finale",
		"The Daily Show 25x22 - Noah Baumbach":               "Noah Baumbach",
		"james.corden.2017.04.20.anne.hathaway.720p.hdtv":    "anne hathaway",
		"01 - Pilot":                "Pilot",
		"01.Pilot.1080p":            "Pilot",
		"Show - 01 - Pilot [1080p]": "Pilot",
		"Marvel's Agents of S.H.I.E.L.D. S01E01 - Pilot": "Pilot",
		"Show - 101 [720p]": "",
	} {
		ep, ok := ParseEpisode(stem, "")
		if !ok {
			t.Errorf("%q: no episode", stem)
			continue
		}
		if ep.Title != want {
			t.Errorf("%q: title %q, want %q", stem, ep.Title, want)
		}
	}
}
