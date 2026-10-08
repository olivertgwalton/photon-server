package jellyfin

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/blob"
	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

type themes interface {
	Theme(ctx context.Context, id uuid.UUID) (store.ThemeFile, error)
}

// themeMediaResult is Jellyfin's ThemeMediaResult: the theme songs or videos of OwnerId, the item
// they are of. An app plays them on while its pages are of the same owner, as from a show to its
// seasons.
type themeMediaResult struct {
	Items            []item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
	StartIndex       int    `json:"StartIndex"`
	OwnerID          string `json:"OwnerId"`
}

func themesOf(owner string, songs []item) themeMediaResult {
	return themeMediaResult{Items: songs, TotalRecordCount: len(songs), OwnerID: owner}
}

// themeSongs are the theme tunes a title's page plays under it, as Jellyfin's theme songs: a
// film's or show's own, and a season's or episode's show's where the app asks for its parent's,
// as Jellyfin's web app does. What is not a title the profile sees has none.
func (a *API) themeSongs(w http.ResponseWriter, r *http.Request) (uuid.UUID, themeMediaResult, bool) {
	id, ok := a.itemID(w, r)
	if !ok {
		return id, themeMediaResult{}, false
	}
	p, err := a.svc.Catalogue.Title(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id)
	if isNotFound(err) {
		return id, themesOf(guid(id), []item{}), true
	}
	if err != nil {
		a.internal(w, r, err)
		return id, themeMediaResult{}, false
	}
	owner, name := p.ID, p.Title
	switch p.Kind {
	case domain.ItemSeason, domain.ItemEpisode:
		if !strings.EqualFold(query(r, "inheritFromParent"), "true") || p.Show == nil {
			return id, themesOf(guid(id), []item{}), true
		}
		owner, name = p.Show.ID, p.Show.Title
	case domain.ItemMovie, domain.ItemShow, domain.ItemCollection, domain.ItemExtra:
	}
	songs := make([]item, len(p.Themes))
	for n, theme := range p.Themes {
		songs[n] = a.themeSong(theme, name)
	}
	return id, themesOf(guid(owner), songs), true
}

// themeSong is a theme tune as Jellyfin's Audio item, named for the title it is of.
func (a *API) themeSong(id uuid.UUID, name string) item {
	it := item{Name: name, SortName: name, ServerID: a.id, ID: guid(id), Type: "Audio", LocationType: "FileSystem", MediaType: "Audio"}
	it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
	it.Etag = etag(it)
	return it
}

func (a *API) themeSongsResult(w http.ResponseWriter, r *http.Request) {
	if _, songs, ok := a.themeSongs(w, r); ok {
		a.writeJSON(w, songs)
	}
}

// themeVideos are none: photon's themes are tunes alone.
func (a *API) themeVideos(w http.ResponseWriter, r *http.Request) {
	if id, ok := a.itemID(w, r); ok {
		a.writeJSON(w, themesOf(guid(id), []item{}))
	}
}

// themeMedia answers Jellyfin's AllThemeMediaResult: a title's theme songs, and of theme videos and
// soundtracks none.
func (a *API) themeMedia(w http.ResponseWriter, r *http.Request) {
	id, songs, ok := a.themeSongs(w, r)
	if !ok {
		return
	}
	none := themesOf(guid(id), []item{})
	a.writeJSON(w, struct {
		ThemeVideosResult     themeMediaResult `json:"ThemeVideosResult"`
		ThemeSongsResult      themeMediaResult `json:"ThemeSongsResult"`
		SoundtrackSongsResult themeMediaResult `json:"SoundtrackSongsResult"`
	}{none, songs, none})
}

// themeSongFile serves a theme song's file as it is, at the addresses Jellyfin's apps build for
// audio: universal, which Jellyfin's web app plays a theme song at without asking for its
// PlaybackInfo first, and stream. Nothing is made of it for the app: the tune is played as it is,
// as photon's own apps play it, and is not counted as media sent, as theirs is not.
func (a *API) themeSongFile(w http.ResponseWriter, r *http.Request) {
	if name := strings.ToLower(r.PathValue("file")); name != "universal" && name != "stream" && !strings.HasPrefix(name, "stream.") {
		a.refuse(w, http.StatusNotFound)
		return
	}
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	t, err := a.svc.Themes.Theme(r.Context(), id)
	var o blob.Object
	var kind string
	if err == nil {
		o, kind, err = artwork.OpenTheme(r.Context(), a.svc.Pictures, id, t.Source, t.Root, t.Path)
	}
	if isNotFound(err) || errors.Is(err, os.ErrNotExist) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	h := http.Header{"Cache-Control": {"public, max-age=31536000, immutable"}, "X-Content-Type-Options": {"nosniff"}}
	if kind != "" {
		h.Set("Content-Type", kind)
	}
	if err := blob.Serve(w, r, o, "", h); err != nil {
		a.internal(w, r, err)
	}
}
