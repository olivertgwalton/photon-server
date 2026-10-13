package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
)

type deviceListingJSON struct {
	ID         uuid.UUID `json:"id"`
	Device     string    `json:"device"`
	Client     string    `json:"client"`
	ProfileID  uuid.UUID `json:"profile_id"`
	Profile    string    `json:"profile"`
	SignedInAt time.Time `json:"signed_in_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ThisDevice bool      `json:"this_device"`
}

func (a *API) devices(w http.ResponseWriter, r *http.Request) {
	session := auth.SessionOf(r.Context())
	list, err := a.svc.Auth.Devices(r.Context(), session)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]deviceListingJSON, len(list))
	for i, d := range list {
		out[i] = deviceListingJSON{
			ID: d.ID, Device: d.DeviceName, Client: d.Client, ProfileID: d.ProfileID, Profile: d.Profile,
			SignedInAt: d.CreatedAt, LastSeenAt: d.LastSeenAt, ThisDevice: d.ID == session.ID,
		}
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, listJSON[deviceListingJSON]{Items: out})
}

func (a *API) signOutDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := a.pathID(w, r, "id")
	if !ok {
		return
	}
	if !a.answered(w, r, a.svc.Auth.SignOutDevice(r.Context(), auth.SessionOf(r.Context()), id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) devicesRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/devices", access: signedIn,
			summary: "List the devices signed in that the profile may sign out: its own, a manager's users' too, an admin's every one",
			status:  http.StatusOK, reply: listJSON[deviceListingJSON]{}, handle: a.devices,
		},
		{
			pattern: "DELETE /api/v1/devices/{id}", access: signedIn, summary: "Sign a device out, of those the profile may",
			status: http.StatusNoContent, handle: a.signOutDevice,
		},
	}
}
