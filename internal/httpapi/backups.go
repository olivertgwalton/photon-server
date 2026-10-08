package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/backup"
)

var backupNameParam = param{"name", "", "The dump's file name, as the list gives it."}

type backupJSON struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	MadeAt    time.Time `json:"made_at"`
}

// backupsJSON are the dumps in the backup folder of the node answering, named by node: each node
// keeps those it made, as it held the scheduler's lease, so others may keep others.
type backupsJSON struct {
	NodeID   uuid.UUID    `json:"node_id"`
	NodeName string       `json:"node_name"`
	Folder   string       `json:"folder"`
	Items    []backupJSON `json:"items"`
}

func (a *API) adminBackups(w http.ResponseWriter, r *http.Request) {
	dumps, err := backup.List(a.svc.Setup.BackupDir)
	if a.answered(w, r, err) {
		return
	}
	self := a.svc.Placer.Self()
	out := backupsJSON{NodeID: self.ID, NodeName: self.Name, Folder: a.svc.Setup.BackupDir, Items: []backupJSON{}}
	for _, d := range dumps {
		out.Items = append(out.Items, backupJSON{d.Name, d.Size, d.MadeAt})
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
