package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
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
	Poster        string
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
)

func FieldSources() []FieldSource {
	return []FieldSource{SourceFile, SourceTMDB, SourceTVDB, SourceNFO, SourceUser, SourceMDBList}
}

// MetadataSources are the built-in sources a library may take metadata from, in an order it
// chooses: what files say always ranks lowest, and a reader's own edit highest. A registered
// plugin is one too.
func MetadataSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB, SourceTVDB, SourceMDBList}
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
)

func Capabilities() []Capability {
	return []Capability{CapabilityDescribe, CapabilitySearch, CapabilityRate, CapabilityPerson}
}

// DefaultSources trust an NFO beside the file over a provider, as Jellyfin's default order does,
// and rate titles from MDBList too, as Jellyfin's OMDb and Plex's agent give IMDb's and Rotten
// Tomatoes' scores out of the box; MDBList is passed over until an admin sets its key.
func DefaultSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB, SourceMDBList}
}

func ParseMetadataSources(list string) ([]FieldSource, error) {
	var out []FieldSource
	for name := range strings.SplitSeq(list, ",") {
		out = append(out, FieldSource(strings.TrimSpace(name)))
	}
	return out, CheckMetadataSources(out)
}

// CheckMetadataSources refuses a source a library cannot take metadata from, or one listed twice.
func CheckMetadataSources(list []FieldSource) error {
	for i, src := range list {
		if _, plugin := src.Plugin(); !plugin && !slices.Contains(MetadataSources(), src) {
			return fmt.Errorf("source %q is not one of %v or a plugin's", src, MetadataSources())
		}
		if slices.Contains(list[:i], src) {
			return fmt.Errorf("source %q is listed twice", src)
		}
	}
	return nil
}
