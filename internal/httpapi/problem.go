package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type problemCode string

const (
	codeNotFound         problemCode = "not_found"
	codeMethodNotAllowed problemCode = "method_not_allowed"
	codeUnknownParameter problemCode = "unknown_parameter"
)

func (c problemCode) status() int {
	switch c {
	case codeNotFound:
		return http.StatusNotFound
	case codeMethodNotAllowed:
		return http.StatusMethodNotAllowed
	case codeUnknownParameter:
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
