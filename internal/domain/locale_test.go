package domain

import "testing"

func TestALibraryAsksInItsOwnLocaleElseTheServers(t *testing.T) {
	server := LocaleOf("en-GB")
	for _, tc := range []struct {
		library, want Locale
	}{
		{Locale{}, Locale{"en-GB", "GB"}},
		{Locale{Language: "de-DE"}, Locale{"de-DE", "DE"}},
		{Locale{Language: "fr"}, Locale{"fr", "GB"}},
		{Locale{Country: "IN"}, Locale{"en-GB", "IN"}},
		{Locale{"pt-BR", "PT"}, Locale{"pt-BR", "PT"}},
	} {
		if got := tc.library.Or(server); got != tc.want {
			t.Errorf("%+v under the server's en-GB: %+v, want %+v", tc.library, got, tc.want)
		}
	}
}

func TestACertificateIsWrittenWithItsCountryWhereItIsNotTheServers(t *testing.T) {
	server := LocaleOf("en-GB")
	for _, tc := range []struct {
		loc               Locale
		certificate, want string
	}{
		{server, "15", "15"},
		{Locale{"en-IN", "IN"}, "A", "IN:A"},
		{Locale{"en-IN", "IN"}, "", ""},
		{Locale{"en-IN", "IN"}, "IN:A", "IN:A"},
		// A provider's fallback to the server's own country's is read bare, another's with it.
		{Locale{"en-IN", "IN"}, "GB:15", "15"},
		{Locale{"en-IN", "IN"}, "US:R", "US:R"},
	} {
		if got := tc.loc.Qualified(tc.certificate, server); got != tc.want {
			t.Errorf("%q given in %s: %q, want %q", tc.certificate, tc.loc.Country, got, tc.want)
		}
	}
}
