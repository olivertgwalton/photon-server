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
		want      domain.Metadata
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
			want: domain.Metadata{
				Title: "Alien", OriginalTitle: "Alien", SortTitle: "Alien 1",
				Overview: "In space, no one can hear you scream.", Tagline: "In space no one can hear you scream.",
				Certificate: "R", ReleaseDate: time.Date(1979, 5, 25, 0, 0, 0, 0, time.UTC), Year: 1979,
				Genres: []string{"Horror", "Science Fiction"}, Studios: []string{"20th Century Fox", "Brandywine"},
				IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0078748", domain.ProviderTMDB: "348"},
			},
		},
		{
			name: "series with a legacy id",
			nfo:  `<tvshow><title>The Wire</title><year>2002</year><id>79126</id></tvshow>`,
			want: domain.Metadata{Title: "The Wire", Year: 2002, IDs: map[domain.Provider]string{domain.ProviderTVDB: "79126"}},
		},
		{
			name: "multi-episode file takes its first episode",
			nfo: `<episodedetails><title>Part One</title><aired>2008-01-20</aired></episodedetails>
<episodedetails><title>Part Two</title></episodedetails>`,
			want: domain.Metadata{Title: "Part One", ReleaseDate: time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC), Year: 2008},
		},
		{
			name: "link appended after the document",
			nfo:  "<movie><title>Heat</title></movie>\nhttps://www.themoviedb.org/movie/949-heat",
			want: domain.Metadata{Title: "Heat", IDs: map[domain.Provider]string{domain.ProviderTMDB: "949"}},
		},
		{
			name: "links only",
			nfo:  "https://www.imdb.com/title/tt0113277/\n",
			want: domain.Metadata{IDs: map[domain.Provider]string{domain.ProviderIMDb: "tt0113277"}},
		},
		{
			name: "unescaped ampersand",
			nfo:  `<movie><title>Fast & Furious</title></movie>`,
			want: domain.Metadata{Title: "Fast & Furious"},
		},
		{
			name: "declared Latin-1",
			nfo:  "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><movie><title>Am\xe9lie</title></movie>",
			want: domain.Metadata{Title: "Amélie"},
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
