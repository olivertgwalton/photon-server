package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/kv"
)

// What a client may attempt before it is made to wait. A name has its own limit beside the
// address's, so guessing one profile's password from many addresses is slowed as well.
var (
	signInsPerAddress   = kv.Limit{Every: 6 * time.Second, Burst: 10}
	signInsPerName      = kv.Limit{Every: 6 * time.Minute, Burst: 10}
	switchesPerSession  = kv.Limit{Every: 3 * time.Minute, Burst: 5}
	pairingsPerAddress  = kv.Limit{Every: 6 * time.Second, Burst: 10}
	approvalsPerProfile = kv.Limit{Every: 12 * time.Second, Burst: 5}
)

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
	return what + ":addr:" + a.svc.TrustedProxies.Client(r).String()
}

func nameKey(what, name string) string {
	return what + ":name:" + strings.ToLower(name)
}
