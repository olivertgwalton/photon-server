package jellyfin

import (
	"context"
	"net/http"
	"strings"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
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

// themeOwner is the title whose themes play under an item's page: a film's or show's own, and a
// season's or episode's show's where the app asks for its parent's, as Jellyfin's web app does.
// Nil where it has none: what is not a title the profile sees has none.
func (a *API) themeOwner(w http.ResponseWriter, r *http.Request) (uuid.UUID, *store.TitlePage, bool) {
	id, ok := a.itemID(w, r)
	if !ok {
		return id, nil, false
	}
	profile := auth.SessionOf(r.Context()).Profile.ID
	p, err := a.svc.Catalogue.Title(r.Context(), profile, id)
	if err == nil {
		switch p.Kind {
		case domain.ItemSeason, domain.ItemEpisode:
			if !strings.EqualFold(query(r, "inheritFromParent"), "true") || p.Show == nil {
				return id, nil, true
			}
			p, err = a.svc.Catalogue.Title(r.Context(), profile, p.Show.ID)
		case domain.ItemMovie, domain.ItemShow, domain.ItemCollection, domain.ItemExtra, domain.ItemArtist, domain.ItemAlbum, domain.ItemTrack, domain.ItemAuthor, domain.ItemBook:
		}
	}
	if isNotFound(err) {
		return id, nil, true
	}
	if err != nil {
		a.internal(w, r, err)
		return id, nil, false
	}
	return id, &p, true
}

// themeSongs are the theme tunes of an item's theme owner.
func (a *API) themeSongs(id uuid.UUID, owner *store.TitlePage) themeMediaResult {
	if owner == nil {
		return themesOf(guid(id), nil)
	}
	songs := make([]item, len(owner.Themes))
	for n, theme := range owner.Themes {
		songs[n] = a.themeSong(theme, owner.Title)
	}
	return themesOf(guid(owner.ID), songs)
}

// themeVideos are the theme videos among the extras of an item's theme owner.
func (a *API) themeVideos(w http.ResponseWriter, r *http.Request, id uuid.UUID, owner *store.TitlePage) (themeMediaResult, error) {
	if owner == nil {
		return themesOf(guid(id), nil), nil
	}
	videos, err := a.extraItems(w, r, owner.Extras, listTheme)
	return themesOf(guid(owner.ID), videos), err
}

// themeSong is a theme tune as Jellyfin's Audio item, named for the title it is of.
func (a *API) themeSong(id uuid.UUID, name string) item {
	it := item{Name: name, SortName: name, ServerID: a.id, ID: guid(id), Type: "Audio", LocationType: "FileSystem", MediaType: "Audio"}
	it.pictures(uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, uuid.UUID{}, nil)
	it.Etag = etag(it)
	return it
}

func (a *API) themeSongsResult(w http.ResponseWriter, r *http.Request) {
	if id, owner, ok := a.themeOwner(w, r); ok {
		a.writeJSON(w, a.themeSongs(id, owner))
	}
}

func (a *API) themeVideosResult(w http.ResponseWriter, r *http.Request) {
	id, owner, ok := a.themeOwner(w, r)
	if !ok {
		return
	}
	videos, err := a.themeVideos(w, r, id, owner)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, videos)
}

// themeMedia answers Jellyfin's AllThemeMediaResult: a title's theme songs and videos, and of
// soundtracks none.
func (a *API) themeMedia(w http.ResponseWriter, r *http.Request) {
	id, owner, ok := a.themeOwner(w, r)
	if !ok {
		return
	}
	videos, err := a.themeVideos(w, r, id, owner)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.writeJSON(w, struct {
		ThemeVideosResult     themeMediaResult `json:"ThemeVideosResult"`
		ThemeSongsResult      themeMediaResult `json:"ThemeSongsResult"`
		SoundtrackSongsResult themeMediaResult `json:"SoundtrackSongsResult"`
	}{videos, a.themeSongs(id, owner), themesOf(guid(id), nil)})
}
