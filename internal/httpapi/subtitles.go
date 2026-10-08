package httpapi

import (
	"context"
	"net/http"
	"uuid"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type fetchedSubtitles interface {
	Search(ctx context.Context, profile, item, version uuid.UUID, lang language.Tag) (uuid.UUID, []domain.FoundSubtitle, error)
	Fetch(ctx context.Context, profile, item, version uuid.UUID, s domain.FoundSubtitle) (uuid.UUID, error)
	RemoveFetchedSubtitle(ctx context.Context, id uuid.UUID) error
}

// foundSubtitleJSON is a subtitle a provider has for a copy. for_release is one made for the very
// file, found by its hash, which Plex stars.
type foundSubtitleJSON struct {
	Source          domain.FieldSource `json:"source"`
	ID              string             `json:"id"`
	Language        string             `json:"language,omitzero"`
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

// searchSubtitles answers the subtitles providers have in a language for a copy of a film or an
// episode, the one asked for else its longest, as Plex's and Jellyfin's subtitle search: those
// made for its very file first, found by its hash, then the most fetched.
func (a *API) searchSubtitles(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	lang, err := language.Parse(r.URL.Query().Get("language"))
	if err != nil {
		writeProblem(w, a.logger, codeInvalidParameter, "language is a BCP 47 tag")
		return
	}
	version, ok := a.queryID(w, r, "version_id")
	if !ok {
		return
	}
	searched, found, err := a.svc.Subtitles.Search(r.Context(), sessionOf(r).Profile.ID, id, version, lang)
	if a.answered(w, r, err) {
		return
	}
	out := foundSubtitlesJSON{VersionID: searched, Items: make([]foundSubtitleJSON, len(found))}
	for i, f := range found {
		out.Items[i] = foundSubtitleJSON{
			Source: f.Source, ID: f.ID, Language: tag(f.Language), Release: f.Release, HearingImpaired: f.HearingImpaired,
			Forced: f.Forced, ForRelease: f.ForRelease, Downloads: f.Downloads,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

// fetchSubtitleJSON is a subtitle search found, fetched for the copy it was searched for.
type fetchSubtitleJSON struct {
	VersionID uuid.UUID `json:"version_id"`
	foundSubtitleJSON
}

// fetchSubtitle fetches a subtitle a search found and keeps it beside the copy, for every profile
// to choose, as Jellyfin's subtitle download does.
func (a *API) fetchSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	var req fetchSubtitleJSON
	if !a.decode(w, r, &req) {
		return
	}
	lang, err := language.Parse(req.Language)
	if req.Language != "" && err != nil {
		writeProblem(w, a.logger, codeInvalidBody, "language is a BCP 47 tag")
		return
	}
	made, err := a.svc.Subtitles.Fetch(r.Context(), sessionOf(r).Profile.ID, id, req.VersionID, domain.FoundSubtitle{
		Source: req.Source, ID: req.ID, Language: lang, Release: req.Release, HearingImpaired: req.HearingImpaired, Forced: req.Forced,
	})
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, createdJSON{ID: made})
}

// removeFetchedSubtitle forgets a fetched subtitle; one in a library is the library's.
func (a *API) removeFetchedSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if !a.answered(w, r, a.svc.Subtitles.RemoveFetchedSubtitle(r.Context(), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

// tag is a language's BCP 47 tag, or "" for none.
func tag(t language.Tag) string {
	if t == language.Und {
		return ""
	}
	return t.String()
}

func (a *API) subtitlesRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/titles/{id}/subtitles/search", access: signedIn,
			summary: "Find the subtitles providers have in a language for a copy of a film or episode, those made for its very file first",
			query: []param{
				{"language", "", "The language, a BCP 47 tag."},
				{"version_id", uuid.UUID{}, "The copy; the title's longest where none is named."},
			},
			status: http.StatusOK, reply: foundSubtitlesJSON{}, handle: a.searchSubtitles,
		},
		{
			pattern: "POST /api/v1/titles/{id}/subtitles", access: signedIn,
			summary: "Fetch a subtitle a search found and keep it beside the copy, for every profile",
			body:    fetchSubtitleJSON{}, status: http.StatusCreated, reply: createdJSON{}, handle: a.fetchSubtitle,
		},
		{
			pattern: "DELETE /api/v1/admin/subtitles/{id}", access: admin, summary: "Forget a fetched subtitle",
			status: http.StatusNoContent, handle: a.removeFetchedSubtitle,
		},
	}
}
