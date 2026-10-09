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

// SearchKind is what a search can be narrowed to: a kind of title it finds, or people.
type SearchKind string

const (
	SearchMovie                 = SearchKind(ItemMovie)
	SearchShow                  = SearchKind(ItemShow)
	SearchCollection            = SearchKind(ItemCollection)
	SearchEpisode               = SearchKind(ItemEpisode)
	SearchPerson     SearchKind = "person"
)

func SearchKinds() []SearchKind {
	return []SearchKind{SearchMovie, SearchShow, SearchCollection, SearchEpisode, SearchPerson}
}

// CollectionOrigin is who made a collection: a provider that names its titles part of it, or an
// admin, by hand, by a rule its titles are found by (a smart collection, as Plex's), or by a list
// kept on a provider (as Kometa's list builders).
type CollectionOrigin string

const (
	CollectionTMDB  CollectionOrigin = "tmdb"
	CollectionUser  CollectionOrigin = "user"
	CollectionSmart CollectionOrigin = "smart"
	CollectionList  CollectionOrigin = "list"
)

func CollectionOrigins() []CollectionOrigin {
	return []CollectionOrigin{CollectionTMDB, CollectionUser, CollectionSmart, CollectionList}
}

// CollectionPlacement is where a collection is shown: in its library only, or on the home page of
// each profile that sees it too, as Plex's promoted collections.
type CollectionPlacement string

const (
	PlacementLibrary CollectionPlacement = "library"
	PlacementHome    CollectionPlacement = "home"
)

func CollectionPlacements() []CollectionPlacement {
	return []CollectionPlacement{PlacementLibrary, PlacementHome}
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

// Listed is a title a list kept on a provider holds: a film or a show, by its ids.
type Listed struct {
	Kind ItemKind
	IDs  map[Provider]string
	// Title and Year are what the list calls it, where it says: a remote library's name for a
	// title it adds, until the title is matched.
	Title string
	Year  int
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

// RefreshMode is how much of a title an admin's refresh asks its providers about again. Either
// way what a reader edited or locked, and what an NFO says, stands, and each provider's pictures
// are replaced with what it has now.
type RefreshMode string

const (
	// RefreshMissing asks about the title and the seasons with something not yet described, as
	// the scheduled refresh does.
	RefreshMissing RefreshMode = "missing"
	// RefreshAll asks about every season and episode under it as well.
	RefreshAll RefreshMode = "all"
)

func RefreshModes() []RefreshMode {
	return []RefreshMode{RefreshMissing, RefreshAll}
}

// SeasonScope is which of a show's seasons a provider is asked about: those numbered, or every
// season it knows, as a remote show, which has no files to number them, is described.
type SeasonScope string

const (
	SeasonsNumbered SeasonScope = "numbered"
	SeasonsEvery    SeasonScope = "every"
)

// SeasonRequest is which of a show's seasons a provider is asked about, in the order its files are
// numbered in.
type SeasonRequest struct {
	Scope   SeasonScope
	Numbers []int
	Order   EpisodeOrder
}
