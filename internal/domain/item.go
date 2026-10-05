package domain

type ItemKind string

const (
	ItemMovie   ItemKind = "movie"
	ItemShow    ItemKind = "show"
	ItemSeason  ItemKind = "season"
	ItemEpisode ItemKind = "episode"
	// ItemExtra is a trailer, featurette or the like, belonging to its parent title.
	ItemExtra ItemKind = "extra"
)

func ItemKinds() []ItemKind {
	return []ItemKind{ItemMovie, ItemShow, ItemSeason, ItemEpisode, ItemExtra}
}

// Provider is a metadata source whose ids a title can carry.
type Provider string

const (
	ProviderTMDB Provider = "tmdb"
	ProviderIMDb Provider = "imdb"
	ProviderTVDB Provider = "tvdb"
)

func Providers() []Provider {
	return []Provider{ProviderTMDB, ProviderIMDb, ProviderTVDB}
}

// IDSource is where a title's provider id came from, so a later source knows what it may replace.
type IDSource string

const IDFromPath IDSource = "path"

func IDSources() []IDSource {
	return []IDSource{IDFromPath}
}
