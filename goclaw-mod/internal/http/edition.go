package http

import (
	"encoding/json"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
)

// EditionHandler serves the current edition info for UI feature comparison.
type EditionHandler struct{}

func NewEditionHandler() *EditionHandler { return &EditionHandler{} }

// RegisterRoutes adds the /v1/edition route. No auth required — edition info is public.
func (h *EditionHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/edition", h.handleGet)
}

func (h *EditionHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edition.Current())
}
