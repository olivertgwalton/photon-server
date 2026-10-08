package nfo

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

func TestRead(t *testing.T) {
	for _, tc := range []struct {
		name, nfo string
		want      File
	}{
		{
			name: "Kodi film",
			nfo: `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<movie>
  <title>Alien</title>
  <originaltitle>Alien</originaltitle>
  <sorttitle>Alien 1</sorttitle>
  <outline>Short.</outline>
  <plot>In space, no one can hear you scream.</plot>
  <tagline>In space no one can hear you scream.</tagline>
  <mpaa>Rated R</mpaa>
  <premiered>1979-05-25</premiered>
  <genre>Horror / Science Fiction</genre>
  <studio>20th Century Fox</studio>
  <studio>Brandywine</studio>
  <uniqueid type="imdb">tt0078748</uniqueid>
  <uniqueid type="tmdb" default="true">348</uniqueid>
</movie>`,
			want: File{Metadata: domain.Metadata{
				Title: "Alien", OriginalTitle: "Alien", SortTitle: "Alien 1",
				Overview: "In space, no one can hear you scream.", Tagline: "In space no one can hear you scream.",
				Certificate: "R", ReleaseDate: time.Date(1979, 5, 25, 0, 0, 0, 0, time.UTC), Year: 1979,
				Genres: []string{"Horror", "Science Fiction"}, Studios: []string{"20th Century Fox", "Brandywine"},
				IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0078748", domain.ProviderTMDB: "348"},
			}},
		},
		{
			name: "series known by its bare id",
			nfo:  `<tvshow><title>The Wire</title><year>2002</year><id>79126</id></tvshow>`,
			want: File{Metadata: domain.Metadata{Title: "The Wire", Year: 2002, IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}}},
		},
		{
			name: "multi-episode file is its lowest episode, numbered to the highest",
			nfo: `<episodedetails><title>Part Two</title><season>1</season><episode>2</episode></episodedetails>
<episodedetails><title>Part One</title><season>1</season><episode>1</episode><aired>2008-01-20</aired></episodedetails>`,
			want: File{
				Metadata: domain.Metadata{Title: "Part One / Part Two", ReleaseDate: time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC), Year: 2008},
				Season:   new(1), Episodes: []int{1, 2},
			},
		},
		{
			name: "episode with numbers Kodi does not know",
			nfo:  `<episodedetails><title>Pilot</title><season>-1</season><episode>-1</episode></episodedetails>`,
			want: File{Metadata: domain.Metadata{Title: "Pilot"}},
		},
		{
			name: "season",
			nfo:  `<season><title>The Barksdale Organisation</title><seasonnumber>1</seasonnumber></season>`,
			want: File{Metadata: domain.Metadata{Title: "The Barksdale Organisation"}, Season: new(1)},
		},
		{
			name: "series naming its seasons, locked whole",
			nfo: `<tvshow><title>The Wire</title><namedseason number="1">The Street</namedseason>
<namedseason number="2">The Port</namedseason><lockdata>true</lockdata></tvshow>`,
			want: File{
				Metadata:    domain.Metadata{Title: "The Wire", Locked: domain.Fields()},
				SeasonNames: map[int]string{1: "The Street", 2: "The Port"},
			},
		},
		{
			name: "fields locked by Jellyfin's names",
			nfo:  `<movie><title>Heat</title><lockedfields>Overview|OfficialRating|Cast</lockedfields></movie>`,
			want: File{Metadata: domain.Metadata{Title: "Heat", Locked: []domain.Field{
				domain.FieldOverview, domain.FieldTagline, domain.FieldCertificate,
			}}},
		},
		{
			name: "link appended after the document",
			nfo:  "<movie><title>Heat</title></movie>\nhttps://www.themoviedb.org/movie/949-heat",
			want: File{Metadata: domain.Metadata{Title: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}}},
		},
		{
			name: "links only",
			nfo:  "https://www.imdb.com/title/tt0113277/\n",
			want: File{Metadata: domain.Metadata{IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}}},
		},
		{
			name: "unescaped ampersand",
			nfo:  `<movie><title>Fast & Furious</title></movie>`,
			want: File{Metadata: domain.Metadata{Title: "Fast & Furious"}},
		},
		{
			name: "declared Latin-1",
			nfo:  "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><movie><title>Am\xe9lie</title></movie>",
			want: File{Metadata: domain.Metadata{Title: "Amélie"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Read(strings.NewReader(tc.nfo))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Read (-want +got):\n%s", diff)
			}
		})
	}
}
