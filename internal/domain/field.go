package domain

import "time"

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
// that ranks as high or higher, so a user's edit stands until the user changes it.
type FieldSource string

const (
	SourceFile FieldSource = "file"
	SourceTMDB FieldSource = "tmdb"
	SourceNFO  FieldSource = "nfo"
	SourceUser FieldSource = "user"
)

func FieldSources() []FieldSource {
	return []FieldSource{SourceFile, SourceTMDB, SourceNFO, SourceUser}
}

// Rank orders sources: the reader's own edit, then the NFO they keep beside the file, then a
// provider, then what the file's name says.
func (s FieldSource) Rank() int {
	switch s {
	case SourceFile:
		return 1
	case SourceTMDB:
		return 2
	case SourceNFO:
		return 3
	case SourceUser:
		return 4
	}
	return 0
}
