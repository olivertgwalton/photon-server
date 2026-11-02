package httpapi

import (
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// markerJSON is a stretch of a version, on its whole timeline.
type markerJSON struct {
	Kind    domain.MarkerKind `json:"kind"`
	StartMS int64             `json:"start_ms"`
	EndMS   int64             `json:"end_ms"`
}

// markerAbsentJSON says a part of a version, counted from 0, has no stretch of a kind.
type markerAbsentJSON struct {
	Kind domain.MarkerKind `json:"kind"`
	Part int               `json:"part"`
}

type markersJSON struct {
	Markers []markerJSON       `json:"markers"`
	Absent  []markerAbsentJSON `json:"absent,omitzero"`
}

// setMarkers says where a version's intro, credits, recap and preview are, on its whole timeline as
// its chapters are, and which of its parts have none of a kind, over whatever its chapters or
// fingerprints say; saying nothing clears what was said.
func (a *API) setMarkers(r *http.Request, req markersJSON) (none, error) {
	markers := make([]domain.Marker, len(req.Markers))
	for i, m := range req.Markers {
		if m.Kind == "" {
			return none{}, refuse(codeInvalidBody, "a marker's kind is intro, credits, recap or preview")
		}
		if m.StartMS < 0 || m.EndMS <= m.StartMS {
			return none{}, refuse(codeInvalidBody, "a marker ends after it starts, at 0 or later")
		}
		markers[i] = domain.Marker{Kind: m.Kind, StartMS: m.StartMS, EndMS: m.EndMS}
	}
	absent := make([]domain.MarkerAbsent, len(req.Absent))
	for i, m := range req.Absent {
		if m.Kind == "" {
			return none{}, refuse(codeInvalidBody, "a marker's kind is intro, credits, recap or preview")
		}
		if m.Part < 0 {
			return none{}, refuse(codeInvalidBody, "a part is counted from 0")
		}
		absent[i] = domain.MarkerAbsent{Kind: m.Kind, Part: m.Part}
	}
	return none{}, notFoundAs(a.svc.Editing.SetMarkers(r.Context(), pathUUID(r, "id"), markers, absent), "no version has that id")
}

func (a *API) markersRoutes() []route {
	return []route{
		a.handles(route{
			pattern: "PUT /api/v1/admin/versions/{id}/markers", access: admin,
			summary: "Say where a version's intro, credits, recap and preview are, or that a part has none, over what was found",
			status:  http.StatusNoContent,
		}, a.setMarkers),
	}
}
