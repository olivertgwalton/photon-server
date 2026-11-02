package httpapi

import (
	"context"
	"net/http"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

type fetchedSubtitles interface {
	Search(ctx context.Context, profile, item, version uuid.UUID, lang language.Tag) (uuid.UUID, []domain.FoundSubtitle, error)
	Fetch(ctx context.Context, profile, item, version uuid.UUID, s domain.FoundSubtitle) (uuid.UUID, error)
	RemoveFetchedSubtitle(ctx context.Context, id uuid.UUID) error
}

// foundSubtitleJSON is a subtitle a provider has for a version. for_release is one made for the very
// file, found by its hash, which Plex stars.
type foundSubtitleJSON struct {
	Source          domain.FieldSource `json:"source"`
	ID              string             `json:"id"`
	Language        domain.LanguageTag `json:"language,omitzero"`
	Release         string             `json:"release,omitzero"`
	HearingImpaired bool               `json:"hearing_impaired,omitzero"`
	Forced          bool               `json:"forced,omitzero"`
	ForRelease      bool               `json:"for_release,omitzero"`
	Downloads       int                `json:"downloads,omitzero"`
}

type foundSubtitlesJSON struct {
	VersionID uuid.UUID           `json:"version_id"`
	Items     []foundSubtitleJSON `json:"items"`
}

// subtitleCandidates answers the subtitles providers have in a language for a version of a film or an
// episode, the one asked for else its longest, as Plex's and Jellyfin's subtitle search: those
// made for its very file first, found by its hash, then the most fetched.
func (a *API) subtitleCandidates(r *http.Request, _ none) (foundSubtitlesJSON, error) {
	lang, err := language.Parse(r.URL.Query().Get("language"))
	if err != nil {
		return foundSubtitlesJSON{}, refuse(codeInvalidParameter, "language is a BCP 47 tag")
	}
	version, err := queryID(r, "version_id")
	if err != nil {
		return foundSubtitlesJSON{}, err
	}
	searched, found, err := a.svc.Subtitles.Search(r.Context(), auth.SessionOf(r.Context()).Profile.ID, pathUUID(r, "id"), version, lang)
	if err != nil {
		return foundSubtitlesJSON{}, err
	}
	out := foundSubtitlesJSON{VersionID: searched, Items: make([]foundSubtitleJSON, len(found))}
	for i, f := range found {
		out.Items[i] = foundSubtitleJSON{
			Source: f.Source, ID: f.ID, Language: domain.TagOf(f.Language), Release: f.Release, HearingImpaired: f.HearingImpaired,
			Forced: f.Forced, ForRelease: f.ForRelease, Downloads: f.Downloads,
		}
	}
	return out, nil
}

// fetchSubtitleJSON is a subtitle search found, fetched for the version it was searched for.
type fetchSubtitleJSON struct {
	VersionID uuid.UUID `json:"version_id"`
	foundSubtitleJSON
}

// fetchSubtitle fetches a subtitle a search found and keeps it beside the version, for every profile
// to choose, as Jellyfin's subtitle download does.
func (a *API) fetchSubtitle(r *http.Request, req fetchSubtitleJSON) (createdJSON, error) {
	lang, err := language.Parse(string(req.Language))
	if req.Language != "" && err != nil {
		return createdJSON{}, refuse(codeInvalidBody, "language is a BCP 47 tag")
	}
	made, err := a.svc.Subtitles.Fetch(r.Context(), auth.SessionOf(r.Context()).Profile.ID, pathUUID(r, "id"), req.VersionID, domain.FoundSubtitle{
		Source: req.Source, ID: req.ID, Language: lang, Release: req.Release, HearingImpaired: req.HearingImpaired, Forced: req.Forced,
	})
	return createdJSON{ID: made}, err
}

// removeFetchedSubtitle forgets a fetched subtitle; one in a library is the library's.
func (a *API) removeFetchedSubtitle(r *http.Request, _ none) (none, error) {
	return none{}, a.svc.Subtitles.RemoveFetchedSubtitle(r.Context(), pathUUID(r, "id"))
}

func (a *API) subtitlesRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/titles/{id}/subtitles/candidates", access: signedIn,
			summary: "Find the subtitles providers have in a language for a version of a film or episode, those made for its very file first",
			query: []param{
				{"language", domain.LanguageTag(""), "The language, a BCP 47 tag."},
				{"version_id", uuid.UUID{}, "The version; the title's longest where none is named."},
			},
			status: http.StatusOK,
		}, a.subtitleCandidates),
		a.handles(route{
			pattern: "POST /api/v1/titles/{id}/subtitles", access: signedIn,
			summary: "Fetch a subtitle a search found and keep it beside the version, for every profile",
			status:  http.StatusCreated,
		}, a.fetchSubtitle),
		a.handles(route{
			pattern: "DELETE /api/v1/admin/subtitles/{id}", access: admin, summary: "Forget a fetched subtitle",
			status: http.StatusNoContent,
		}, a.removeFetchedSubtitle),
	}
}
