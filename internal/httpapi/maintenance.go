package httpapi

import (
	"context"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type maintenanceSettings interface {
	Maintenance(ctx context.Context) (domain.Maintenance, error)
	SetMaintenance(ctx context.Context, m domain.Maintenance) error
}

// maintenanceJSON is the maintenance window, from start_hour to end_hour of the day in time_zone,
// an IANA name, past midnight where it ends before it starts; and when previews are made and
// intros and credits found by sound: window, only inside it, or window_and_added, there and as soon
// as a part is added. Nothing that reads media starts while anything plays.
type maintenanceJSON struct {
	StartHour int           `json:"start_hour"`
	EndHour   int           `json:"end_hour"`
	TimeZone  string        `json:"time_zone"`
	Previews  domain.Timing `json:"previews"`
	Markers   domain.Timing `json:"markers"`
}

func showMaintenance(m domain.Maintenance) maintenanceJSON {
	return maintenanceJSON{StartHour: m.StartHour, EndHour: m.EndHour, TimeZone: m.Zone.String(), Previews: m.Previews, Markers: m.Markers}
}

// adminMaintenance answers the maintenance window, as Plex's Scheduled Tasks settings show it.
func (a *API) adminMaintenance(r *http.Request, _ none) (maintenanceJSON, error) {
	m, err := a.svc.Maintenance.Maintenance(r.Context())
	if err != nil {
		return maintenanceJSON{}, err
	}
	return showMaintenance(m), nil
}

// setMaintenance replaces the maintenance window, and tells every node, which keeps to it at once.
func (a *API) setMaintenance(r *http.Request, req maintenanceJSON) (maintenanceJSON, error) {
	zone, err := domain.ParseZone(req.TimeZone)
	m := domain.Maintenance{StartHour: req.StartHour, EndHour: req.EndHour, Zone: zone, Previews: req.Previews, Markers: req.Markers}
	if err == nil {
		err = m.Check()
	}
	if err != nil {
		return maintenanceJSON{}, refuse(codeInvalidBody, err.Error())
	}
	if err := a.svc.Maintenance.SetMaintenance(r.Context(), m); err != nil {
		return maintenanceJSON{}, err
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventMaintenanceChanged})
	return showMaintenance(m), nil
}

func (a *API) maintenanceRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "GET /api/v1/admin/maintenance", access: admin,
			summary: "Say when the server reads its media for previews and markers",
			status:  http.StatusOK,
		}, a.adminMaintenance),
		a.handles(route{
			pattern: "PUT /api/v1/admin/maintenance", access: admin,
			summary: "Replace the maintenance window and when previews and markers wait for it",
			status:  http.StatusOK,
		}, a.setMaintenance),
	}
}
