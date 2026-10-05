package domain

// RatingSite is who a rating is of: the site whose readers or critics gave it.
type RatingSite string

const (
	SiteIMDb RatingSite = "imdb"
	SiteTMDB RatingSite = "tmdb"
	// SiteRottenTomatoes is the Tomatometer: the share of critics who liked it.
	SiteRottenTomatoes RatingSite = "rotten_tomatoes"
	// SiteRottenTomatoesAudience is the Popcornmeter: the share of the audience who did.
	SiteRottenTomatoesAudience RatingSite = "rotten_tomatoes_audience"
	SiteMetacritic             RatingSite = "metacritic"
	SiteLetterboxd             RatingSite = "letterboxd"
	SiteTrakt                  RatingSite = "trakt"
)

func RatingSites() []RatingSite {
	return []RatingSite{
		SiteIMDb, SiteTMDB, SiteRottenTomatoes, SiteRottenTomatoesAudience, SiteMetacritic, SiteLetterboxd, SiteTrakt,
	}
}

// Rating is what a site's readers or critics make of a title, scored out of 100 whatever scale the
// site uses (IMDb's 8.1 is 81), and by how many.
type Rating struct {
	Site  RatingSite
	Score float64
	Votes int
}

// RatingSources are the sources that give ratings.
func RatingSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB, SourceTVDB, SourceMDBList}
}
