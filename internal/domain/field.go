package domain

import (
	"fmt"
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
	// Videos and Artwork are what the source links to; they are kept per source, not per field.
	Videos  []RemoteVideo
	Artwork []Artwork
	// Locked fields are claimed at this source's rank even where it gives no value, so no lower
	// source fills them.
	Locked []Field
}

// Candidate is a title a provider offers as a match.
type Candidate struct {
	ID            int
	Title         string
	OriginalTitle string
	Year          int
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
)

func FieldSources() []FieldSource {
	return []FieldSource{SourceFile, SourceTMDB, SourceTVDB, SourceNFO, SourceUser}
}

// MetadataSources are the sources a library may take metadata from, in an order it chooses:
// what files say always ranks lowest, and a reader's own edit highest.
func MetadataSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB, SourceTVDB}
}

// DefaultSources trust an NFO beside the file over a provider, as Jellyfin's default order does.
func DefaultSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB}
}

func ParseMetadataSources(list string) ([]FieldSource, error) {
	var out []FieldSource
	for name := range strings.SplitSeq(list, ",") {
		src := FieldSource(strings.TrimSpace(name))
		if !slices.Contains(MetadataSources(), src) {
			return nil, fmt.Errorf("source %q is not one of %v", src, MetadataSources())
		}
		if slices.Contains(out, src) {
			return nil, fmt.Errorf("source %q is listed twice", src)
		}
		out = append(out, src)
	}
	return out, nil
}
