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
func (a *API) adminMaintenance(w http.ResponseWriter, r *http.Request) {
	m, err := a.svc.Maintenance.Maintenance(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, a.logger, "application/json", http.StatusOK, showMaintenance(m))
}

// setMaintenance replaces the maintenance window, and tells every node, which keeps to it at once.
func (a *API) setMaintenance(w http.ResponseWriter, r *http.Request) {
	var req maintenanceJSON
	if !a.decode(w, r, &req) {
		return
	}
	zone, err := domain.ParseZone(req.TimeZone)
	m := domain.Maintenance{StartHour: req.StartHour, EndHour: req.EndHour, Zone: zone, Previews: req.Previews, Markers: req.Markers}
	if err == nil {
		err = m.Check()
	}
	if err != nil {
		writeProblem(w, a.logger, codeInvalidBody, err.Error())
		return
	}
	if err := a.svc.Maintenance.SetMaintenance(r.Context(), m); err != nil {
		a.internal(w, r, err)
		return
	}
	a.svc.Events.Raise(r.Context(), domain.Event{Kind: domain.EventMaintenanceChanged})
	writeJSON(w, a.logger, "application/json", http.StatusOK, showMaintenance(m))
}
