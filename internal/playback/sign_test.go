package playback

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestASignedAddressOpensOnlyItsPathUntilItLapses(t *testing.T) {
	s := NewSigner([]byte("key"))
	now := time.Unix(1_800_000_000, 0)
	signed := s.Sign("/api/v1/parts/a/stream", now.Add(time.Hour))
	path, query, _ := strings.Cut(signed, "?")
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	exp, sig := q.Get("exp"), q.Get("sig")
	for _, tc := range []struct {
		name  string
		path  string
		exp   string
		sig   string
		at    time.Time
		valid bool
	}{
		{"as signed", path, exp, sig, now, true},
		{"another path", "/api/v1/parts/b/stream", exp, sig, now, false},
		{"a later expiry", path, "1900000000", sig, now, false},
		{"lapsed", path, exp, sig, now.Add(2 * time.Hour), false},
		{"another key's", path, exp, NewSigner([]byte("other")).mac(path, exp), now, false},
	} {
		if got := s.Valid(tc.path, tc.exp, tc.sig, tc.at); got != tc.valid {
			t.Errorf("%s: valid = %v, want %v", tc.name, got, tc.valid)
		}
	}
}
