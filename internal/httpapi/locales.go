package httpapi

import (
	"net/http"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type localesJSON struct {
	// Languages a library may ask its metadata in, IETF tags: those TMDB has translations in.
	Languages []domain.LanguageTag `json:"languages"`
	// Countries a library may take its certificates from, ISO 3166-1 alpha-2 codes: those whose
	// certificates the server can read for parental controls.
	Countries []domain.CountryCode `json:"countries"`
}

// locales lists what a library may ask its metadata in, as Jellyfin's Localization cultures and
// countries do.
func (a *API) locales(w http.ResponseWriter, r *http.Request) {
	countries, err := a.svc.Libraries.CertificateCountries(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, localesJSON{Languages: each(domain.MetadataLanguages(), func(s string) domain.LanguageTag { return domain.LanguageTag(s) }), Countries: each(countries, func(s string) domain.CountryCode { return domain.CountryCode(s) })})
}

// metadataLanguage is a metadata language as sent, written as its standard writes it, or why it is
// refused; "" stays "", the one above's.
func metadataLanguage(s string) (canonical, refusal string) {
	if s == "" {
		return "", ""
	}
	tag, err := language.Parse(s)
	if err != nil {
		return "", "metadata_language is an IETF language tag, such as en-GB"
	}
	return tag.String(), ""
}

// certificationCountry is a certification country as sent, written as its standard writes it, or
// why it is refused; "" stays "", the one above's.
func certificationCountry(s string) (canonical, refusal string) {
	if s == "" {
		return "", ""
	}
	region, err := language.ParseRegion(s)
	if err != nil || !region.IsCountry() {
		return "", "certification_country is an ISO 3166-1 alpha-2 country, such as GB"
	}
	return region.String(), ""
}
