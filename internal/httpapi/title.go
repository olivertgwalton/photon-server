package httpapi

import (
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/words"
)

// subtitleKind is what a subtitle is made of, which decides how a player may show it: plain text,
// which HLS carries as WebVTT; styled text, whose look WebVTT would lose; or pictures.
type subtitleKind string

const (
	subtitleText    subtitleKind = "text"
	subtitleStyled  subtitleKind = "styled"
	subtitlePicture subtitleKind = "picture"
)

func subtitleKinds() []subtitleKind {
	return []subtitleKind{subtitleText, subtitleStyled, subtitlePicture}
}

func subtitleKindOf(codec string) subtitleKind {
	switch {
	case hls.TextSubtitle(codec):
		return subtitleText
	case hls.StyledSubtitle(codec):
		return subtitleStyled
	}
	return subtitlePicture
}

type titlePageJSON struct {
	ID uuid.UUID `json:"id"`
	// Fetching is a remote film or episode whose provider is fetching a version: it plays once the
	// provider has it, a while from now.
	Fetching bool `json:"fetching,omitzero"`
	// Requestable is a title no library holds that a search found for the profile to ask for,
	// drawn as its provider describes it: it has no versions, seasons or state, and plays nothing.
	Requestable   bool                       `json:"requestable,omitzero"`
	LibraryID     *uuid.UUID                 `json:"library_id,omitzero"`
	Kind          domain.ItemKind            `json:"kind"`
	Title         string                     `json:"title"`
	OriginalTitle string                     `json:"original_title,omitzero"`
	Overview      string                     `json:"overview,omitzero"`
	Tagline       string                     `json:"tagline,omitzero"`
	Certificate   string                     `json:"certificate,omitzero"`
	Year          int                        `json:"year,omitzero"`
	ReleaseDate   domain.Date                `json:"release_date,omitzero"`
	Genres        []string                   `json:"genres,omitzero"`
	Studios       []string                   `json:"studios,omitzero"`
	IDs           map[domain.Provider]string `json:"ids,omitzero"`
	Ratings       []ratingRefJSON            `json:"ratings,omitzero"`
	Collections   []collectionCardJSON       `json:"collections,omitzero"`
	Credits       []creditRefJSON            `json:"credits,omitzero"`
	Origin        domain.CollectionOrigin    `json:"origin,omitzero"`
	// Rule is what finds a smart collection's titles, List the list a list collection holds.
	Rule *smartRuleJSON `json:"rule,omitzero"`
	List *listRefJSON   `json:"list,omitzero"`
	// LibraryIDs are the libraries a smart or list collection reads.
	LibraryIDs []uuid.UUID `json:"library_ids,omitzero"`
	// BoxSet is how a library's wall shows a collection: library, collapsed, expanded or beside.
	BoxSet       domain.CollectionBoxSet `json:"box_set,omitzero"`
	EpisodeOrder domain.EpisodeOrder     `json:"episode_order,omitzero"`
	// EpisodeGroup is the TMDB episode group a show in the group order is numbered by.
	EpisodeGroup string `json:"episode_group,omitzero"`
	// MetadataLanguage, CertificationCountry and TitleLanguage are a film's or show's own, over its
	// library's; absent where it takes its library's.
	MetadataLanguage     domain.LanguageTag   `json:"metadata_language,omitzero"`
	CertificationCountry domain.CountryCode   `json:"certification_country,omitzero"`
	TitleLanguage        domain.TitleLanguage `json:"title_language,omitzero"`
	// QualifiedCertificate is Certificate with the country whose system rates it, where that is not
	// the server's (GB:PG), as parental controls read it and an edit takes it.
	QualifiedCertificate string    `json:"qualified_certificate,omitzero"`
	AddedAt              time.Time `json:"added_at"`
	SeasonNumber         *int      `json:"season_number,omitzero"`
	EpisodeNumber        *int      `json:"episode_number,omitzero"`
	EpisodeEnd           *int      `json:"episode_end,omitzero"`
	// AirsBeforeSeason, AirsBeforeEpisode and AirsAfterSeason are where a special aired among its
	// show's seasons, which it is watched in.
	AirsBeforeSeason  *int              `json:"airs_before_season,omitzero"`
	AirsBeforeEpisode *int              `json:"airs_before_episode,omitzero"`
	AirsAfterSeason   *int              `json:"airs_after_season,omitzero"`
	Show              *titleRefJSON     `json:"show,omitzero"`
	Season            *titleRefJSON     `json:"season,omitzero"`
	Versions          []versionPageJSON `json:"versions,omitzero"`
	Seasons           []seasonCardJSON  `json:"seasons,omitzero"`
	Episodes          []episodeCardJSON `json:"episodes,omitzero"`
	Extras            []extraCardJSON   `json:"extras,omitzero"`
	Videos            []videoLinkJSON   `json:"videos,omitzero"`
	State             titleStateJSON    `json:"state,omitzero"`
	// Artwork is the title's best picture of each kind, by id: /api/v1/artwork/{id}. Every picture a
	// provider offers for it is an admin's to choose from at /api/v1/admin/titles/{id}/artwork/candidates.
	Artwork    map[domain.ArtworkKind][]uuid.UUID `json:"artwork,omitzero"`
	Blurhashes store.Blurhashes                   `json:"blurhashes,omitzero"`
	// Themes are the tunes to play under its page, in order, by id: /api/v1/themes/{id}. A season's
	// and an episode's are its show's.
	Themes []uuid.UUID `json:"themes,omitzero"`
}

type titleRefJSON struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

type versionPageJSON struct {
	ID uuid.UUID `json:"id"`
	// DisplayTitle names the version by its label or edition and its picture, in the reader's
	// language.
	DisplayTitle string            `json:"display_title"`
	Edition      string            `json:"edition,omitzero"`
	Label        string            `json:"label,omitzero"`
	Container    string            `json:"container"`
	DurationMS   int64             `json:"duration_ms"`
	SizeBytes    int64             `json:"size_bytes"`
	BitrateKbps  int               `json:"bitrate_kbps,omitzero"`
	Parts        int               `json:"parts"`
	MissingSince *time.Time        `json:"missing_since,omitzero"`
	Streams      []streamPageJSON  `json:"streams"`
	Subtitles    []subtitleRefJSON `json:"subtitles,omitzero"`
	Chapters     []chapterRefJSON  `json:"chapters,omitzero"`
	Markers      []markerRefJSON   `json:"markers,omitzero"`
	// Files are its parts in order, each where it starts on the version's timeline, by the id the
	// /api/v1/parts/{id} routes take.
	Files []partRefJSON `json:"files"`
	// Trickplay is the thumbnail sheets of each part that has them; a part's sheets are at
	// /api/v1/parts/{id}/trickplay/{sheet}.
	Trickplay []partTrickplayJSON `json:"trickplay,omitzero"`
	// DefaultAudioStream and DefaultSubtitleStream or DefaultSubtitleFile are the tracks it plays
	// with unasked, for the profile asking: none where no subtitle comes on.
	DefaultAudioStream    *int       `json:"default_audio_stream,omitzero"`
	DefaultSubtitleStream *int       `json:"default_subtitle_stream,omitzero"`
	DefaultSubtitleFile   *uuid.UUID `json:"default_subtitle_file,omitzero"`
}

type partRefJSON struct {
	ID         uuid.UUID `json:"id"`
	Index      int       `json:"index"`
	SizeBytes  int64     `json:"size_bytes"`
	DurationMS int64     `json:"duration_ms"`
	OffsetMS   int64     `json:"offset_ms"`
}

type partTrickplayJSON struct {
	PartID   uuid.UUID `json:"part_id"`
	OffsetMS int64     `json:"offset_ms"`
	trickplayJSON
}

type streamPageJSON struct {
	Index int `json:"index"`
	// DisplayTitle names the track as a menu lists it, in the reader's language.
	DisplayTitle    string             `json:"display_title"`
	Kind            domain.StreamKind  `json:"kind"`
	Codec           string             `json:"codec"`
	Profile         string             `json:"profile,omitzero"`
	Language        domain.LanguageTag `json:"language,omitzero"`
	Title           string             `json:"title,omitzero"`
	Default         bool               `json:"default,omitzero"`
	Forced          bool               `json:"forced,omitzero"`
	HearingImpaired bool               `json:"hearing_impaired,omitzero"`
	Commentary      bool               `json:"commentary,omitzero"`
	Width           int                `json:"width,omitzero"`
	Height          int                `json:"height,omitzero"`
	FrameRate       float64            `json:"frame_rate,omitzero"`
	BitDepth        int16              `json:"bit_depth,omitzero"`
	Level           int                `json:"level,omitzero"`
	Range           domain.Range       `json:"range,omitzero"`
	// Resolution is a video's, as a wall's filter files it.
	Resolution domain.Resolution `json:"resolution,omitzero"`
	// SubtitleKind is a subtitle's.
	SubtitleKind  subtitleKind `json:"subtitle_kind,omitzero"`
	DVProfile     int16        `json:"dv_profile,omitzero"`
	Channels      int          `json:"channels,omitzero"`
	ChannelLayout string       `json:"channel_layout,omitzero"`
	SampleRate    int          `json:"sample_rate,omitzero"`
	BitrateKbps   int          `json:"bitrate_kbps,omitzero"`
}

type subtitleRefJSON struct {
	ID              uuid.UUID          `json:"id"`
	DisplayTitle    string             `json:"display_title"`
	Codec           string             `json:"codec"`
	Kind            subtitleKind       `json:"kind"`
	Language        domain.LanguageTag `json:"language,omitzero"`
	Title           string             `json:"title,omitzero"`
	Default         bool               `json:"default,omitzero"`
	Forced          bool               `json:"forced,omitzero"`
	HearingImpaired bool               `json:"hearing_impaired,omitzero"`
}

type ratingRefJSON struct {
	Site  domain.RatingSite `json:"site"`
	Score float64           `json:"score"`
	Votes int               `json:"votes,omitzero"`
}

// chapterRefJSON is a chapter on the version's whole timeline, across its parts. Image is the address
// of its picture, for those that have one.
type chapterRefJSON struct {
	StartMS int64        `json:"start_ms"`
	EndMS   int64        `json:"end_ms"`
	Title   string       `json:"title,omitzero"`
	Image   uriReference `json:"image,omitzero"`
}

type seasonCardJSON struct {
	ID         uuid.UUID        `json:"id"`
	Number     int              `json:"number"`
	Title      string           `json:"title"`
	Overview   string           `json:"overview,omitzero"`
	Year       int              `json:"year,omitzero"`
	Aired      domain.Date      `json:"release_date,omitzero"`
	Episodes   int              `json:"episodes"`
	Poster     uuid.UUID        `json:"poster,omitzero"`
	State      titleStateJSON   `json:"state,omitzero"`
	Blurhashes store.Blurhashes `json:"blurhashes,omitzero"`
}

type episodeCardJSON struct {
	ID uuid.UUID `json:"id"`
	// SeasonNumber is the season it is filed in: 0, the specials', for a special placed in the
	// season listed.
	SeasonNumber *int             `json:"season_number,omitzero"`
	Number       *int             `json:"episode_number,omitzero"`
	End          *int             `json:"episode_end,omitzero"`
	Title        string           `json:"title"`
	Overview     string           `json:"overview,omitzero"`
	Aired        domain.Date      `json:"release_date,omitzero"`
	DurationMS   int64            `json:"duration_ms,omitzero"`
	Thumb        uuid.UUID        `json:"thumb,omitzero"`
	State        titleStateJSON   `json:"state,omitzero"`
	Blurhashes   store.Blurhashes `json:"blurhashes,omitzero"`
}

// extraCardJSON is a trailer or other extra, pictured by a still of its video where its previews
// are made.
type extraCardJSON struct {
	ID         uuid.UUID        `json:"id"`
	Kind       domain.ExtraKind `json:"extra_kind"`
	Title      string           `json:"title"`
	DurationMS int64            `json:"duration_ms,omitzero"`
	Image      uriReference     `json:"image,omitzero"`
}

type collectionCardJSON struct {
	ID         uuid.UUID        `json:"id"`
	Title      string           `json:"title"`
	Poster     uuid.UUID        `json:"poster,omitzero"`
	Blurhashes store.Blurhashes `json:"blurhashes,omitzero"`
}

// videoLinkJSON is a provider's link to a video hosted elsewhere, with its site's still of it
// where the site publishes one, served at /api/v1/artwork/{thumb}.
type videoLinkJSON struct {
	Kind      domain.ExtraKind   `json:"extra_kind"`
	Site      string             `json:"site"`
	Key       string             `json:"key"`
	Name      string             `json:"name"`
	Language  domain.LanguageTag `json:"language,omitzero"`
	Published *time.Time         `json:"published_at,omitzero"`
	Thumb     uuid.UUID          `json:"thumb,omitzero"`
}

type markerRefJSON struct {
	Kind    domain.MarkerKind   `json:"kind"`
	StartMS int64               `json:"start_ms"`
	EndMS   int64               `json:"end_ms"`
	Source  domain.MarkerSource `json:"source"`
}

type creditRefJSON struct {
	// PersonID is none for someone a title to ask for credits whom the server does not know.
	PersonID   uuid.UUID         `json:"person_id,omitzero"`
	Name       string            `json:"name"`
	Kind       domain.CreditKind `json:"kind"`
	Role       string            `json:"role,omitzero"`
	Photo      uuid.UUID         `json:"photo,omitzero"`
	Blurhashes store.Blurhashes  `json:"blurhashes,omitzero"`
}

type titleStateJSON struct {
	PositionMS    int64      `json:"position_ms,omitzero"`
	Plays         int        `json:"plays,omitzero"`
	WatchedAt     *time.Time `json:"watched_at,omitzero"`
	LastPlayedAt  *time.Time `json:"last_played_at,omitzero"`
	FavouriteAt   *time.Time `json:"favourite_at,omitzero"`
	WatchlistedAt *time.Time `json:"watchlisted_at,omitzero"`
	Unwatched     int        `json:"unwatched,omitzero"`
}

func titlePageOf(p store.TitlePage, w words.Words) titlePageJSON {
	return titlePageJSON{
		ID: p.ID, LibraryID: p.Library, Kind: p.Kind, Title: p.Title, OriginalTitle: p.OriginalTitle, Overview: p.Overview,
		Tagline: p.Tagline, Certificate: domain.Bare(p.Certificate), QualifiedCertificate: p.Certificate, Year: p.Year, ReleaseDate: p.ReleaseDate,
		Genres: p.Genres, Studios: p.Studios, IDs: p.IDs,
		Ratings:     each(p.Ratings, func(r domain.Rating) ratingRefJSON { return ratingRefJSON(r) }),
		Collections: each(p.Collections, func(c store.CollectionCard) collectionCardJSON { return collectionCardJSON(c) }),
		Credits:     each(p.Credits, func(c store.CreditRef) creditRefJSON { return creditRefJSON(c) }),
		Origin:      p.Origin, Rule: ruleOf(p.Rule), List: listOf(p.List), LibraryIDs: p.Libraries, BoxSet: p.BoxSet, EpisodeOrder: p.EpisodeOrder, EpisodeGroup: p.EpisodeGroup,
		MetadataLanguage: domain.LanguageTag(p.Locale.Language), CertificationCountry: domain.CountryCode(p.Locale.Country), TitleLanguage: p.Titles, AddedAt: p.AddedAt, SeasonNumber: p.SeasonNumber,
		EpisodeNumber: p.EpisodeNumber, EpisodeEnd: p.EpisodeEnd, AirsBeforeSeason: p.AirsBeforeSeason,
		AirsBeforeEpisode: p.AirsBeforeEpisode, AirsAfterSeason: p.AirsAfterSeason,
		Show: (*titleRefJSON)(p.Show), Season: (*titleRefJSON)(p.Season),
		Versions: each(p.Versions, func(v store.VersionPage) versionPageJSON { return versionPageOf(v, w) }),
		Seasons: each(p.Seasons, func(s store.SeasonCard) seasonCardJSON {
			return seasonCardJSON{
				ID: s.ID, Number: s.Number, Title: s.Title, Overview: s.Overview, Year: s.Year, Aired: s.Aired,
				Episodes: s.Episodes, Poster: s.Poster, State: titleStateJSON(s.State), Blurhashes: s.Blurhashes,
			}
		}),
		Episodes: each(p.Episodes, func(e store.EpisodeCard) episodeCardJSON {
			return episodeCardJSON{
				ID: e.ID, SeasonNumber: e.Season, Number: e.Number, End: e.End, Title: e.Title, Overview: e.Overview, Aired: e.Aired,
				DurationMS: e.DurationMS, Thumb: e.Thumb, State: titleStateJSON(e.State), Blurhashes: e.Blurhashes,
			}
		}),
		Extras: each(p.Extras, func(e store.ExtraCard) extraCardJSON {
			return extraCardJSON{ID: e.ID, Kind: e.Kind, Title: e.Title, DurationMS: e.DurationMS, Image: uriReference(e.Image)}
		}),
		Videos: each(p.Videos, func(v store.VideoLink) videoLinkJSON {
			return videoLinkJSON{
				Kind: v.Kind, Site: v.Site, Key: v.Key, Name: v.Name, Language: domain.LanguageTag(v.Language), Published: v.Published, Thumb: v.Thumb,
			}
		}),
		State:      titleStateJSON(p.State),
		Artwork:    p.Artwork,
		Blurhashes: p.Blurhashes,
		Themes:     p.Themes,
	}
}

func versionPageOf(v store.VersionPage, w words.Words) versionPageJSON {
	return versionPageJSON{
		ID: v.ID, DisplayTitle: w.Version(v), Edition: v.Edition, Label: v.Label, Container: domain.ContainerName(v.Container), DurationMS: v.DurationMS,
		SizeBytes: v.SizeBytes, BitrateKbps: v.BitrateKbps, Parts: v.Parts, MissingSince: v.MissingSince,
		Streams: each(v.Streams, func(s domain.Stream) streamPageJSON { return streamPageOf(s, w) }),
		Subtitles: each(v.Subtitles, func(s store.SubtitleRef) subtitleRefJSON {
			return subtitleRefJSON{
				ID: s.ID, DisplayTitle: w.SubtitleFile(s), Codec: s.Codec, Kind: subtitleKindOf(s.Codec), Language: domain.TagOf(s.Language), Title: s.Title,
				Default: s.Default, Forced: s.Forced, HearingImpaired: s.HearingImpaired,
			}
		}),
		Chapters: each(v.Chapters, func(c store.ChapterRef) chapterRefJSON {
			return chapterRefJSON{StartMS: c.StartMS, EndMS: c.EndMS, Title: c.Title, Image: uriReference(c.Image)}
		}),
		Markers: each(v.Markers, func(m store.MarkerRef) markerRefJSON { return markerRefJSON(m) }),
		Files: each(v.Files, func(f store.PartRef) partRefJSON {
			return partRefJSON{ID: f.ID, Index: f.Index, SizeBytes: f.SizeBytes, DurationMS: f.DurationMS, OffsetMS: f.OffsetMS}
		}),
		Trickplay: each(v.Trickplay, func(t store.PartTrickplay) partTrickplayJSON {
			return partTrickplayJSON{PartID: t.PartID, OffsetMS: t.OffsetMS, trickplayJSON: trickplayJSON(t.Trickplay)}
		}),
		DefaultAudioStream: v.DefaultAudioStream, DefaultSubtitleStream: v.DefaultSubtitleStream,
		DefaultSubtitleFile: v.DefaultSubtitleFile,
	}
}

func streamPageOf(s domain.Stream, w words.Words) streamPageJSON {
	out := streamPageJSON{
		Index: s.Index, DisplayTitle: w.Stream(s), Kind: s.Kind, Codec: s.Codec, Profile: s.Profile, Language: domain.TagOf(s.Language), Title: s.Title,
		Default: s.Default, Forced: s.Forced, HearingImpaired: s.HearingImpaired, Commentary: s.Commentary,
		Width: s.Width, Height: s.Height, FrameRate: s.FrameRate, BitDepth: int16(s.BitDepth), Level: s.Level,
		Range: s.Range, Channels: s.Channels, ChannelLayout: s.ChannelLayout,
		SampleRate: s.SampleRate, BitrateKbps: s.BitrateKbps,
	}
	if s.DolbyVision != nil {
		out.DVProfile = int16(s.DolbyVision.Profile)
	}
	switch s.Kind {
	case domain.StreamVideo:
		if s.Width > 0 {
			out.Resolution = domain.ResolutionOf(s.Width)
		}
	case domain.StreamSubtitle:
		out.SubtitleKind = subtitleKindOf(s.Codec)
	case domain.StreamAudio:
	}
	return out
}
