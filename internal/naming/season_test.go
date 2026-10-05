package naming

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseSeason(t *testing.T) {
	tests := []struct {
		folder, series string
		want           int
		ok             bool
	}{
		{folder: "Season 1", want: 1, ok: true},
		{folder: "Season1", want: 1, ok: true},
		{folder: "Season 7 (2016)", want: 7, ok: true},
		{folder: "S01", want: 1, ok: true},
		{folder: "The.Wonder.Years.S04.PDTV.x264-JCH", want: 4, ok: true},
		{folder: "Drive.S01.2160p.WEB-DL.DDP5.1.H.265-XXXX", want: 1, ok: true},
		{folder: "3.Staffel", want: 3, ok: true},
		{folder: "Staffel 3", want: 3, ok: true},
		{folder: "1st season", want: 1, ok: true},
		{folder: "Saison 2", want: 2, ok: true},
		{folder: "Temporada 4", want: 4, ok: true},
		{folder: "시즌 2", want: 2, ok: true},
		{folder: "2", want: 2, ok: true},
		{folder: "Specials", want: 0, ok: true},
		{folder: "Seinfeld Season 2", series: "Seinfeld", want: 2, ok: true},
		{folder: "Seinfeld Season 2", series: "Friends"},
		{folder: "Season (8)"},
		{folder: "s06e05"},
		{folder: "Episode 1 Season 2"},
		{folder: "Extras"},
		{folder: "1920"},
	}
	for _, tt := range tests {
		t.Run(tt.folder, func(t *testing.T) {
			got, ok := ParseSeason(tt.folder, tt.series)
			if got != tt.want || ok != tt.ok {
				t.Errorf("ParseSeason(%q, %q) = %d, %v; want %d, %v", tt.folder, tt.series, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSeriesName(t *testing.T) {
	tests := []struct {
		folder string
		want   Name
	}{
		{"The.Show.S01", Name{Title: "The Show"}},
		{"The.Show.P.I.S01", Name{Title: "The Show P.I"}},
		{"Bunker.S03.1080p.PULSAR.WEB-DL.DDP5.1.Atmos.H.264-showWEB", Name{Title: "Bunker"}},
		{"Outer.Colony.S01.1080p.NOVA.WEB-DL.DDP5.1.H.264.HUN.ENG-QUASAR", Name{Title: "Outer Colony"}},
		{"Marvel's.Agents.of.S.H.I.E.L.D.", Name{Title: "Marvel's Agents of S.H.I.E.L.D."}},
		{"1923 (2022)", Name{Title: "1923", Year: 2022}},
		{"Lost (2004) [tvdbid-73739]", Name{Title: "Lost", Year: 2004, IDs: IDs{TVDB: "73739"}}},
	}
	for _, tt := range tests {
		t.Run(tt.folder, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, SeriesName(tt.folder)); diff != "" {
				t.Errorf("SeriesName(%q) (-want +got):\n%s", tt.folder, diff)
			}
		})
	}
}
