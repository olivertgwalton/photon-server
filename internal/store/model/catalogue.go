package model

import (
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Item struct {
	ID        uuid.UUID
	LibraryID uuid.UUID
	Kind      domain.ItemKind
	Title     string
	SortTitle string
	Year      *int
	Folder    string
	AddedAt   time.Time

	ParentID      *uuid.UUID
	SeasonNumber  *int
	EpisodeNumber *int
	EpisodeEnd    *int
	AirDate       *time.Time
	ExtraKind     *domain.ExtraKind

	ScanTitle     string
	OriginalTitle *string
	Overview      *string
	Tagline       *string
	Certificate   *string
	ReleaseDate   *time.Time
	Genres        []string
	Studios       []string
	IdentifiedAt  *time.Time
	EpisodeOrder  domain.EpisodeOrder
}

// LibrarySource is a source a library ranks for what it fetches of a kind of item; position 0 is
// the most trusted.
type LibrarySource struct {
	LibraryID uuid.UUID
	ItemKind  domain.ItemKind
	Fetcher   domain.Fetcher
	Source    domain.FieldSource
	Position  int
	Enabled   bool
}

// LibraryRemoteExtra is a kind of video a library keeps providers' links to.
type LibraryRemoteExtra struct {
	LibraryID uuid.UUID
	Kind      domain.ExtraKind
}

// Artwork is a picture of a title: a file in its library, at place relative to the library's
// root, or a provider's, at place as a URL.
type Artwork struct {
	ID       uuid.UUID
	ItemID   uuid.UUID
	Source   domain.FieldSource
	Kind     domain.ArtworkKind
	Place    string
	Position int
	Folder   *string
	Language *string
	Width    *int
	Height   *int
	Blurhash *string
}

type Version struct {
	ID           uuid.UUID
	ItemID       uuid.UUID
	LibraryID    uuid.UUID
	Fingerprint  []byte
	Edition      *string
	Label        *string
	Container    string
	Width        *int
	Height       *int
	VideoCodec   *string
	VideoRange   *domain.Range
	DVProfile    *int16
	BitrateKbps  int
	SizeBytes    int64
	DurationMS   int64
	MissingSince *time.Time
}

type Part struct {
	ID         uuid.UUID
	VersionID  uuid.UUID
	Idx        int16
	SizeBytes  int64
	DurationMS int64
	OffsetMS   int64
	// FingerprintedAt is when the part's sound was last compared with its season's.
	FingerprintedAt *time.Time
}

type Stream struct {
	PartID          uuid.UUID
	Idx             int
	Kind            domain.StreamKind
	Codec           string
	Profile         *string
	Language        *string
	Title           *string
	IsDefault       bool
	Forced          bool
	HearingImpaired bool
	Commentary      bool
	Width           *int
	Height          *int
	FrameRate       *float64
	BitDepth        *int16
	Level           *int
	VideoRange      *domain.Range
	Interlaced      bool
	DVProfile       *int16
	DVLevel         *int16
	DVCompatibility *int16
	Channels        *int
	ChannelLayout   *string
	SampleRate      *int
	BitrateKbps     *int
}

type Chapter struct {
	PartID  uuid.UUID
	Idx     int
	StartMS int64
	EndMS   int64
	Title   *string
}

// Marker is a stretch of a part a player may offer to skip, from the part's start.
type Marker struct {
	PartID uuid.UUID
	Kind   domain.MarkerKind
	Source domain.MarkerSource
	// StartMS and EndMS are absent where an admin said the part has none of the kind.
	StartMS *int64
	EndMS   *int64
}

// SubtitleFile is a subtitle beside a copy rather than inside it.
type SubtitleFile struct {
	ID              uuid.UUID
	VersionID       uuid.UUID
	LibraryID       uuid.UUID
	RelPath         string
	Codec           string
	Language        *string
	Title           *string
	Forced          bool
	IsDefault       bool
	HearingImpaired bool
	SizeBytes       int64
	MtimeNS         int64
}

type Rating struct {
	ItemID uuid.UUID
	Source domain.FieldSource
	Site   domain.RatingSite
	Score  float32
	Votes  *int
}

type PlaylistEntry struct {
	ID         uuid.UUID
	PlaylistID uuid.UUID
	ItemID     uuid.UUID
	Position   int
}

type Person struct {
	ID       uuid.UUID
	Name     string
	PhotoURL *string
	PhotoID  *uuid.UUID
	// PhotoBlurhash is taken when the photo is first fetched, and dropped with it.
	PhotoBlurhash *string
	Biography     *string
	Born          *time.Time
	Died          *time.Time
	Birthplace    *string
	DescribedAt   *time.Time
}
