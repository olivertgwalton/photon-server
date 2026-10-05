package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
