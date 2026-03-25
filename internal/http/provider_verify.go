package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleVerifyProvider tests a provider+model combination with a minimal LLM call.
//
//	POST /v1/providers/{id}/verify
//	Body: {"model": "anthropic/claude-sonnet-4"}
//	Response: {"valid": true} or {"valid": false, "error": "..."}
func (h *ProvidersHandler) handleVerifyProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "model")})
		return
	}

	// Look up provider record from DB to get the provider name
	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	// ACP: verify binary exists on the server (no LLM call needed)
	if p.ProviderType == store.ProviderACP {
		binary := p.APIBase
		if binary == "" {
			binary = "claude"
		}
		// Validate binary against known allowlist (same check as registerACPFromDB)
		if binary != "claude" && binary != "codex" && binary != "gemini" && !filepath.IsAbs(binary) {
			writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": "invalid binary path"})
			return
		}
		if _, err := exec.LookPath(binary); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": "binary not found: " + binary})
		} else {
			writeJSON(w, http.StatusOK, map[string]any{"valid": true})
		}
		return
	}

	// Claude CLI: validate model alias locally (no LLM call needed)
	if p.ProviderType == "claude_cli" {
		validModels := map[string]bool{"sonnet": true, "opus": true, "haiku": true}
		if validModels[req.Model] {
			writeJSON(w, http.StatusOK, map[string]any{"valid": true})
		} else {
			writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": "Invalid model. Use: sonnet, opus, or haiku"})
		}
		return
	}

	if h.providerReg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": "no provider registry available"})
		return
	}

	// Use provider's own TenantID (not request context) so cross-tenant admins
	// can verify providers belonging to other tenants.
	provider, err := h.providerReg.GetForTenant(p.TenantID, p.Name)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": "provider not registered: " + p.Name})
		return
	}

	// Non-chat models (image/video generation) can't be verified via Chat API.
	// Accept them if the provider is reachable (already validated above).
	if isNonChatModel(req.Model) {
		writeJSON(w, http.StatusOK, map[string]any{"valid": true, "note": "generation model accepted (not verifiable via chat)"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	_, err = provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "user", Content: "hi"},
		},
		Model: req.Model,
		Options: map[string]any{
			// Use a small but safe value — reasoning models need headroom beyond 1 token.
			"max_tokens": 50,
		},
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": friendlyVerifyError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true})
}

// handleClaudeCLIAuthStatus checks whether the Claude CLI is authenticated on the server.
//
//	GET /v1/providers/claude-cli/auth-status
//	Response: {"logged_in": true, "email": "...", "subscription_type": "max"}
//	     or: {"logged_in": false, "error": "..."}
func (h *ProvidersHandler) handleClaudeCLIAuthStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Try to find CLI path from existing Claude CLI provider in DB
	cliPath := "claude"
	if existing, err := h.store.ListProviders(r.Context()); err == nil {
		for _, p := range existing {
			if p.ProviderType == "claude_cli" && p.APIBase != "" {
				cliPath = p.APIBase
				break
			}
		}
	}

	_, dockerErr := os.Stat("/.dockerenv")
	inDocker := dockerErr == nil

	status, err := providers.CheckClaudeAuthStatus(ctx, cliPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"logged_in": false,
			"error":     err.Error(),
			"in_docker": inDocker,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in":         status.LoggedIn,
		"email":             status.Email,
		"subscription_type": status.SubscriptionType,
		"in_docker":         inDocker,
	})
}

// isNonChatModel returns true for models that cannot be verified via Chat API
// (image/video generation models).
func isNonChatModel(model string) bool {
	nonChatPrefixes := []string{
		"veo-", "google/veo-",
		"dall-e-", "imagen-", "google/imagen-",
		"gemini-2.5-flash-image", "google/gemini-2.5-flash-image",
	}
	m := strings.ToLower(model)
	for _, prefix := range nonChatPrefixes {
		if strings.HasPrefix(m, prefix) || strings.Contains(m, "/"+prefix) {
			return true
		}
	}
	return false
}

// friendlyVerifyError extracts a human-readable message from provider errors.
// Raw errors often contain JSON blobs like: `HTTP 400: minimax: {"type":"error","error":{"type":"bad_request_error","message":"unknown model ..."}}`
func friendlyVerifyError(err error) string {
	// Timeout / context cancellation → user-friendly message
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
		return "Verification timed out — the provider took too long to respond. Please try again."
	}
	if errors.Is(err, context.Canceled) {
		return "Verification was cancelled. Please try again."
	}

	msg := err.Error()

	// Try to extract "message" field from embedded JSON
	if idx := strings.Index(msg, `"message"`); idx >= 0 {
		// Find the value after "message":
		rest := msg[idx:]
		// Look for :"<value>"
		start := strings.Index(rest, `:`)
		if start >= 0 {
			rest = strings.TrimLeft(rest[start+1:], " ")
			if len(rest) > 0 && rest[0] == '"' {
				rest = rest[1:]
				if before, _, ok := strings.Cut(rest, `"`); ok {
					extracted := before
					if extracted != "" {
						return extracted
					}
				}
			}
		}
	}

	// Fallback: strip "HTTP NNN: provider: " prefix for cleaner display
	if idx := strings.LastIndex(msg, ": "); idx >= 0 && idx < len(msg)-2 {
		suffix := msg[idx+2:]
		// If the remainder still looks like JSON, just say "invalid model"
		if strings.HasPrefix(suffix, "{") {
			return "Model not recognized by provider"
		}
		return suffix
	}

	return msg
}
