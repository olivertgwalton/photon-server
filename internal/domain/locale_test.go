package domain

import "testing"

func TestALibraryAsksInItsOwnLocaleElseTheServers(t *testing.T) {
	server := LocaleOf("en-GB")
	for _, tc := range []struct {
		library, want Locale
	}{
		{Locale{}, Locale{Language: "en-GB", Country: "GB"}},
		{Locale{Language: "de-DE"}, Locale{Language: "de-DE", Country: "DE"}},
		{Locale{Language: "fr"}, Locale{Language: "fr", Country: "GB"}},
		{Locale{Country: "IN"}, Locale{Language: "en-GB", Country: "IN"}},
		{Locale{Language: "pt-BR", Country: "PT"}, Locale{Language: "pt-BR", Country: "PT"}},
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
		{Locale{Language: "en-IN", Country: "IN"}, "A", "IN:A"},
		{Locale{Language: "en-IN", Country: "IN"}, "", ""},
		{Locale{Language: "en-IN", Country: "IN"}, "IN:A", "IN:A"},
		// A provider's fallback to the server's own country's is read bare, another's with it.
		{Locale{Language: "en-IN", Country: "IN"}, "GB:15", "15"},
		{Locale{Language: "en-IN", Country: "IN"}, "US:R", "US:R"},
	} {
		if got := tc.loc.Qualified(tc.certificate, server); got != tc.want {
			t.Errorf("%q given in %s: %q, want %q", tc.certificate, tc.loc.Country, got, tc.want)
		}
	}
}

func TestACertificateIsShownWithoutItsCountry(t *testing.T) {
	for certificate, want := range map[string]string{"IN:A": "A", "15": "15", "": "", "PG-13": "PG-13"} {
		if got := Bare(certificate); got != want {
			t.Errorf("%q shown as %q, want %q", certificate, got, want)
		}
	}
}
