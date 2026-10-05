package domain

type ItemKind string

const (
	ItemMovie   ItemKind = "movie"
	ItemShow    ItemKind = "show"
	ItemSeason  ItemKind = "season"
	ItemEpisode ItemKind = "episode"
	// ItemExtra is a trailer, featurette or the like, belonging to its parent title.
	ItemExtra ItemKind = "extra"
	// ItemCollection is a box set: titles grouped by a provider, or by an admin.
	ItemCollection ItemKind = "collection"
)

func ItemKinds() []ItemKind {
	return []ItemKind{ItemMovie, ItemShow, ItemSeason, ItemEpisode, ItemExtra, ItemCollection}
}

// CollectionOrigin is who made a collection: a provider that names its titles part of it, or an
// admin.
type CollectionOrigin string

const (
	CollectionTMDB CollectionOrigin = "tmdb"
	CollectionUser CollectionOrigin = "user"
)

func CollectionOrigins() []CollectionOrigin {
	return []CollectionOrigin{CollectionTMDB, CollectionUser}
}

// Grouping is a collection a provider names a title part of: its id there, its name and pictures.
type Grouping struct {
	ID      string
	Title   string
	Artwork []Artwork
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

const (
	// IDFromMatch is an id the server found by matching a title to a provider.
	IDFromMatch IDSource = "match"
	IDFromNFO   IDSource = "nfo"
	IDFromPath  IDSource = "path"
	// IDFromUser is an id an admin pinned the title to, fixing a match.
	IDFromUser IDSource = "user"
)

func IDSources() []IDSource {
	return []IDSource{IDFromMatch, IDFromNFO, IDFromPath, IDFromUser}
}

// Rank puts an id an admin pinned above all, then one the reader typed into a folder or file name
// above one an NFO carries, since NFOs are mostly written by other tools, and all above a match.
func (s IDSource) Rank() int {
	switch s {
	case IDFromMatch:
		return 1
	case IDFromNFO:
		return 2
	case IDFromPath:
		return 3
	case IDFromUser:
		return 4
	}
	return 0
}

// EpisodeOrder is the order a show's episode files are numbered in.
type EpisodeOrder string

const (
	OrderAired EpisodeOrder = "aired"
	// OrderDVD is as the discs number them.
	OrderDVD EpisodeOrder = "dvd"
	// OrderAbsolute counts every episode from the first, as anime often is.
	OrderAbsolute EpisodeOrder = "absolute"
)

func EpisodeOrders() []EpisodeOrder {
	return []EpisodeOrder{OrderAired, OrderDVD, OrderAbsolute}
}

// SeasonRequest is which of a show's seasons a provider is asked about, in the order its files are
// numbered in.
type SeasonRequest struct {
	Numbers []int
	Order   EpisodeOrder
}
