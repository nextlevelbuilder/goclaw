package http

import (
	"encoding/json"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
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

// bindJSON decodes the request body into dest and writes a structured error on failure.
// Returns true if decoding succeeded; false means an error response was already written.
func bindJSON(w http.ResponseWriter, r *http.Request, locale string, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return false
	}
	return true
}

// writeJSON writes a JSON response with the given status code.
// Used for success responses and legacy error responses during migration.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
