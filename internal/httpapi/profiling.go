package httpapi

import (
	"bytes"
	"net/http"
	"runtime/pprof"
	"time"
)

// cpuProfileSeconds is how long a CPU profile samples where the query does not say, and
// cpuProfileMostSeconds the longest it may.
const (
	cpuProfileSeconds     = 30
	cpuProfileMostSeconds = 300
)

// cpuProfile samples this node's CPU for the seconds asked and answers the profile, which `go build`
// reads for profile-guided optimisation as cmd/photon-server/default.pgo. It is held until the
// end, so a profile refused or abandoned answers nothing half written.
func (a *API) cpuProfile(w http.ResponseWriter, r *http.Request) {
	seconds, ok := a.queryNumber(w, r, "seconds", cpuProfileSeconds, 1, cpuProfileMostSeconds)
	if !ok {
		return
	}
	var profile bytes.Buffer
	if err := pprof.StartCPUProfile(&profile); err != nil {
		writeProblem(w, a.logger, codeConflict, "this node is already taking a CPU profile")
		return
	}
	done := time.NewTimer(time.Duration(seconds) * time.Second)
	select {
	case <-done.C:
	case <-r.Context().Done():
		done.Stop()
	}
	pprof.StopCPUProfile()
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="default.pgo"`)
	writeBody(w, a.logger, profile.Bytes())
}

func (a *API) profilingRoutes() []route {
	return []route{
		{
			pattern: "GET /api/v1/admin/cpu-profile", access: admin,
			summary: "A CPU profile of the node answering, sampled for the seconds asked, to build the server with as default.pgo",
			query:   []param{{"seconds", 0, "How long to sample, from 1 to 300; 30 where left out."}},
			status:  http.StatusOK, reply: asFile{"application/octet-stream"}, handle: a.cpuProfile,
		},
	}
}
