package domain

import "strings"

// Locale is what a provider is asked in: the language its words are written in, an IETF tag such
// as en-GB, and the country whose certificates it gives, an ISO 3166-1 alpha-2 code such as GB.
type Locale struct {
	Language string
	Country  string
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
