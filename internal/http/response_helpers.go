package http

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the standard HTTP error envelope.
// Aligns with WS protocol.ErrorShape for frontend consistency.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError writes a structured error response with code + i18n message.
// code should be a protocol.Err* constant (e.g., protocol.ErrInvalidRequest).
// msg should already be i18n-translated via i18n.T().
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]ErrorResponse{
		"error": {Code: code, Message: msg},
	})
}

// writeJSON writes a JSON response with the given status code.
// Used for success responses and legacy error responses during migration.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
