package openlibrary

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
)

// answers are Open Library's, by path and query, as it gave them for these titles.
var answers = map[string]string{
	"/search.json author=J.K.+Rowling&fields=key%2Ctitle%2Cauthor_name&limit=5&title=Harry+Potter+and+the+Deathly+Hallows": `{"docs":[
		{"key":"/works/OL27018711W","title":"Harry Potter and the Deathly Hallows [1/4]","author_name":["J. K. Rowling"]},
		{"key":"/works/OL82586W","title":"Harry Potter and the Deathly Hallows","author_name":["J. K. Rowling"]}]}`,
	"/isbn/9780545010221.json ": `{"key":"/books/OL24384209M","works":[{"key":"/works/OL82586W"}]}`,
	"/search/authors.json limit=10&q=J.K.+Rowling": `{"docs":[
		{"key":"OL16034707A","name":"J.k. Rowling","work_count":11},{"key":"OL23919A","name":"J. K. Rowling","work_count":411},
		{"key":"OL1A","name":"Joanne Rowling Fan Club","work_count":900}]}`,
	"/works/OL82586W.json ": `{"title":"Harry Potter and the Deathly Hallows",
		"description":{"type":"/type/text","value":"Harry sets out to find the Horcruxes."},
		"subjects":["the Elder Wand","children's books","dementors","good and evil","magic","schools"],"covers":[15158660,-1]}`,
	"/authors/OL23919A.json ": `{"name":"J. K. Rowling","bio":"Joanne Rowling, born 1965.","photos":[5543033,-1]}`,
}

func client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "photon-server/") {
			http.Error(w, "say who you are", http.StatusForbidden)
			return
		}
		a, ok := answers[r.URL.Path+" "+r.URL.RawQuery]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, a); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := New("1.0", nil)
	c.api.Base, c.api.HTTP, c.pace.Every = srv.URL+"/", srv.Client(), 0
	return c
}

// A book is matched by its ISBN where its ebook carries one, else by its title less its edition
// and its author written either way; an author by the namesake who wrote most.
func TestBooksAndAuthorsAreMatched(t *testing.T) {
	c := client(t)
	for _, tc := range []struct {
		kind domain.ItemKind
		h    provider.Hints
		want string
	}{
		{domain.ItemBook, provider.Hints{Title: "Harry Potter and the Deathly Hallows (Full-Cast Edition)", Artist: "J.K. Rowling"}, "OL82586W"},
		{domain.ItemBook, provider.Hints{Title: "Anything", IDs: map[domain.Provider]string{domain.ProviderISBN: "9780545010221"}}, "OL82586W"},
		{domain.ItemBook, provider.Hints{Title: "Harry Potter and the Deathly Hallows", Artist: "Someone Else"}, ""},
		{domain.ItemAuthor, provider.Hints{Title: "J.K. Rowling"}, "OL23919A"},
	} {
		got, err := c.Match(t.Context(), domain.Locale{}, tc.kind, tc.h)
		if err != nil || got != tc.want {
			t.Errorf("%s %+v: %q, %v; want %q", tc.kind, tc.h, got, err, tc.want)
		}
	}
}

// A book is described by what its work says, its first subjects and its cover; an author by their
// biography and photograph.
func TestBooksAndAuthorsAreDescribed(t *testing.T) {
	c := client(t)
	book, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemBook, "OL82586W", domain.SeasonRequest{})
	want := domain.Metadata{
		Title: "Harry Potter and the Deathly Hallows", Overview: "Harry sets out to find the Horcruxes.",
		Genres:  []string{"the Elder Wand", "children's books", "dementors", "good and evil", "magic"},
		IDs:     map[domain.Provider]string{domain.ProviderOpenLibrary: "OL82586W"},
		Artwork: []domain.Artwork{{Kind: domain.ArtworkPoster, URL: coversURL + "b/id/15158660-L.jpg"}},
	}
	if diff := cmp.Diff(want, book); err != nil || diff != "" {
		t.Errorf("the book, %v (-want +got):\n%s", err, diff)
	}
	author, _, err := c.Describe(t.Context(), domain.Locale{}, domain.ItemAuthor, "OL23919A", domain.SeasonRequest{})
	if err != nil || author.Overview != "Joanne Rowling, born 1965." || len(author.Artwork) != 1 || author.Artwork[0].URL != coversURL+"a/id/5543033-L.jpg" {
		t.Errorf("the author: %+v, %v", author, err)
	}
}
