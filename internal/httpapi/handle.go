package httpapi

import (
	"errors"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/store"
)

// none is the body of a route that takes none, or the reply of one that answers its status alone.
type none struct{}

// handles is rt answered by fn: the body read into Req unless Req is none, and fn's answer written
// with rt's status, as JSON unless Resp is none. rt's description takes its body and reply from
// them.
func (a *API) handles[Req, Resp any](rt route, fn func(r *http.Request, req Req) (Resp, error)) route {
	var req Req
	var resp Resp
	if _, ok := any(req).(none); !ok {
		rt.body = req
	}
	if _, ok := any(resp).(none); !ok {
		rt.reply = resp
	}
	rt.handle = func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if rt.body != nil && !a.decode(w, r, &req) {
			return
		}
		resp, err := fn(r, req)
		switch {
		case a.answered(w, r, err):
		case rt.reply == nil:
			w.WriteHeader(rt.status)
		default:
			writeJSON(w, a.logger, "application/json", rt.status, resp)
		}
	}
	return rt
}

// notFoundAs is err, naming the missing resource notFound where it is store.ErrNotFound.
func notFoundAs(err error, notFound string) error {
	if errors.Is(err, store.ErrNotFound) {
		return refuse(codeNotFound, notFound)
	}
	return err
}
