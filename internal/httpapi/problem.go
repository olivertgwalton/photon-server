package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/olivertgwalton/photon-server/internal/artwork"
	"github.com/olivertgwalton/photon-server/internal/auth"
	"github.com/olivertgwalton/photon-server/internal/hls"
	"github.com/olivertgwalton/photon-server/internal/playback"
	"github.com/olivertgwalton/photon-server/internal/plugin"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
	"github.com/olivertgwalton/photon-server/internal/task"
)

type problemCode string

const (
	codeNotFound           problemCode = "not_found"
	codeMethodNotAllowed   problemCode = "method_not_allowed"
	codeUnknownParameter   problemCode = "unknown_parameter"
	codeInvalidParameter   problemCode = "invalid_parameter"
	codeNotReady           problemCode = "not_ready"
	codeInvalidBody        problemCode = "invalid_body"
	codeUnauthenticated    problemCode = "unauthenticated"
	codeInvalidCredentials problemCode = "invalid_credentials" //nolint:gosec // a problem code, not a credential
	codeInternal           problemCode = "internal"
	codePairingNotFound    problemCode = "pairing_not_found"
	codeWrongSecret        problemCode = "wrong_secret"
	codeRateLimited        problemCode = "rate_limited"
	codeNoCompatibleStream problemCode = "no_compatible_stream"
	codeForbidden          problemCode = "forbidden"
	codeConflict           problemCode = "conflict"
	codeTranscodeLimit     problemCode = "transcode_limit"
	// codeProviderUnavailable is a metadata provider, a plugin, that could not be reached or
	// failed on its side.
	codeProviderUnavailable problemCode = "provider_unavailable"
	// RFC 8628's own error names, so a client that knows the RFC needs no mapping.
	codeAuthorizationPending problemCode = "authorization_pending"
	codeSlowDown             problemCode = "slow_down"
	codeExpiredToken         problemCode = "expired_token"
)

// problemCodes are every code; TestEveryProblemCodeIsListed holds the list to the constants.
func problemCodes() []problemCode {
	return []problemCode{
		codeNotFound, codeMethodNotAllowed, codeUnknownParameter, codeInvalidParameter, codeNotReady,
		codeInvalidBody, codeUnauthenticated, codeInvalidCredentials, codeInternal, codePairingNotFound,
		codeWrongSecret, codeRateLimited, codeNoCompatibleStream, codeForbidden, codeConflict,
		codeTranscodeLimit, codeProviderUnavailable, codeAuthorizationPending, codeSlowDown, codeExpiredToken,
	}
}

func (c problemCode) status() int {
	switch c {
	case codeNotFound:
		return http.StatusNotFound
	case codeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case codeUnknownParameter, codeInvalidParameter:
		return http.StatusBadRequest
	case codeNotReady, codeTranscodeLimit:
		return http.StatusServiceUnavailable
	case codeInvalidBody:
		return http.StatusBadRequest
	case codeUnauthenticated, codeInvalidCredentials:
		return http.StatusUnauthorized
	case codeWrongSecret:
		return http.StatusForbidden
	case codeRateLimited:
		return http.StatusTooManyRequests
	case codeNoCompatibleStream:
		return http.StatusUnprocessableEntity
	case codeForbidden:
		return http.StatusForbidden
	case codeConflict:
		return http.StatusConflict
	case codeProviderUnavailable:
		return http.StatusBadGateway
	case codeInternal:
		return http.StatusInternalServerError
	case codePairingNotFound:
		return http.StatusNotFound
	case codeAuthorizationPending, codeSlowDown, codeExpiredToken:
		return http.StatusBadRequest
	}
	panic("httpapi: problem code without a status: " + string(c))
}

// problems are the errors a client may cause and what it is told of each: its code, and a detail,
// or the error's own words where they are written for a client and may carry more than the
// sentinel's.
var problems = []struct {
	err      error
	code     problemCode
	detail   string
	ownWords bool
}{
	{err: store.ErrNotFound, code: codeNotFound},
	{err: fs.ErrNotExist, code: codeNotFound},
	{err: artwork.ErrMissing, code: codeNotFound},
	{err: store.ErrNoNext, code: codeNotFound, ownWords: true},
	{err: store.ErrLibraryExists, code: codeConflict, ownWords: true},
	{err: store.ErrProfileExists, code: codeConflict, ownWords: true},
	{err: store.ErrPluginExists, code: codeConflict, ownWords: true},
	{err: store.ErrNotUserCollection, code: codeConflict, ownWords: true},
	{err: store.ErrLastAdmin, code: codeConflict, ownWords: true},
	{err: store.ErrAdminNeedsPassword, code: codeConflict, ownWords: true},
	{err: store.ErrUnknownPlugin, code: codeInvalidBody, ownWords: true},
	{err: store.ErrNotACandidate, code: codeInvalidBody, detail: "id is one of the title's candidates of that kind"},
	{err: store.ErrMarkerOutsidePart, code: codeInvalidBody, ownWords: true},
	{err: store.ErrMarkerRepeated, code: codeInvalidBody, ownWords: true},
	{err: store.ErrMarkerNoPart, code: codeInvalidBody, ownWords: true},
	{err: auth.ErrDeviceNotFound, code: codeNotFound},
	{err: auth.ErrPairingNotFound, code: codePairingNotFound},
	{err: auth.ErrWrongSecret, code: codeWrongSecret},
	{err: auth.ErrPINNotDigits, code: codeInvalidBody, ownWords: true},
	{err: auth.ErrPasswordTooShort, code: codeInvalidBody, ownWords: true},
	{err: auth.ErrNoPassword, code: codeConflict, ownWords: true},
	{err: task.ErrNoTask, code: codeNotFound},
	{err: playback.ErrNoPlayback, code: codeNotFound, detail: "the playback has stopped, or lapsed"},
	{err: hls.ErrNoRemux, code: codeNotFound, detail: "the playback has stopped, or lapsed"},
	{err: playback.ErrNoSuchAudio, code: codeInvalidBody, detail: "audio_stream is not one of the copy's audio streams"},
	{err: playback.ErrNoSuchSubtitle, code: codeInvalidBody, detail: "subtitle_stream is not one of the copy's subtitle streams"},
	{err: plugin.ErrRefused, code: codeInvalidBody, ownWords: true},
	{err: provider.ErrUnavailable, code: codeProviderUnavailable, ownWords: true},
}

// answered writes the problem err is, if it is one. A client that has gone is told nothing.
func (a *API) answered(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return true
	}
	for _, p := range problems {
		if errors.Is(err, p.err) {
			detail := p.detail
			if p.ownWords {
				detail = err.Error()
			}
			writeProblem(w, a.logger, p.code, detail)
			return true
		}
	}
	a.internal(w, r, err)
	return true
}

type problem struct {
	Title  string      `json:"title"`
	Status int         `json:"status"`
	Code   problemCode `json:"code"`
	Detail string      `json:"detail,omitzero"`
}

func writeProblem(w http.ResponseWriter, logger *slog.Logger, code problemCode, detail string) {
	status := code.status()
	writeJSON(w, logger, "application/problem+json", status, problem{
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
		Detail: detail,
	})
}

func writeJSON(w http.ResponseWriter, logger *slog.Logger, contentType string, status int, v any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Warn("response not written", slog.Any("err", err))
	}
}
