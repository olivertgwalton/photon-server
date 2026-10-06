package scan

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/library"
)

type plannedCopy struct {
	Parts          []string
	Edition, Label string
}

type plannedFilm struct {
	Title    string
	Year     int
	Versions []plannedCopy
}

func flatten(films []film) []plannedFilm {
	var out []plannedFilm
	for _, f := range films {
		pf := plannedFilm{Title: f.name.Title, Year: f.name.Year}
		for _, v := range f.versions {
			pc := plannedCopy{Edition: v.edition, Label: v.label}
			for _, p := range v.parts {
				pc.Parts = append(pc.Parts, p.Name)
			}
			pf.Versions = append(pf.Versions, pc)
		}
		out = append(out, pf)
	}
	return out
}

func folder(path string, names ...string) library.Folder {
	f := library.Folder{Path: path}
	for _, n := range names {
		f.Files = append(f.Files, library.File{Name: n})
	}
	return f
}

func TestPlanFilms(t *testing.T) {
	tests := []struct {
		name   string
		folder library.Folder
		want   []plannedFilm
	}{
		{
			name:   "a film named by its folder, whatever the file is called",
			folder: folder("Heat (1995)", "heat.1995.1080p.bluray.mkv", "heat.1995.1080p.bluray.en.srt", "poster.jpg"),
			want:   []plannedFilm{{Title: "Heat", Year: 1995, Versions: []plannedCopy{{Parts: []string{"heat.1995.1080p.bluray.mkv"}}}}},
		},
		{
			name: "copies named after the folder are versions of one film",
			folder: folder("Blade Runner (1982)",
				"Blade Runner (1982) - 2160p.mkv",
				"Blade Runner (1982) - [1080p].mkv",
				"Blade Runner (1982) {edition-Final Cut}.mkv"),
			want: []plannedFilm{{Title: "Blade Runner", Year: 1982, Versions: []plannedCopy{
				{Parts: []string{"Blade Runner (1982) - 2160p.mkv"}, Label: "2160p"},
				{Parts: []string{"Blade Runner (1982) - [1080p].mkv"}, Label: "1080p"},
				{Parts: []string{"Blade Runner (1982) {edition-Final Cut}.mkv"}, Edition: "Final Cut"},
			}}},
		},
		{
			name:   "a folder's id tag is not part of the name its copies start with",
			folder: folder("Heat (1995) [tmdbid-949]", "Heat (1995) - 2160p.mkv", "Heat (1995) - 1080p.mkv"),
			want: []plannedFilm{{Title: "Heat", Year: 1995, Versions: []plannedCopy{
				{Parts: []string{"Heat (1995) - 2160p.mkv"}, Label: "2160p"},
				{Parts: []string{"Heat (1995) - 1080p.mkv"}, Label: "1080p"},
			}}},
		},
		{
			name:   "parts of one copy stack in order",
			folder: folder("Lawrence of Arabia (1962)", "Lawrence of Arabia (1962) cd2.mkv", "Lawrence of Arabia (1962) cd1.mkv"),
			want: []plannedFilm{{Title: "Lawrence of Arabia", Year: 1962, Versions: []plannedCopy{
				{Parts: []string{"Lawrence of Arabia (1962) cd1.mkv", "Lawrence of Arabia (1962) cd2.mkv"}},
			}}},
		},
		{
			name:   "files not named after their folder are films of their own",
			folder: folder("Collection", "Alien (1979).mkv", "Aliens (1986).mkv"),
			want: []plannedFilm{
				{Title: "Alien", Year: 1979, Versions: []plannedCopy{{Parts: []string{"Alien (1979).mkv"}}}},
				{Title: "Aliens", Year: 1986, Versions: []plannedCopy{{Parts: []string{"Aliens (1986).mkv"}}}},
			},
		},
		{
			name:   "a longer title that starts with the folder's is a different film",
			folder: folder("Alien", "Alien.mkv", "Aliens.mkv"),
			want: []plannedFilm{
				{Title: "Alien", Versions: []plannedCopy{{Parts: []string{"Alien.mkv"}}}},
				{Title: "Aliens", Versions: []plannedCopy{{Parts: []string{"Aliens.mkv"}}}},
			},
		},
		{
			name:   "films of different years named alike are films of their own",
			folder: folder("Halloween", "Halloween (1978).mkv", "Halloween (2018).mkv"),
			want: []plannedFilm{
				{Title: "Halloween", Year: 1978, Versions: []plannedCopy{{Parts: []string{"Halloween (1978).mkv"}}}},
				{Title: "Halloween", Year: 2018, Versions: []plannedCopy{{Parts: []string{"Halloween (2018).mkv"}}}},
			},
		},
		{
			name:   "release names of different years are films of their own",
			folder: folder("Dune", "Dune.1984.1080p.BluRay.x264.mkv", "Dune.2021.2160p.WEB-DL.DDP5.1.Atmos.mkv"),
			want: []plannedFilm{
				{Title: "Dune", Year: 1984, Versions: []plannedCopy{{Parts: []string{"Dune.1984.1080p.BluRay.x264.mkv"}}}},
				{Title: "Dune", Year: 2021, Versions: []plannedCopy{{Parts: []string{"Dune.2021.2160p.WEB-DL.DDP5.1.Atmos.mkv"}}}},
			},
		},
		{
			name:   "every file at the library root is a film of its own",
			folder: folder(".", "Heat (1995).mkv"),
			want:   []plannedFilm{{Title: "Heat", Year: 1995, Versions: []plannedCopy{{Parts: []string{"Heat (1995).mkv"}}}}},
		},
		{
			name:   "trailers and samples are not copies",
			folder: folder("Heat (1995)", "Heat (1995).mkv", "Heat (1995)-trailer.mkv", "sample.mkv"),
			want:   []plannedFilm{{Title: "Heat", Year: 1995, Versions: []plannedCopy{{Parts: []string{"Heat (1995).mkv"}}}}},
		},
		{
			name:   "an extras folder holds no films",
			folder: folder("Heat (1995)/Featurettes", "Making Of.mkv"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, flatten(planFilms(tt.folder))); diff != "" {
				t.Errorf("planFilms (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSubtitlesFor(t *testing.T) {
	tests := []struct {
		name   string
		folder library.Folder
		want   map[string][]string // version label → subtitle file: language/forced
	}{
		{
			name: "named after the copy, after the title, and in a Subs folder",
			folder: folder("Heat (1995)", "Heat (1995).mkv", "Heat (1995).en.srt", "Heat (1995).fr.forced.ass",
				"Subs/English.srt", "Heat 1995.de.srt", "Heat (1995).sub", "Heat (1995).idx"),
			want: map[string][]string{"": {
				"Heat (1995).en.srt:en", "Heat (1995).fr.forced.ass:fr forced",
				"Subs/English.srt:en", "Heat (1995).idx:und",
			}},
		},
		{
			name: "a title's subtitle is every version's; a copy's only its own",
			folder: folder("Heat (1995)", "Heat (1995) - 2160p.mkv", "Heat (1995) - 1080p.mkv",
				"Heat (1995).en.srt", "Heat (1995) - 2160p.de.srt", "Subs/English.srt"),
			want: map[string][]string{
				"2160p": {"Heat (1995).en.srt:en", "Heat (1995) - 2160p.de.srt:de"},
				"1080p": {"Heat (1995).en.srt:en"},
			},
		},
		{
			name:   "stacked parts share the subtitle named after what they share",
			folder: folder("Lawrence (1962)", "Lawrence (1962) cd1.mkv", "Lawrence (1962) cd2.mkv", "Lawrence (1962).en.srt"),
			want:   map[string][]string{"": {"Lawrence (1962).en.srt:en"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string][]string{}
			for _, f := range planFilms(tt.folder) {
				for _, v := range f.versions {
					got[v.label] = []string{}
					for _, s := range v.subtitles {
						desc := s.file.Name + ":" + s.tags.Language.String()
						if s.tags.Forced {
							desc += " forced"
						}
						got[v.label] = append(got[v.label], desc)
					}
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("subtitles (-want +got):\n%s", diff)
			}
		})
	}
}
