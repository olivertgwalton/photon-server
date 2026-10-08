package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/olivertgwalton/photon-server/internal/kv"
)

// What a client may attempt before it is made to wait, beside signing in and pairing, which auth
// limits.
var switchesPerSession = kv.Limit{Every: 3 * time.Minute, Burst: 5}

// allowed spends one attempt from each key's allowance and answers 429 with Retry-After when any
// is spent. When the limits cannot be checked it refuses rather than letting attempts through.
func (a *API) allowed(w http.ResponseWriter, r *http.Request, limit kv.Limit, keys ...string) bool {
	for _, key := range keys {
		wait, err := a.svc.Limits.Allow(r.Context(), key, limit)
		if err != nil {
			a.internal(w, r, err)
			return false
		}
		if wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			writeProblem(w, a.logger, codeRateLimited, "")
			return false
		}
	}
	return true
}

func (a *API) addrKey(r *http.Request, what string) string {
	return what + ":addr:" + a.svc.Reach.Client(r).String()
}
