package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels/teams/appmanifest"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TeamsAppPackageHandler generates Teams app package ZIPs.
type TeamsAppPackageHandler struct {
	instanceStore store.ChannelInstanceStore
}

// NewTeamsAppPackageHandler creates a handler that can resolve bot_id from channel instances.
func NewTeamsAppPackageHandler(s store.ChannelInstanceStore) *TeamsAppPackageHandler {
	return &TeamsAppPackageHandler{instanceStore: s}
}

// RegisterRoutes registers the Teams app package download endpoint.
func (h *TeamsAppPackageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/teams/app-package", requireAuth("", h.handleGenerate))
}

func (h *TeamsAppPackageHandler) handleGenerate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	q := r.URL.Query()

	name := q.Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}

	botID := q.Get("bot_id")
	if botID == "" {
		if instID := q.Get("instance_id"); instID != "" && h.instanceStore != nil {
			if uid, err := uuid.Parse(instID); err == nil {
				if inst, err := h.instanceStore.Get(r.Context(), uid); err == nil && inst.ChannelType == "teams" && len(inst.Credentials) > 0 {
					var creds map[string]any
					if json.Unmarshal(inst.Credentials, &creds) == nil {
						if v, ok := creds["bot_id"]; ok {
							botID = fmt.Sprintf("%v", v)
						}
					}
				}
			}
		}
	}

	opts := appmanifest.Options{
		BotID:       botID,
		Name:        name,
		FullName:    q.Get("full_name"),
		Description: q.Get("description"),
		Developer:   q.Get("developer"),
	}

	zipData, err := appmanifest.GenerateZIP(opts)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="teams-app-%s.zip"`, sanitizeAppFilename(name)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(zipData)))
	w.Write(zipData)
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeAppFilename removes chars unsafe for filenames, truncates to 50 chars.
func sanitizeAppFilename(s string) string {
	s = unsafeFilenameChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = s[:50]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "bot"
	}
	return s
}
