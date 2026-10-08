package jellyfin

import (
	"encoding/hex"
	"hash/fnv"
	"net/http"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/domain"
)

// segmentTypes are Jellyfin's MediaSegmentType for each kind of marker.
var segmentTypes = map[domain.MarkerKind]string{
	domain.MarkerIntro: "Intro", domain.MarkerCredits: "Outro", domain.MarkerRecap: "Recap", domain.MarkerPreview: "Preview",
}

type segment struct {
	ID         string `json:"Id"`
	ItemID     string `json:"ItemId"`
	Type       string `json:"Type"`
	StartTicks int64  `json:"StartTicks"`
	EndTicks   int64  `json:"EndTicks"`
}

// mediaSegments answers a title's intro, credits, recap and preview, as Jellyfin's media segments: the
// markers of the copy photon would play, which apps offer to skip.
func (a *API) mediaSegments(w http.ResponseWriter, r *http.Request) {
	id, ok := a.itemID(w, r)
	if !ok {
		return
	}
	c, err := a.svc.Playing.Playable(r.Context(), auth.SessionOf(r.Context()).Profile.ID, id, uuid.UUID{})
	if isNotFound(err) {
		a.refuse(w, http.StatusNotFound)
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	versions, err := a.svc.Catalogue.Versions(r.Context(), []uuid.UUID{id})
	if err != nil {
		a.internal(w, r, err)
		return
	}
	wanted := values(r, "includeSegmentTypes")
	out := struct {
		Items            []segment `json:"Items"`
		TotalRecordCount int       `json:"TotalRecordCount"`
		StartIndex       int       `json:"StartIndex"`
	}{Items: []segment{}}
	for _, v := range versions[id] {
		if v.ID != c.Version {
			continue
		}
		for _, m := range v.Markers {
			kind := segmentTypes[m.Kind]
			if len(wanted) > 0 && !has(wanted, kind) {
				continue
			}
			h := fnv.New128a()
			h.Write(v.ID[:])
			h.Write([]byte(m.Kind))
			out.Items = append(out.Items, segment{
				ID: hex.EncodeToString(h.Sum(nil)), ItemID: guid(id), Type: kind,
				StartTicks: m.StartMS * ticksPerMS, EndTicks: m.EndMS * ticksPerMS,
			})
		}
	}
	out.TotalRecordCount = len(out.Items)
	a.writeJSON(w, out)
}
