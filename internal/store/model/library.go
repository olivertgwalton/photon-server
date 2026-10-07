package model

import (
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Library struct {
	ID          uuid.UUID
	Name        string
	Kind        domain.LibraryKind
	Root        string
	Monitor     domain.Monitor
	RefreshDays int16
	Previews    domain.PreviewLevel
	Markers     domain.MarkerDetection
	Keyframes   domain.KeyframeMode
	Deletion    domain.MediaDeletion
	Themes      domain.ThemeLookup
	// MetadataLanguage and CertificationCountry are nil for the server's own.
	MetadataLanguage     *string
	CertificationCountry *string
	ArtworkLanguage      domain.ArtworkLanguage
}
