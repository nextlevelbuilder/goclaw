package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PendingMessagesHandler handles pending message HTTP endpoints.
type PendingMessagesHandler struct {
	store       store.PendingMessageStore
	agentStore  store.AgentStore
	providerReg *providers.Registry
	keepRecent  int    // global keepRecent from config (0 = use default 15)
	maxTokens   int    // max output tokens for LLM summarization (0 = use default)
	cfgProvider string // config-level provider override (empty = resolve from agent)
	cfgModel    string // config-level model override (empty = resolve from agent)
}

func NewPendingMessagesHandler(s store.PendingMessageStore, agentStore store.AgentStore, providerReg *providers.Registry) *PendingMessagesHandler {
	return &PendingMessagesHandler{store: s, agentStore: agentStore, providerReg: providerReg}
}

// SetKeepRecent sets the global keepRecent value from config.
func (h *PendingMessagesHandler) SetKeepRecent(n int) { h.keepRecent = n }

// SetMaxTokens sets the global maxTokens value from config.
func (h *PendingMessagesHandler) SetMaxTokens(n int) { h.maxTokens = n }

// SetProviderModel sets explicit provider/model from config.
func (h *PendingMessagesHandler) SetProviderModel(provider, model string) {
	h.cfgProvider = provider
	h.cfgModel = model
}

func (h *PendingMessagesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/pending-messages", h.authMiddleware(h.handleListGroups))
	mux.HandleFunc("GET /v1/pending-messages/messages", h.authMiddleware(h.handleListMessages))
	mux.HandleFunc("DELETE /v1/pending-messages", h.authMiddleware(h.handleDelete))
	mux.HandleFunc("POST /v1/pending-messages/compact", h.authMiddleware(h.handleCompact))
}

func (h *PendingMessagesHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// GET /v1/pending-messages — list all groups with resolved titles
func (h *PendingMessagesHandler) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.store.ListGroups(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Resolve group titles from session metadata (best-effort, non-blocking)
	if titles, err := h.store.ResolveGroupTitles(r.Context(), groups); err == nil {
		for i := range groups {
			if t, ok := titles[groups[i].ChannelName+":"+groups[i].HistoryKey]; ok {
				groups[i].GroupTitle = t
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// GET /v1/pending-messages/messages?channel=X&key=Y — list messages for a group
func (h *PendingMessagesHandler) handleListMessages(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	channel := r.URL.Query().Get("channel")
	key := r.URL.Query().Get("key")
	if channel == "" || key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgChannelKeyReq)})
		return
	}

	msgs, err := h.store.ListByKey(r.Context(), channel, key)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// DELETE /v1/pending-messages?channel=X&key=Y — clear a group
func (h *PendingMessagesHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	channel := r.URL.Query().Get("channel")
	key := r.URL.Query().Get("key")
	if channel == "" || key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgChannelKeyReq)})
		return
	}

	if err := h.store.DeleteByKey(r.Context(), channel, key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type compactRequest struct {
	ChannelName string `json:"channel_name"`
	HistoryKey  string `json:"history_key"`
}

// POST /v1/pending-messages/compact — LLM-based summarization of old messages, keeping recent ones.
// Falls back to hard delete if no LLM provider is available.
func (h *PendingMessagesHandler) handleCompact(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	var req compactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if req.ChannelName == "" || req.HistoryKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "channel_name and history_key")})
		return
	}

	// Resolve an LLM provider for summarization using the default agent's config
	provider, model := h.resolveProviderAndModel(r.Context())
	if provider == nil {
		// Fallback: hard delete if no provider available
		slog.Warn("compact.no_provider", "channel", req.ChannelName, "key", req.HistoryKey)
		if err := h.store.DeleteByKey(r.Context(), req.ChannelName, req.HistoryKey); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "method": "deleted"})
		return
	}

	// Run compaction in background so the HTTP response returns immediately.
	// The long-running LLM call (30-120s) was blocking the response, which
	// caused browser WebSocket connections to drop (pong timeout).
	keepRecent := h.keepRecent
	if keepRecent <= 0 {
		keepRecent = 15
	}
	tenantID := store.TenantIDFromContext(r.Context())
	go func() {
		ctx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 180*time.Second)
		defer cancel()
		remaining, err := channels.CompactGroup(ctx, h.store, req.ChannelName, req.HistoryKey, provider, model, keepRecent, h.maxTokens)
		if err != nil {
			slog.Warn("compact.failed", "channel", req.ChannelName, "key", req.HistoryKey, "error", err)
		} else {
			slog.Info("compact.done", "channel", req.ChannelName, "key", req.HistoryKey, "remaining", remaining)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "method": "summarizing"})
}

// resolveProviderAndModel resolves the LLM provider+model for pending message compaction.
// Priority: config provider/model > default agent's provider/model > first available provider.
func (h *PendingMessagesHandler) resolveProviderAndModel(ctx context.Context) (providers.Provider, string) {
	if h.providerReg == nil {
		return nil, ""
	}

	// Config-level provider/model override.
	if h.cfgProvider != "" {
		if p, err := h.providerReg.Get(ctx, h.cfgProvider); err == nil {
			model := h.cfgModel
			if model == "" {
				model = p.DefaultModel()
			}
			if model != "" {
				return p, model
			}
		}
	}

	// Fallback: default agent's provider+model.
	if h.agentStore != nil {
		if ag, err := h.agentStore.GetDefault(ctx); err == nil && ag.Provider != "" {
			if p, err := h.providerReg.GetForTenant(ag.TenantID, ag.Provider); err == nil {
				model := ag.Model
				if model == "" {
					model = p.DefaultModel()
				}
				if model != "" {
					return p, model
				}
			}
		}
	}

	// Fallback: first provider with a valid default model
	for _, name := range h.providerReg.List(ctx) {
		p, err := h.providerReg.Get(ctx, name)
		if err != nil {
			continue
		}
		if p.DefaultModel() != "" {
			return p, p.DefaultModel()
		}
	}
	return nil, ""
}
