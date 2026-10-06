package httpapi

import (
	"net/http"
	"time"
	"uuid"
)

type deviceListingJSON struct {
	ID         string    `json:"id"`
	Device     string    `json:"device"`
	Client     string    `json:"client"`
	Profile    string    `json:"profile"`
	SignedInAt time.Time `json:"signed_in_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ThisDevice bool      `json:"this_device"`
}

func (a *API) devices(w http.ResponseWriter, r *http.Request) {
	session := sessionOf(r)
	list, err := a.svc.Auth.Devices(r.Context(), session)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]deviceListingJSON, len(list))
	for i, d := range list {
		out[i] = deviceListingJSON{
			ID: d.ID.String(), Device: d.DeviceName, Client: d.Client, Profile: d.Profile,
			SignedInAt: d.CreatedAt, LastSeenAt: d.LastSeenAt, ThisDevice: d.ID == session.ID,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[deviceListingJSON]{Items: out})
}

func (a *API) signOutDevice(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeProblem(w, a.logger, codeNotFound, "")
		return
	}
	if !a.answered(w, r, a.svc.Auth.SignOutDevice(r.Context(), sessionOf(r), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
