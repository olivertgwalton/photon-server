package jellyfin

import (
	"cmp"
	"encoding/hex"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// ticksPerMS is Jellyfin's time unit, the tick of 100 ns, in a millisecond.
const ticksPerMS = 10_000

// item is Jellyfin's BaseItemDto, as much of it as photon has to say. Jellyfin writes what it has
// and leaves out what it has not, so an app decodes whichever it needs.
type item struct {
	Name                    string              `json:"Name"`
	OriginalTitle           string              `json:"OriginalTitle,omitempty"`
	ServerID                string              `json:"ServerId"`
	ID                      string              `json:"Id"`
	Etag                    string              `json:"Etag,omitempty"`
	DateCreated             *time.Time          `json:"DateCreated,omitempty"`
	CanDelete               bool                `json:"CanDelete"`
	CanDownload             *bool               `json:"CanDownload,omitempty"`
	Container               string              `json:"Container,omitempty"`
	SortName                string              `json:"SortName,omitempty"`
	PremiereDate            *time.Time          `json:"PremiereDate,omitempty"`
	EndDate                 *time.Time          `json:"EndDate,omitempty"`
	MediaSources            []mediaSource       `json:"MediaSources,omitempty"`
	Path                    string              `json:"Path,omitempty"`
	CriticRating            *float64            `json:"CriticRating,omitempty"`
	OfficialRating          string              `json:"OfficialRating,omitempty"`
	Overview                string              `json:"Overview,omitempty"`
	Taglines                []string            `json:"Taglines,omitempty"`
	Genres                  []string            `json:"Genres,omitempty"`
	CommunityRating         *float64            `json:"CommunityRating,omitempty"`
	RunTimeTicks            int64               `json:"RunTimeTicks,omitempty"`
	ProductionYear          int                 `json:"ProductionYear,omitempty"`
	ProductionLocations     []string            `json:"ProductionLocations,omitempty"`
	IndexNumber             *int                `json:"IndexNumber,omitempty"`
	IndexNumberEnd          *int                `json:"IndexNumberEnd,omitempty"`
	ParentIndexNumber       *int                `json:"ParentIndexNumber,omitempty"`
	ProviderIDs             map[string]string   `json:"ProviderIds,omitempty"`
	IsFolder                bool                `json:"IsFolder"`
	ParentID                string              `json:"ParentId,omitempty"`
	Type                    string              `json:"Type"`
	People                  []person            `json:"People,omitempty"`
	ParentLogoItemID        string              `json:"ParentLogoItemId,omitempty"`
	ParentBackdropItemID    string              `json:"ParentBackdropItemId,omitempty"`
	ParentBackdropImageTags []string            `json:"ParentBackdropImageTags,omitempty"`
	UserData                *userData           `json:"UserData,omitempty"`
	ChildCount              *int                `json:"ChildCount,omitempty"`
	SeriesName              string              `json:"SeriesName,omitempty"`
	SeriesID                string              `json:"SeriesId,omitempty"`
	SeasonID                string              `json:"SeasonId,omitempty"`
	DisplayPreferencesID    string              `json:"DisplayPreferencesId,omitempty"`
	PrimaryImageAspectRatio float64             `json:"PrimaryImageAspectRatio,omitempty"`
	CollectionType          string              `json:"CollectionType,omitempty"`
	SeriesPrimaryImageTag   string              `json:"SeriesPrimaryImageTag,omitempty"`
	SeasonName              string              `json:"SeasonName,omitempty"`
	MediaStreams            []mediaStream       `json:"MediaStreams,omitempty"`
	VideoType               string              `json:"VideoType,omitempty"`
	ImageTags               map[string]string   `json:"ImageTags"`
	BackdropImageTags       []string            `json:"BackdropImageTags"`
	ParentLogoImageTag      string              `json:"ParentLogoImageTag,omitempty"`
	ImageBlurHashes         map[string]blurhash `json:"ImageBlurHashes"`
	LocationType            string              `json:"LocationType"`
	MediaType               string              `json:"MediaType"`
	PlaylistItemID          string              `json:"PlaylistItemId,omitempty"`

	// Trickplay is each copy's thumbnail sheets, by the copy's id and their width.
	Trickplay map[string]map[int]trickplayInfo `json:"Trickplay,omitempty"`

	Chapters []chapterInfo `json:"Chapters,omitempty"`
}

// blurhash is the BlurHashes of an item's pictures of one kind, by tag.
type blurhash map[string]string

// userData is Jellyfin's UserItemDataDto: what the profile has made of an item. The Kotlin SDK
// refuses one short of any field it keeps, and Swift's one without a Key.
type userData struct {
	PlayedPercentage      *float64   `json:"PlayedPercentage,omitempty"`
	UnplayedItemCount     *int       `json:"UnplayedItemCount,omitempty"`
	PlaybackPositionTicks int64      `json:"PlaybackPositionTicks"`
	PlayCount             int        `json:"PlayCount"`
	IsFavorite            bool       `json:"IsFavorite"`
	LastPlayedDate        *time.Time `json:"LastPlayedDate,omitempty"`
	Played                bool       `json:"Played"`
	Key                   string     `json:"Key"`
	ItemID                string     `json:"ItemId"`
}

// person is Jellyfin's BaseItemPerson. Jellyfin's web app shows a cast member's photo by its
// PrimaryImageTag alone.
type person struct {
	Name            string              `json:"Name"`
	ID              string              `json:"Id,omitempty"`
	Role            string              `json:"Role,omitempty"`
	Type            string              `json:"Type"`
	PrimaryImageTag string              `json:"PrimaryImageTag,omitempty"`
	ImageBlurHashes map[string]blurhash `json:"ImageBlurHashes,omitempty"`
}

// itemKinds are Jellyfin's BaseItemKind for each of photon's kinds of title.
var itemKinds = map[domain.ItemKind]string{
	domain.ItemMovie: "Movie", domain.ItemShow: "Series", domain.ItemSeason: "Season",
	domain.ItemEpisode: "Episode", domain.ItemCollection: "BoxSet", domain.ItemExtra: "Video",
}

// collectionTypes are Jellyfin's CollectionType for each kind of library.
var collectionTypes = map[domain.LibraryKind]string{domain.LibraryMovies: "movies", domain.LibraryShows: "tvshows"}

// Shapes of the pictures Jellyfin's apps lay cards out by.
const (
	posterAspect = 2.0 / 3
	stillAspect  = 16.0 / 9
)

func tag(id uuid.UUID) string {
	if id == (uuid.UUID{}) {
		return ""
	}
	return guid(id)
}

func ref(r *store.TitleRef) (id, name string) {
	if r == nil {
		return "", ""
	}
	return guid(r.ID), r.Title
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// pictures fills an item's pictures: tags are photon's picture ids, which change whenever the
// picture does, and the image route serves a picture by its tag.
func (it *item) pictures(primary, backdrop, logo, thumb uuid.UUID, hashes store.Blurhashes) {
	it.ImageTags, it.BackdropImageTags, it.ImageBlurHashes = map[string]string{}, []string{}, map[string]blurhash{}
	add := func(kind string, id uuid.UUID) {
		if id == (uuid.UUID{}) {
			return
		}
		if kind == "Backdrop" {
			it.BackdropImageTags = append(it.BackdropImageTags, guid(id))
		} else {
			it.ImageTags[kind] = guid(id)
		}
		if h, ok := hashes[id]; ok {
			if it.ImageBlurHashes[kind] == nil {
				it.ImageBlurHashes[kind] = blurhash{}
			}
			it.ImageBlurHashes[kind][guid(id)] = h
		}
	}
	add("Primary", primary)
	add("Backdrop", backdrop)
	add("Logo", logo)
	add("Thumb", thumb)
}

// newItem is what every item says: its name, kind and ids, and that photon has its files.
func (a *API) newItem(id uuid.UUID, kind domain.ItemKind, name string) item {
	it := item{
		Name: name, SortName: name, ServerID: a.id, ID: guid(id), Type: itemKinds[kind], LocationType: "FileSystem",
		MediaType: "Unknown",
	}
	switch kind {
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra:
		it.MediaType, it.VideoType = "Video", "VideoFile"
	case domain.ItemShow, domain.ItemSeason, domain.ItemCollection:
		it.IsFolder = true
	}
	return it
}

// fromCard is a title as a list shows it.
func (a *API) fromCard(c store.Card) item {
	it := a.newItem(c.ID, c.Kind, c.Title)
	it.DateCreated, it.PremiereDate = optionalTime(c.AddedAt), optionalTime(c.ReleaseDate)
	it.ProductionYear, it.Overview, it.Genres, it.OfficialRating = c.Year, c.Overview, c.Genres, domain.Bare(c.Certificate)
	it.RunTimeTicks = c.DurationMS * ticksPerMS
	it.ratings(c.Ratings)
	it.SeriesID, it.SeriesName = ref(c.Show)
	it.SeasonID, it.SeasonName = ref(c.Season)
	it.ParentIndexNumber, it.IndexNumber, it.IndexNumberEnd = c.SeasonNumber, c.EpisodeNumber, c.EpisodeEnd
	switch c.Kind {
	case domain.ItemEpisode:
		// An episode's picture is its still; it wears its show's poster, backdrop and lettering.
		it.ParentID = it.SeasonID
		it.pictures(c.Thumb, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, c.Blurhashes)
		it.PrimaryImageAspectRatio = stillAspect
		if c.Backdrop != (uuid.UUID{}) {
			it.ParentBackdropItemID, it.ParentBackdropImageTags = it.SeriesID, []string{guid(c.Backdrop)}
		}
		if c.Logo != (uuid.UUID{}) {
			it.ParentLogoItemID, it.ParentLogoImageTag = it.SeriesID, guid(c.Logo)
		}
		it.SeriesPrimaryImageTag = tag(c.Poster)
	case domain.ItemMovie, domain.ItemShow, domain.ItemSeason, domain.ItemExtra, domain.ItemCollection:
		it.pictures(c.Poster, c.Backdrop, c.Logo, c.Thumb, c.Blurhashes)
		it.PrimaryImageAspectRatio = posterAspect
	}
	it.UserData = a.userData(c.ID, c.State, c.DurationMS, c.Kind)
	return it
}

// fromSeason is a show's season.
func (a *API) fromSeason(show store.TitleRef, s store.SeasonCard) item {
	it := a.newItem(s.ID, domain.ItemSeason, s.Title)
	it.IndexNumber, it.ProductionYear, it.Overview = &s.Number, s.Year, s.Overview
	it.PremiereDate = optionalTime(time.Time(s.Aired))
	it.ParentID, it.SeriesID, it.SeriesName = guid(show.ID), guid(show.ID), show.Title
	it.ChildCount = &s.Episodes
	it.pictures(s.Poster, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, s.Blurhashes)
	it.PrimaryImageAspectRatio = posterAspect
	it.UserData = a.userData(s.ID, s.State, 0, domain.ItemSeason)
	return it
}

// fromTitle is a title's own page, all photon knows of it.
func (a *API) fromTitle(p store.TitlePage, w words.Words) item {
	it := a.fromCard(store.Card{
		ID: p.ID, Kind: p.Kind, Title: p.Title, Year: p.Year, ReleaseDate: time.Time(p.ReleaseDate), AddedAt: p.AddedAt,
		Poster: first(p.Artwork[domain.ArtworkPoster]), Backdrop: first(p.Artwork[domain.ArtworkBackdrop]),
		Logo: first(p.Artwork[domain.ArtworkLogo]), Thumb: first(p.Artwork[domain.ArtworkThumb]), State: p.State,
		Show: p.Show, Season: p.Season, SeasonNumber: p.SeasonNumber, EpisodeNumber: p.EpisodeNumber,
		EpisodeEnd: p.EpisodeEnd, Overview: p.Overview, Genres: p.Genres, Certificate: p.Certificate,
		Ratings: p.Ratings, Blurhashes: p.Blurhashes,
	})
	it.OriginalTitle = p.OriginalTitle
	if p.Tagline != "" {
		it.Taglines = []string{p.Tagline}
	}
	it.ProviderIDs = providerIDs(p.IDs)
	if p.Kind == domain.ItemSeason && p.Show != nil {
		it.ParentID, it.SeriesID, it.SeriesName = guid(p.Show.ID), guid(p.Show.ID), p.Show.Title
	}
	for _, c := range p.Credits {
		who := person{Name: c.Name, ID: guid(c.PersonID), Role: c.Role, Type: cmp.Or(personKinds[c.Kind], "Unknown"), PrimaryImageTag: tag(c.Photo)}
		if h, ok := c.Blurhashes[c.Photo]; ok {
			who.ImageBlurHashes = map[string]blurhash{"Primary": {guid(c.Photo): h}}
		}
		it.People = append(it.People, who)
	}
	it.CanDownload = downloadable(p.Versions)
	if len(p.Versions) > 0 {
		it.sources(p.Versions, w)
		it.MediaStreams = it.MediaSources[0].MediaStreams
		it.trickplay(p.Versions)
		it.chapters(p.Versions)
	}
	if p.Kind == domain.ItemShow {
		n := len(p.Seasons)
		it.ChildCount = &n
	}
	return it
}

// sources fills a playable item's copies, and its running time and container from the first.
func (it *item) sources(versions []store.VersionPage, w words.Words) {
	it.MediaSources = make([]mediaSource, len(versions))
	for n, v := range versions {
		it.MediaSources[n] = sourceOf(v, w)
	}
	it.Container, it.Path = it.MediaSources[0].Container, it.MediaSources[0].Path
	if it.RunTimeTicks == 0 {
		it.RunTimeTicks = it.MediaSources[0].RunTimeTicks
	}
}

// downloadable is whether /Items/{itemId}/Download serves a title's file: the copy it serves, the
// one played unasked, is on disk in one file. Jellyfin says so only where it is asked.
func downloadable(versions []store.VersionPage) *bool {
	ok := len(versions) > 0 && versions[0].MissingSince == nil && versions[0].Parts == 1
	return &ok
}

// providerIDs names each provider's id as Jellyfin's providers name theirs.
func providerIDs(ids map[domain.Provider]string) map[string]string {
	names := map[domain.Provider]string{domain.ProviderTMDB: "Tmdb", domain.ProviderIMDb: "Imdb", domain.ProviderTVDB: "Tvdb"}
	out := map[string]string{}
	for p, v := range ids {
		if name, ok := names[p]; ok {
			out[name] = v
		}
	}
	return out
}

// personKinds are Jellyfin's PersonKind for each kind of credit.
var personKinds = map[domain.CreditKind]string{
	domain.CreditActor: "Actor", domain.CreditGuestStar: "GuestStar", domain.CreditDirector: "Director",
	domain.CreditWriter: "Writer", domain.CreditProducer: "Producer", domain.CreditComposer: "Composer",
	domain.CreditCreator: "Creator",
}

// ratings sets the audience's score out of ten and the critics' out of a hundred, as Jellyfin's
// CommunityRating and CriticRating are.
func (it *item) ratings(rs []domain.Rating) {
	for _, r := range rs {
		switch r.Site {
		case domain.SiteTMDB, domain.SiteIMDb:
			if it.CommunityRating == nil {
				score := float64(int(r.Score)) / 10
				it.CommunityRating = &score
			}
		case domain.SiteRottenTomatoes:
			score := r.Score
			it.CriticRating = &score
		case domain.SiteRottenTomatoesAudience:
		}
	}
}

// userData is what the profile has made of a title, running length long.
func (a *API) userData(id uuid.UUID, s store.TitleState, lengthMS int64, kind domain.ItemKind) *userData {
	u := &userData{
		PlaybackPositionTicks: s.PositionMS * ticksPerMS, PlayCount: s.Plays, IsFavorite: s.FavouriteAt != nil,
		LastPlayedDate: s.LastPlayedAt, Played: s.WatchedAt != nil, Key: guid(id), ItemID: guid(id),
	}
	if s.PositionMS > 0 && lengthMS > 0 {
		pct := float64(s.PositionMS) / float64(lengthMS) * 100
		u.PlayedPercentage = &pct
	}
	switch kind {
	case domain.ItemShow, domain.ItemSeason:
		u.UnplayedItemCount = &s.Unwatched
	case domain.ItemMovie, domain.ItemEpisode, domain.ItemExtra, domain.ItemCollection:
	}
	return u
}

// etag changes whenever what a list says of an item does, so an app that keeps items knows to read
// one again.
func etag(it item) string {
	h := fnv.New64a()
	for _, s := range []string{
		it.Name, it.Overview, it.OfficialRating, strconv.Itoa(it.ProductionYear),
		strconv.FormatInt(it.RunTimeTicks, 10), it.ImageTags["Primary"], strings.Join(it.BackdropImageTags, ","),
		strconv.Itoa(len(it.MediaSources)),
	} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func first(ids []uuid.UUID) uuid.UUID {
	if len(ids) == 0 {
		return uuid.UUID{}
	}
	return ids[0]
}

// library is a library as Jellyfin's apps open one: a CollectionFolder of films or shows.
func (a *API) library(l *store.SeenLibrary) item {
	it := item{
		Name: l.Name, SortName: l.Name, ServerID: a.id, ID: guid(l.ID), Type: "CollectionFolder", IsFolder: true,
		CollectionType: cmp.Or(collectionTypes[l.Kind], "unknown"), DisplayPreferencesID: guid(l.ID),
		LocationType: "FileSystem", MediaType: "Unknown",
	}
	it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
	it.Etag = etag(it)
	return it
}
