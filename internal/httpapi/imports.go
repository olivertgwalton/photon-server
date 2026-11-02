package httpapi

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/historyimport"
)

type importer interface {
	Start(ctx context.Context, kind domain.ImportSource, address string, profile uuid.UUID, c historyimport.Credentials) (uuid.UUID, error)
}

type importList interface {
	Imports(ctx context.Context) ([]domain.HistoryImport, error)
	Import(ctx context.Context, id uuid.UUID) (domain.HistoryImport, error)
}

// credentialsJSON signs in to a source: Plex by token, Jellyfin and Emby by username and password.
type credentialsJSON struct {
	Token    string `json:"token,omitzero"`
	Username string `json:"username,omitzero"`
	Password string `json:"password,omitzero"`
}

type startImportJSON struct {
	Source      domain.ImportSource `json:"source"`
	URL         uri                 `json:"url"`
	Credentials credentialsJSON     `json:"credentials"`
	Profile     uuid.UUID           `json:"profile_id"`
}

type missedJSON struct {
	Title  string            `json:"title"`
	Reason domain.ImportMiss `json:"reason"`
}

type importJSON struct {
	ID         uuid.UUID           `json:"id"`
	Source     domain.ImportSource `json:"source"`
	URL        uri                 `json:"url"`
	Profile    uuid.UUID           `json:"profile_id"`
	Status     domain.ImportStatus `json:"status"`
	Error      string              `json:"error,omitzero"`
	Matched    int                 `json:"matched"`
	Imported   int                 `json:"imported"`
	Skipped    int                 `json:"skipped"`
	Unmatched  int                 `json:"unmatched"`
	Misses     []missedJSON        `json:"misses"`
	CreatedAt  time.Time           `json:"created_at"`
	FinishedAt *time.Time          `json:"finished_at"`
}

func importOf(h domain.HistoryImport) importJSON {
	misses := make([]missedJSON, len(h.Misses))
	for i, m := range h.Misses {
		misses[i] = missedJSON(m)
	}
	return importJSON{
		ID: h.ID, Source: h.Source, URL: uri(h.URL), Profile: h.Profile, Status: h.Status, Error: h.Error,
		Matched: h.Matched, Imported: h.Imported, Skipped: h.Skipped, Unmatched: h.Unmatched, Misses: misses,
		CreatedAt: h.CreatedAt.UTC(), FinishedAt: h.FinishedAt,
	}
}

// startImport signs in to the source, so a wrong address or credentials are refused at once, and
// queues the import.
func (a *API) startImport(r *http.Request, req startImportJSON) (createdJSON, error) {
	if req.Source == "" || req.Profile == (uuid.UUID{}) {
		return createdJSON{}, refuse(codeInvalidBody, "source and profile_id are set")
	}
	c := historyimport.Credentials(req.Credentials)
	id, err := a.svc.Importer.Start(r.Context(), req.Source, string(req.URL), req.Profile, c)
	return createdJSON{id}, err
}

func (a *API) adminImports(r *http.Request, _ none) (listJSON[importJSON], error) {
	all, err := a.svc.HistoryImports.Imports(r.Context())
	out := make([]importJSON, len(all))
	for i, h := range all {
		out[i] = importOf(h)
	}
	return listJSON[importJSON]{out}, err
}

func (a *API) adminImport(r *http.Request, _ none) (importJSON, error) {
	h, err := a.svc.HistoryImports.Import(r.Context(), pathUUID(r, "id"))
	return importOf(h), err
}

func (a *API) importsRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/imports", access: admin, summary: "List the imports of watch history, the newest first",
			status: http.StatusOK,
		}, a.adminImports),
		a.handles(route{
			pattern: "POST /api/v1/admin/imports", access: admin,
			summary: "Import a Plex, Jellyfin or Emby user's watch history into a profile, once the source takes its credentials",
			status:  http.StatusCreated,
		}, a.startImport),
		a.handles(route{
			pattern: "GET /api/v1/admin/imports/{id}", access: admin, summary: "How an import of watch history went",
			status: http.StatusOK,
		}, a.adminImport),
	}
}
