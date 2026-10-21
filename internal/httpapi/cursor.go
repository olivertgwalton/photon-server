package httpapi

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// cursorPageJSON is a page of a list read newest first, and next, where there is more, to ask for
// the page after it by.
type cursorPageJSON[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitzero"`
}

var cursorParams = []param{
	{"after", "", "Where the page starts: the next of the page before it, absent for the first."},
	{"limit", 0, "How many to answer, from 1 to " + strconv.Itoa(maxWallLimit) + "."},
}

// cursorPaging reads where a page of a list read newest first starts and how long it is.
func (a *API) cursorPaging(w http.ResponseWriter, r *http.Request, defaultLimit int) (store.Before, int, bool) {
	before := store.Newest
	if s := r.URL.Query().Get("after"); s != "" {
		var ok bool
		if before, ok = beforeOf(s); !ok {
			writeProblem(w, a.logger, codeInvalidParameter, "after is not a page's next")
			return store.Before{}, 0, false
		}
	}
	limit, ok := a.queryNumber(w, r, "limit", defaultLimit, 1, maxWallLimit)
	return before, limit, ok
}

// nextOf is where the page after one ending at last starts, for a client to give back as it is:
// when and which the last was, nothing else being needed to find the page.
func nextOf(at time.Time, id uuid.UUID) string {
	b := binary.BigEndian.AppendUint64(nil, uint64(at.UnixMicro()))
	return base64.RawURLEncoding.EncodeToString(append(b, id[:]...))
}

func beforeOf(next string) (store.Before, bool) {
	b, err := base64.RawURLEncoding.DecodeString(next)
	if err != nil || len(b) != 8+16 {
		return store.Before{}, false
	}
	return store.Before{At: time.UnixMicro(int64(binary.BigEndian.Uint64(b))), ID: uuid.UUID(b[8:])}, true
}
