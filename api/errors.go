package api

import (
	"encoding/json"
	"net/http"
)

// Error codes used across all handlers.
const (
	ErrCodeBadRequest   = "BAD_REQUEST"
	ErrCodeNotFound     = "NOT_FOUND"
	ErrCodeInternal     = "INTERNAL_ERROR"
	ErrCodePayloadLimit = "PAYLOAD_TOO_LARGE"
)

// writeError writes a structured ErrorResponse with the given HTTP status.
func writeError(w http.ResponseWriter, status int, code, message string, details ...any) {
	body := ErrorBody{Code: code, Message: message}
	if len(details) > 0 {
		body.Details = details[0]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(ErrorResponse{Error: body})
}

// writeJSON writes v as JSON with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
