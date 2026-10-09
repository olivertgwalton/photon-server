package domain

import (
	"regexp"
	"time"
)

// Metadata is what one source says about a title. A zero field says nothing.
type Metadata struct {
	Title         string
	SortTitle     string
	OriginalTitle string
	Overview      string
	Tagline       string
	Certificate   string
	ReleaseDate   time.Time
	Year          int
	Genres        []string
	Studios       []string
	IDs           map[Provider]string
	// Videos, Artwork and Ratings are kept per source, not per field.
	Videos  []RemoteVideo
	Artwork []Artwork
	Ratings []Rating
	// Collections are the box sets the source names the title part of.
	Collections []Grouping
	// Credits are its cast and crew, in the source's order.
	Credits []Credit
	// Locked fields are claimed at this source's rank even where it gives no value, so no lower
	// source fills them.
	Locked []Field
}

// Candidate is a title a provider offers as a match, and its poster to tell it by.
type Candidate struct {
	ID            string
	Title         string
	OriginalTitle string
	Year          int
	// Overview is what the provider says it is about, to tell like-named titles apart by.
	Overview string
	Poster   string
}

// SeasonMetadata is what a source says about a season and its episodes, by episode number.
type SeasonMetadata struct {
	Metadata Metadata
	Episodes map[int]Metadata
}

// Field is a piece of a title's metadata whose source is remembered.
type Field string

const (
	FieldTitle         Field = "title"
	FieldSortTitle     Field = "sort_title"
	FieldOriginalTitle Field = "original_title"
	FieldOverview      Field = "overview"
	FieldTagline       Field = "tagline"
	FieldCertificate   Field = "certificate"
	FieldReleaseDate   Field = "release_date"
	FieldYear          Field = "year"
	FieldGenres        Field = "genres"
	FieldStudios       Field = "studios"
)

func Fields() []Field {
	return []Field{
		FieldTitle, FieldSortTitle, FieldOriginalTitle, FieldOverview, FieldTagline,
		FieldCertificate, FieldReleaseDate, FieldYear, FieldGenres, FieldStudios,
	}
}

// FieldSource is where a field's value came from. A value is replaced only by one from a source
// its library ranks as high or higher, so a user's edit stands until the user changes it.
type FieldSource string

const (
	SourceFile FieldSource = "file"
	SourceTMDB FieldSource = "tmdb"
	SourceTVDB FieldSource = "tvdb"
	SourceNFO  FieldSource = "nfo"
	SourceUser FieldSource = "user"
	// SourceMDBList gives ratings alone.
	SourceMDBList FieldSource = "mdblist"
	SourceOMDb    FieldSource = "omdb"
	// SourceOpenSubtitles finds subtitles, and says nothing of titles.
	SourceOpenSubtitles FieldSource = "opensubtitles"
)

func FieldSources() []FieldSource {
	return []FieldSource{SourceFile, SourceTMDB, SourceTVDB, SourceNFO, SourceUser, SourceMDBList, SourceOMDb, SourceOpenSubtitles}
}

// MetadataSources are the built-in sources a library may rank, in an order it chooses: what files
// say always ranks lowest, and a reader's own edit highest. A registered plugin is one too.
func MetadataSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB, SourceTVDB, SourceMDBList, SourceOMDb}
}

// PluginPattern is a metadata plugin's source id: its slug after "plugin:", so none collides with
// a built-in source. It is also the kind of id a plugin files titles under, and Postgres checks
// both by the same pattern.
const PluginPattern = `^plugin:[a-z0-9][a-z0-9-]{0,62}$`

const pluginPrefix = "plugin:"

var pluginSource = regexp.MustCompile(PluginPattern)

func PluginSource(slug string) FieldSource {
	return FieldSource(pluginPrefix + slug)
}

// Plugin answers the slug of a plugin's source id, or false for a built-in source.
func (s FieldSource) Plugin() (string, bool) {
	if !pluginSource.MatchString(string(s)) {
		return "", false
	}
	return string(s[len(pluginPrefix):]), true
}

// Capability is something a metadata provider can do.
type Capability string

const (
	// CapabilityDescribe is matching a title and saying what is known of it.
	CapabilityDescribe Capability = "describe"
	// CapabilitySearch is listing titles by a name, for an admin choosing a match by hand.
	CapabilitySearch Capability = "search"
	CapabilityRate   Capability = "rate"
	// CapabilityPerson is saying what is known of someone a title credits.
	CapabilityPerson Capability = "person"
	// CapabilityList is keeping lists of titles, which a library's collections are made of.
	CapabilityList Capability = "list"
	// CapabilityStream is streaming films and episodes, which a remote library plays.
	CapabilityStream Capability = "stream"
	// CapabilitySubtitles is finding subtitles for a title, and fetching one.
	CapabilitySubtitles Capability = "subtitles"
	// CapabilityEvents is being told of what happens on the server, as a webhook is.
	CapabilityEvents Capability = "events"
	// CapabilitySegments is timing a film's or an episode's intro and credits.
	CapabilitySegments Capability = "segments"
	// CapabilityPages is serving pages of its own the server's menus link to.
	CapabilityPages Capability = "pages"
)

func Capabilities() []Capability {
	return []Capability{CapabilityDescribe, CapabilitySearch, CapabilityRate, CapabilityPerson, CapabilityList, CapabilityStream, CapabilitySubtitles, CapabilityEvents, CapabilitySegments, CapabilityPages}
}

// PluginProtocol is what a plugin speaks: photon's own protocol, or a Stremio addon's.
type PluginProtocol string

const (
	PluginPhoton  PluginProtocol = "photon"
	PluginStremio PluginProtocol = "stremio"
)

func PluginProtocols() []PluginProtocol {
	return []PluginProtocol{PluginPhoton, PluginStremio}
}
