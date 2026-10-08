package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

var backupNameParam = param{"name", "", "The dump's file name, as the list gives it."}

type backupRestores interface {
	Begin(ctx context.Context, name string) error
	Underway(ctx context.Context) (domain.Restore, bool, error)
	Last(ctx context.Context) (domain.RestoreOutcome, bool, error)
}

type backupJSON struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	MadeAt    time.Time `json:"made_at"`
}

type restoreJSON struct {
	Dump      string              `json:"dump"`
	NodeID    uuid.UUID           `json:"node_id"`
	StartedAt time.Time           `json:"started_at"`
	Phase     domain.RestorePhase `json:"phase"`
}

type restoreOutcomeJSON struct {
	Dump   string               `json:"dump"`
	At     time.Time            `json:"at"`
	Result domain.RestoreResult `json:"result"`
	Reason string               `json:"reason,omitzero"`
}

// backupsJSON are the dumps in the backup folder of the node answering, named by node: each node
// keeps those it made, as it held the scheduler's lease, so others may keep others. Restoring is
// the restore under way, across the cluster, and last_restore how the last ended.
type backupsJSON struct {
	NodeID      uuid.UUID           `json:"node_id"`
	NodeName    string              `json:"node_name"`
	Folder      string              `json:"folder"`
	Items       []backupJSON        `json:"items"`
	Restoring   *restoreJSON        `json:"restoring,omitzero"`
	LastRestore *restoreOutcomeJSON `json:"last_restore,omitzero"`
}

func (a *API) adminBackups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dumps, err := backup.List(a.svc.Setup.BackupDir)
	if a.answered(w, r, err) {
		return
	}
	self := a.svc.Placer.Self()
	out := backupsJSON{NodeID: self.ID, NodeName: self.Name, Folder: a.svc.Setup.BackupDir, Items: []backupJSON{}}
	for _, d := range dumps {
		out.Items = append(out.Items, backupJSON{d.Name, d.Size, d.MadeAt})
	}
	underway, ok, err := a.svc.Backups.Underway(ctx)
	if a.answered(w, r, err) {
		return
	}
	if ok {
		out.Restoring = &restoreJSON{underway.Dump, underway.Node, underway.Started, underway.Phase}
	}
	last, ok, err := a.svc.Backups.Last(ctx)
	if a.answered(w, r, err) {
		return
	}
	if ok {
		out.LastRestore = &restoreOutcomeJSON{last.Dump, last.At, last.Result, last.Reason}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, out)
}

func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	f, err := backup.Open(a.svc.Setup.BackupDir, name)
	if a.answered(w, r, err) {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if a.answered(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// restoreBackup asks every node to stop so this one may restore the dump, as Jellyfin schedules
// a restore and restarts; what can be refused is refused first, changing nothing.
func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if a.answered(w, r, a.svc.Backups.Begin(r.Context(), r.PathValue("name"))) {
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// restoringPage is what a browser is shown by a node waiting for a restore to finish; it asks
// again until the node serves the web app.
const restoringPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta http-equiv="refresh" content="5">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Restoring · Photon</title></head>
<body style="font-family: system-ui, sans-serif; display: grid; place-items: center; min-height: 90vh; margin: 0 16px">
<main><h1>Restoring…</h1><p>The server is restoring its database and will be back shortly.</p></main></body></html>
`

// Restoring answers every request while a restore is under way and the node waits to start: not
// ready, as /readyz says to a balancer and the API to a client, and a page a browser shows.
func Restoring(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		if r.URL.Path == "/readyz" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeProblem(w, logger, codeNotReady, "the server is restoring its database")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, restoringPage)
	})
}
