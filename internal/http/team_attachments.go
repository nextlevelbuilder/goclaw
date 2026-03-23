package http

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TeamAttachmentsHandler serves team task attachment files for download.
type TeamAttachmentsHandler struct {
	teamStore store.TeamStore
	token     string
	dataDir   string
}

// NewTeamAttachmentsHandler creates a new handler for serving team attachments.
func NewTeamAttachmentsHandler(teamStore store.TeamStore, token, dataDir string) *TeamAttachmentsHandler {
	return &TeamAttachmentsHandler{teamStore: teamStore, token: token, dataDir: dataDir}
}

// RegisterRoutes registers the attachment download endpoint.
func (h *TeamAttachmentsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/teams/{teamId}/attachments/{attachmentId}/download", h.authMiddleware(h.handleDownload))
}

func (h *TeamAttachmentsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Priority 1: HMAC-signed file token (?ft=) — no gateway token exposure.
		if ft := r.URL.Query().Get("ft"); ft != "" {
			path := r.URL.Path // full path for HMAC binding
			if VerifyFileToken(ft, path, FileSigningKey()) {
				next(w, r)
				return
			}
			http.Error(w, "invalid or expired file token", http.StatusUnauthorized)
			return
		}
		// Priority 2: Bearer header (API clients).
		provided := extractBearerToken(r)
		if provided == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		authedReq, ok := requireAuthBearer(h.token, "", provided, w, r)
		if !ok {
			return
		}
		next(w, authedReq)
	}
}

func (h *TeamAttachmentsHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if h.teamStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTeamsNotConfigured)})
		return
	}

	teamID, err := uuid.Parse(r.PathValue("teamId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team id"})
		return
	}

	attachmentID, err := uuid.Parse(r.PathValue("attachmentId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment id"})
		return
	}

	att, err := h.teamStore.GetAttachment(r.Context(), attachmentID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// IDOR check: attachment must belong to the requested team.
	if att.TeamID != teamID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "attachment does not belong to this team"})
		return
	}

	// Resolve disk path. Absolute paths (new) are used directly;
	// relative paths (legacy) are joined with the team workspace directory.
	var cleanPath string
	if filepath.IsAbs(att.Path) {
		cleanPath = filepath.Clean(att.Path)
	} else {
		// Legacy: {workspace}/teams/{teamID}/{chatID}/{relPath}
		cleanPath = filepath.Clean(filepath.Join(h.dataDir, "teams", att.TeamID.String(), att.ChatID, att.Path))
	}
	// Security: file must be within the workspace root to prevent path traversal.
	wsRoot := filepath.Clean(h.dataDir) + string(filepath.Separator)
	if !strings.HasPrefix(cleanPath, wsRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment path"})
		return
	}

	// Set download headers with sanitized filename (prevent header injection).
	filename := filepath.Base(att.Path)
	safeName := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' {
			return '_'
		}
		return r
	}, filename)
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName+`"`)
	if att.MimeType != "" {
		w.Header().Set("Content-Type", att.MimeType)
	}

	http.ServeFile(w, r, cleanPath)
}
