//go:build integration

package jellyfin

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// A film's trailers on YouTube and Vimeo are its remote trailers to an app, by their address, as
// Jellyfin's web app plays them from its trailer button; its featurette there, and a trailer on
// a site no app opens by address, are not.
func TestAnAppIsGivenAFilmsRemoteTrailers(t *testing.T) {
	st, ada, heat, _ := aFilm(t)
	if err := st.SaveIdentity(t.Context(), heat, domain.SourceTMDB, domain.Metadata{Videos: []domain.RemoteVideo{
		{Kind: domain.ExtraTrailer, Site: "YouTube", Key: "LjLamj-b0I8", Name: "Official Trailer"},
		{Kind: domain.ExtraFeaturette, Site: "YouTube", Key: "making-of", Name: "Making Heat"},
		{Kind: domain.ExtraTrailer, Site: "Vimeo", Key: "1234", Name: "Teaser"},
		{Kind: domain.ExtraTrailer, Site: "Dailymotion", Key: "x8", Name: "Elsewhere"},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	api := New(slog.New(slog.DiscardHandler), uuid.NewV7().String(), func() string { return "Den" }, Services{
		RemoteVersions: noRemoteVersions{}, Discover: noDiscoveries{},
		Auth: profiles{"pst_ada": ada}, Catalogue: st, Preferences: st,
	})
	w := serve(api, http.MethodGet, "/Items/"+guid(heat), `MediaBrowser Client="Jellyfin Web", Token="pst_ada"`, "")
	var it struct {
		RemoteTrailers []struct {
			URL  string `json:"Url"`
			Name string `json:"Name"`
		} `json:"RemoteTrailers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &it); w.Code != http.StatusOK || err != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got []string
	for _, tr := range it.RemoteTrailers {
		got = append(got, tr.Name+" "+tr.URL)
	}
	want := []string{"Official Trailer https://www.youtube.com/watch?v=LjLamj-b0I8", "Teaser https://vimeo.com/1234"}
	if !slices.Equal(got, want) {
		t.Errorf("remote trailers %v, want %v", got, want)
	}
}
