package domain

import (
	"cmp"
	"strings"
)

// Locale is what a provider is asked in: the language its words are written in, an IETF tag such
// as en-GB, and the country whose certificates it gives, an ISO 3166-1 alpha-2 code such as GB.
type Locale struct {
	Language string
	Country  string
	// Artwork is which of a title's pictures are taken first: "" is localized.
	Artwork ArtworkLanguage
}

// ArtworkLanguage is which of a title's pictures a library takes first, as Plex's "Prefer artwork
// based on library language": those in its language, then English, then wordless; or the most
// liked of any language.
type ArtworkLanguage string

const (
	ArtworkLocalized ArtworkLanguage = "localized"
	ArtworkAny       ArtworkLanguage = "any"
)

func ArtworkLanguages() []ArtworkLanguage {
	return []ArtworkLanguage{ArtworkLocalized, ArtworkAny}
}

// LocaleOf is the locale a language tag alone names: its region is its country, as en-GB's is GB.
func LocaleOf(tag string) Locale {
	_, region, _ := strings.Cut(tag, "-")
	return Locale{Language: tag, Country: strings.ToUpper(region)}
}

// Base is the language without its region: en of en-GB.
func (l Locale) Base() string {
	base, _, _ := strings.Cut(l.Language, "-")
	return base
}

// Or is l with what it leaves unsaid taken from def. A country unsaid is first its own language's
// region, where that names one: a library asking in de-DE wants Germany's certificates.
func (l Locale) Or(def Locale) Locale {
	return Locale{
		Language: cmp.Or(l.Language, def.Language),
		Country:  cmp.Or(l.Country, LocaleOf(l.Language).Country, def.Country),
		Artwork:  cmp.Or(l.Artwork, def.Artwork),
	}
}

// Qualified is a certificate a provider gave in l's country, as the server keeps it: bare where it
// is the server's own country's (def's), else written with its country (IN:A), as Plex writes
// de/12, so a rating means what it meant where it was given. One the provider already wrote with
// its country, falling back to another's, keeps it but where that is the server's.
func (l Locale) Qualified(certificate string, def Locale) string {
	if country, bare, ok := strings.Cut(certificate, ":"); ok {
		if country == def.Country {
			return bare
		}
		return certificate
	}
	if certificate == "" || l.Country == "" || l.Country == def.Country {
		return certificate
	}
	return l.Country + ":" + certificate
}

// Bare is a certificate as every client is shown it: without the country the server keeps one from
// elsewhere with (IN:A is A), which only parental controls read it by.
func Bare(certificate string) string {
	if country, bare, ok := strings.Cut(certificate, ":"); ok && len(country) == 2 {
		return bare
	}
	return certificate
}

// MetadataLanguages are the languages a library may ask its metadata in: those TMDB has
// translations in, as its configuration lists them.
func MetadataLanguages() []string {
	return []string{
		"af-ZA", "ar-AE", "ar-BH", "ar-EG", "ar-IQ", "ar-JO", "ar-LY", "ar-MA", "ar-QA", "ar-SA",
		"ar-TD", "ar-YE", "be-BY", "bg-BG", "bn-BD", "bn-IN", "br-FR", "ca-AD", "ca-ES", "ch-GU",
		"cs-CZ", "cy-GB", "da-DK", "de-AT", "de-CH", "de-DE", "el-CY", "el-GR", "en-AG", "en-AU",
		"en-BB", "en-BZ", "en-CA", "en-CM", "en-GB", "en-GG", "en-GH", "en-GI", "en-GY", "en-IE",
		"en-JM", "en-KE", "en-LC", "en-MW", "en-NZ", "en-PG", "en-TC", "en-US", "en-ZM", "en-ZW",
		"eo-EO", "es-AR", "es-CL", "es-DO", "es-EC", "es-ES", "es-GQ", "es-GT", "es-HN", "es-MX",
		"es-NI", "es-PA", "es-PE", "es-PY", "es-SV", "es-UY", "et-EE", "eu-ES", "fa-IR", "fi-FI",
		"fr-BF", "fr-CA", "fr-CD", "fr-CI", "fr-FR", "fr-GF", "fr-GP", "fr-MC", "fr-ML", "fr-MU",
		"fr-PF", "ga-IE", "gd-GB", "gl-ES", "he-IL", "hi-IN", "hr-HR", "hu-HU", "hy-AM", "id-ID",
		"it-IT", "it-VA", "ja-JP", "ka-GE", "kk-KZ", "kn-IN", "ko-KR", "ku-TR", "ky-KG", "lt-LT",
		"lv-LV", "ml-IN", "mr-IN", "ms-MY", "ms-SG", "nb-NO", "ne-NP", "nl-BE", "nl-NL", "no-NO",
		"oc-FR", "pa-IN", "pl-PL", "pt-AO", "pt-BR", "pt-MZ", "pt-PT", "ro-MD", "ro-RO", "ru-RU",
		"si-LK", "sk-SK", "sl-SI", "so-SO", "sq-AL", "sq-XK", "sr-ME", "sr-RS", "sv-SE", "sw-TZ",
		"ta-IN", "te-IN", "th-TH", "tl-PH", "tr-TR", "uk-UA", "ur-PK", "uz-UZ", "vi-VN", "zh-CN",
		"zh-HK", "zh-SG", "zh-TW", "zu-ZA",
	}
}
