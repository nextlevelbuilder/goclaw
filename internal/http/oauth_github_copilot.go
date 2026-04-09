package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type pendingGitHubCopilotFlow struct {
	login            *oauth.PendingGitHubCopilotLogin
	cancel           context.CancelFunc
	flowKey          string
	tenantID         uuid.UUID
	userID           string
	providerName     string
	displayName      string
	apiBase          string
	enterpriseDomain string
}

func (h *OAuthHandler) newGitHubCopilotTokenSource(ctx context.Context, providerName, displayName, apiBase string) *oauth.GitHubCopilotTokenSource {
	return oauth.NewGitHubCopilotTokenSource(h.provStore, h.secretStore, providerName).
		WithTenantID(oauthTenantID(ctx)).
		WithProviderMeta(displayName, apiBase)
}

func (h *OAuthHandler) ensureGitHubCopilotProviderName(ctx context.Context, providerName string) error {
	if h.provStore == nil {
		return nil
	}
	p, err := h.provStore.GetProviderByName(ctx, providerName)
	if err != nil {
		return nil
	}
	if p.ProviderType != store.ProviderGitHubCopilotOAuth {
		return &oauth.ProviderTypeConflictError{ProviderName: providerName, ProviderType: p.ProviderType}
	}
	return nil
}

func (h *OAuthHandler) handleGitHubCopilotStatus(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}
	if err := h.ensureGitHubCopilotProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}
	flowKey := oauthFlowKey(r.Context(), providerName)
	h.mu.Lock()
	pending := h.pendingCopilot[flowKey]
	h.mu.Unlock()
	if pending != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":    false,
			"pending":          true,
			"provider_name":    providerName,
			"verification_uri": pending.login.Device.VerificationURI,
			"user_code":        pending.login.Device.UserCode,
		})
		return
	}
	ts := h.newGitHubCopilotTokenSource(r.Context(), providerName, "", "")
	if !ts.Exists(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "pending": false})
		return
	}
	if _, err := ts.Token(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "pending": false, "error": "token invalid or expired"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "pending": false, "provider_name": providerName})
}

func (h *OAuthHandler) handleGitHubCopilotStart(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}
	var body struct {
		DisplayName      string `json:"display_name"`
		APIBase          string `json:"api_base"`
		EnterpriseDomain string `json:"enterprise_domain"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
			return
		}
	}
	if err := h.ensureGitHubCopilotProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}
	ts := h.newGitHubCopilotTokenSource(r.Context(), providerName, body.DisplayName, body.APIBase)
	if ts.Exists(r.Context()) {
		if _, err := ts.Token(); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"status": "already_authenticated", "provider_name": providerName})
			return
		}
	}
	flowKey := oauthFlowKey(r.Context(), providerName)
	h.mu.Lock()
	if pending := h.pendingCopilot[flowKey]; pending != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"provider_name": providerName, "verification_uri": pending.login.Device.VerificationURI, "user_code": pending.login.Device.UserCode, "expires_in": pending.login.Device.ExpiresIn, "interval": pending.login.Device.Interval})
		return
	}
	pending, err := oauth.StartLoginGitHubCopilot(body.EnterpriseDomain)
	if err != nil {
		h.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Duration(pending.Device.ExpiresIn+30)*time.Second)
	flow := &pendingGitHubCopilotFlow{login: pending, cancel: cancel, flowKey: flowKey, tenantID: oauthTenantID(r.Context()), userID: store.UserIDFromContext(r.Context()), providerName: providerName, displayName: body.DisplayName, apiBase: body.APIBase, enterpriseDomain: body.EnterpriseDomain}
	h.pendingCopilot[flowKey] = flow
	h.mu.Unlock()
	go h.waitForGitHubCopilot(waitCtx, flow)
	writeJSON(w, http.StatusOK, map[string]any{"provider_name": providerName, "verification_uri": pending.Device.VerificationURI, "user_code": pending.Device.UserCode, "expires_in": pending.Device.ExpiresIn, "interval": pending.Device.Interval})
}

func (h *OAuthHandler) waitForGitHubCopilot(ctx context.Context, flow *pendingGitHubCopilotFlow) {
	githubToken, tokenResp, err := flow.login.Wait(ctx)
	h.mu.Lock()
	if h.pendingCopilot[flow.flowKey] == flow {
		delete(h.pendingCopilot, flow.flowKey)
	}
	h.mu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Warn("copilot.oauth.wait", "provider", flow.providerName, "error", err)
		return
	}
	saveCtx := store.WithTenantID(context.Background(), flow.tenantID)
	if _, err := h.saveAndRegisterGitHubCopilot(saveCtx, flow.providerName, flow.displayName, flow.apiBase, flow.enterpriseDomain, githubToken, tokenResp); err != nil {
		slog.Error("copilot.oauth.save", "provider", flow.providerName, "error", err)
	}
}

func (h *OAuthHandler) saveAndRegisterGitHubCopilot(ctx context.Context, providerName, displayName, apiBase, enterpriseDomain, githubAccessToken string, tokenResp *oauth.GitHubCopilotTokenResponse) (uuid.UUID, error) {
	ts := h.newGitHubCopilotTokenSource(ctx, providerName, displayName, apiBase)
	providerID, err := ts.SaveLoginResult(ctx, githubAccessToken, tokenResp, enterpriseDomain)
	if err != nil {
		return uuid.Nil, err
	}
	if h.providerReg != nil {
		tid := oauthTenantID(ctx)
		resolvedBase := apiBase
		if h.provStore != nil {
			providerCtx := store.WithTenantID(ctx, tid)
			if providerData, err := h.provStore.GetProviderByName(providerCtx, providerName); err == nil {
				if providerData.APIBase != "" {
					resolvedBase = providerData.APIBase
				}
			}
		}
		h.providerReg.RegisterForTenant(tid, providers.NewGitHubCopilotProvider(providerName, ts, resolvedBase, ""))
	}
	if tokenResp != nil {
		for _, model := range []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2-codex", "gpt-5.1-codex"} {
			oauth.EnableGitHubCopilotModel(tokenResp.Token, model, enterpriseDomain)
		}
	}
	return providerID, nil
}

func (h *OAuthHandler) handleGitHubCopilotLogout(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}
	if err := h.ensureGitHubCopilotProviderName(r.Context(), providerName); err != nil {
		writeOAuthProviderConflict(w, err)
		return
	}
	ts := h.newGitHubCopilotTokenSource(r.Context(), providerName, "", "")
	if err := ts.Delete(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if h.providerReg != nil {
		tid := store.TenantIDFromContext(r.Context())
		if tid == uuid.Nil {
			tid = store.MasterTenantID
		}
		h.providerReg.UnregisterForTenant(tid, providerName)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}