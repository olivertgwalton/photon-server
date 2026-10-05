package domain

// ArtworkKind is what a picture of a title is for.
type ArtworkKind string

const (
	ArtworkPoster   ArtworkKind = "poster"
	ArtworkBackdrop ArtworkKind = "backdrop"
	ArtworkLogo     ArtworkKind = "logo"
	// ArtworkThumb is a landscape still: an episode's picture, or a title's on a wide card.
	ArtworkThumb  ArtworkKind = "thumb"
	ArtworkBanner ArtworkKind = "banner"
)

func ArtworkKinds() []ArtworkKind {
	return []ArtworkKind{ArtworkPoster, ArtworkBackdrop, ArtworkLogo, ArtworkThumb, ArtworkBanner}
}

// ArtworkSources are where pictures come from: an admin's choice of a provider's, files beside the
// title, then the providers.
func ArtworkSources() []FieldSource {
	return []FieldSource{SourceUser, SourceFile, SourceTMDB, SourceTVDB}
}

// Artwork is one picture of a title: a file in the library (Path, relative to its root) or a
// provider's (URL).
type Artwork struct {
	Kind     ArtworkKind
	Path     string
	URL      string
	Language string
	Width    int
	Height   int
}
