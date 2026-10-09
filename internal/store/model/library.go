package model

import (
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Library struct {
	ID             uuid.UUID
	Name           string
	Kind           domain.LibraryKind
	Media          domain.LibraryMedia
	Root           *string
	ListSource     *domain.FieldSource
	ListID         *string
	StreamSource   *domain.FieldSource
	DiscoverSource *domain.FieldSource
	Monitor        domain.Monitor
	RefreshDays    int16
	Previews       domain.PreviewLevel
	Markers        domain.MarkerDetection
	Keyframes      domain.KeyframeMode
	Deletion       domain.MediaDeletion
	Themes         domain.ThemeLookup
	// MetadataLanguage and CertificationCountry are nil for the server's own.
	MetadataLanguage     *string
	CertificationCountry *string
	ArtworkLanguage      domain.ArtworkLanguage
	TitleLanguage        domain.TitleLanguage
	CollectionMode       domain.CollectionMode
	SubtitleLanguages    []string
	SubtitleMatch        domain.SubtitleMatch
}
