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

func (a *API) devices(r *http.Request, _ none) (listJSON[deviceListingJSON], error) {
	session := auth.SessionOf(r.Context())
	list, err := a.svc.Auth.Devices(r.Context(), session)
	out := make([]deviceListingJSON, len(list))
	for i, d := range list {
		out[i] = deviceListingJSON{
			ID: d.ID, Device: d.DeviceName, Client: d.Client, ProfileID: d.ProfileID, Profile: d.Profile,
			SignedInAt: d.CreatedAt, LastSeenAt: d.LastSeenAt, ThisDevice: d.ID == session.ID,
		}
	}
	return listJSON[deviceListingJSON]{Items: out}, err
}

func (a *API) signOutDevice(r *http.Request, _ none) (none, error) {
	return none{}, a.svc.Auth.SignOutDevice(r.Context(), auth.SessionOf(r.Context()), pathUUID(r, "id"))
}

func (a *API) devicesRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/devices", access: signedIn,
			summary: "List the devices signed in that the profile may sign out: its own, a manager's users' too, an admin's every one",
			status:  http.StatusOK,
		}, a.devices),
		a.handles(route{
			pattern: "DELETE /api/v1/devices/{id}", access: signedIn, summary: "Sign a device out, of those the profile may",
			status: http.StatusNoContent,
		}, a.signOutDevice),
	}
}
