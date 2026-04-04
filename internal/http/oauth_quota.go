package http

import (
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *OAuthHandler) handleQuota(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	providerName := oauthProviderName(r)
	if !isValidSlug(providerName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "provider")})
		return
	}

	tenantID := store.TenantIDFromContext(r.Context())
	provider, err := lookupProviderByNameWithMasterFallback(r.Context(), h.provStore, tenantID, providerName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": err.Error(),
			"code":  "provider_not_found",
		})
		return
	}
	if provider.ProviderType != store.ProviderChatGPTOAuth {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": (&oauth.ProviderTypeConflictError{
				ProviderName: providerName,
				ProviderType: provider.ProviderType,
			}).Error(),
			"code": "provider_type_conflict",
		})
		return
	}

	result := oauth.FetchOpenAIQuota(r.Context(), provider, h.newTokenSource(r.Context(), providerName, "", ""))
	writeJSON(w, http.StatusOK, result)
}
