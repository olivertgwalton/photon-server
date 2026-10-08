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
	URL         string              `json:"url"`
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
	URL        string              `json:"url"`
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
		ID: h.ID, Source: h.Source, URL: h.URL, Profile: h.Profile, Status: h.Status, Error: h.Error,
		Matched: h.Matched, Imported: h.Imported, Skipped: h.Skipped, Unmatched: h.Unmatched, Misses: misses,
		CreatedAt: h.CreatedAt.UTC(), FinishedAt: h.FinishedAt,
	}
}

// startImport signs in to the source, so a wrong address or credentials are refused at once, and
// queues the import.
func (a *API) startImport(w http.ResponseWriter, r *http.Request) {
	var req startImportJSON
	if !a.decode(w, r, &req) {
		return
	}
	if req.Source == "" || req.Profile == (uuid.UUID{}) {
		writeProblem(w, a.logger, codeInvalidBody, "source and profile_id are set")
		return
	}
	c := historyimport.Credentials(req.Credentials)
	id, err := a.svc.Importer.Start(r.Context(), req.Source, req.URL, req.Profile, c)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusCreated, createdJSON{id})
}

func (a *API) adminImports(w http.ResponseWriter, r *http.Request) {
	all, err := a.svc.HistoryImports.Imports(r.Context())
	if a.answered(w, r, err) {
		return
	}
	out := make([]importJSON, len(all))
	for i, h := range all {
		out[i] = importOf(h)
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[importJSON]{out})
}

func (a *API) adminImport(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	h, err := a.svc.HistoryImports.Import(r.Context(), id)
	if a.answered(w, r, err) {
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, importOf(h))
}
