package provider

import "testing"

func TestACertificateIsTheCountrysElseTheUSsElseAnyones(t *testing.T) {
	for _, tc := range []struct {
		country string
		given   []Rated
		want    string
	}{
		{"GB", []Rated{{"US", "PG-13"}, {"GB", "12A"}}, "12A"},
		{"IN", []Rated{{"GB", "12A"}, {"US", "PG-13"}}, "US:PG-13"},
		{"IN", []Rated{{"FR", ""}, {"DE", "12"}, {"GB", "12A"}}, "DE:12"},
		{"IN", nil, ""},
		// A country that gives an empty one gives none.
		{"GB", []Rated{{"GB", ""}, {"US", "R"}}, "US:R"},
	} {
		if got := Certificate(tc.country, tc.given); got != tc.want {
			t.Errorf("in %s from %v: %q, want %q", tc.country, tc.given, got, tc.want)
		}
	}
}
