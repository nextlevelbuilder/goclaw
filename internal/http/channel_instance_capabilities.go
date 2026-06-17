package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type channelCapabilityDTO struct {
	Type               string   `json:"type"`
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	DisplayName        string   `json:"display_name,omitempty"`
	Enabled            bool     `json:"enabled"`
	Source             string   `json:"source"`
	ToolAllow          []string `json:"tool_allow,omitempty"`
	ToolDeny           []string `json:"tool_deny,omitempty"`
	CredentialSource   string   `json:"credential_source"`
	HasCredential      bool     `json:"has_credential"`
	ContextGrant       bool     `json:"context_grant_configured"`
	ContextCredentials bool     `json:"context_credentials_configured"`
}

func (h *ChannelInstancesHandler) handleListContextCapabilities(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return
	}
	scopeType := r.PathValue("scopeType")
	scopeKey := r.PathValue("scopeKey")
	if scopeType == "" || scopeKey == "" {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "scopeType and scopeKey"))
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = store.CredentialUserIDFromContext(r.Context())
	} else if userID != store.CredentialUserIDFromContext(r.Context()) && !h.requireContextAdmin(w, r) {
		return
	}

	scope := store.ChannelContextScope{ChannelInstanceID: inst.ID, ChannelInstanceName: inst.Name, ScopeType: scopeType, ScopeKey: scopeKey}
	r = r.WithContext(store.WithChannelContextScope(r.Context(), scope))

	mcpRows, err := h.listChannelMCPRows(r, inst, scope, userID)
	if err != nil {
		slog.Error("channel_instances.capabilities_mcp", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "MCP capabilities"))
		return
	}
	cliRows, err := h.listChannelCLIRows(r, inst, scope, userID)
	if err != nil {
		slog.Error("channel_instances.capabilities_cli", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "CLI capabilities"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"scope_type":   scopeType,
		"scope_key":    scopeKey,
		"capabilities": append(mcpRows, cliRows...),
		"mcp":          mcpRows,
		"secure_cli":   cliRows,
	})
}

func (h *ChannelInstancesHandler) listChannelMCPRows(r *http.Request, inst *store.ChannelInstanceData, scope store.ChannelContextScope, userID string) ([]channelCapabilityDTO, error) {
	if h.mcpStore == nil {
		return []channelCapabilityDTO{}, nil
	}
	accessible, err := h.mcpStore.ListAccessible(r.Context(), inst.AgentID, userID)
	if err != nil {
		return nil, err
	}
	contextGrants := map[uuidString]store.MCPContextGrant{}
	if h.mcpContextStore != nil {
		for _, chainedScope := range store.ChannelContextScopeChainFromContext(r.Context()) {
			grants, err := h.mcpContextStore.ListContextGrants(r.Context(), inst.ID, chainedScope.ScopeType, chainedScope.ScopeKey)
			if err != nil {
				return nil, err
			}
			for _, grant := range grants {
				contextGrants[uuidString(grant.ServerID.String())] = grant
			}
		}
	}
	contextCreds := map[uuidString]store.MCPContextCredentials{}
	if h.mcpContextStore != nil {
		for _, chainedScope := range store.ChannelContextScopeChainFromContext(r.Context()) {
			creds, err := h.mcpContextStore.ListContextCredentials(r.Context(), inst.ID, chainedScope.ScopeType, chainedScope.ScopeKey)
			if err != nil {
				return nil, err
			}
			for _, cred := range creds {
				contextCreds[uuidString(cred.ServerID.String())] = cred
			}
		}
	}
	rows := make([]channelCapabilityDTO, 0, len(accessible))
	seen := map[uuidString]bool{}
	for _, info := range accessible {
		idKey := uuidString(info.Server.ID.String())
		seen[idKey] = true
		credentialSource := "global"
		hasCredential := info.Server.APIKey != "" || len(info.Server.Headers) > 0 || len(info.Server.Env) > 0
		contextCredential := false
		if cred, ok := contextCreds[idKey]; ok {
			credentialSource = cred.ScopeType
			hasCredential = cred.APIKey != "" || len(cred.Headers) > 0 || len(cred.Env) > 0
			contextCredential = true
		}
		if userID != "" {
			if creds, err := h.mcpStore.GetUserCredentials(r.Context(), info.Server.ID, userID); err == nil && creds != nil {
				if creds.APIKey != "" || len(creds.Headers) > 0 || len(creds.Env) > 0 {
					credentialSource = "user"
					hasCredential = true
				}
			}
		}
		source := "agent"
		enabled := info.Server.Enabled
		toolAllow := info.ToolAllow
		toolDeny := info.ToolDeny
		contextGrant := false
		if grant, ok := contextGrants[idKey]; ok {
			source = grant.ScopeType
			enabled = grant.Enabled
			toolAllow = decodeStringList(grant.ToolAllow)
			toolDeny = decodeStringList(grant.ToolDeny)
			contextGrant = true
		}
		rows = append(rows, channelCapabilityDTO{
			Type:               "mcp_server",
			ID:                 info.Server.ID.String(),
			Name:               info.Server.Name,
			DisplayName:        info.Server.DisplayName,
			Enabled:            enabled,
			Source:             source,
			ToolAllow:          toolAllow,
			ToolDeny:           toolDeny,
			CredentialSource:   credentialSource,
			HasCredential:      hasCredential,
			ContextGrant:       contextGrant,
			ContextCredentials: contextCredential,
		})
	}
	for _, grant := range contextGrants {
		idKey := uuidString(grant.ServerID.String())
		if seen[idKey] {
			continue
		}
		server, err := h.mcpStore.GetServer(r.Context(), grant.ServerID)
		if err != nil || server == nil {
			continue
		}
		cred, hasCredRow := contextCreds[idKey]
		credentialSource := grant.ScopeType
		if hasCredRow {
			credentialSource = cred.ScopeType
		}
		rows = append(rows, channelCapabilityDTO{
			Type:               "mcp_server",
			ID:                 server.ID.String(),
			Name:               server.Name,
			DisplayName:        server.DisplayName,
			Enabled:            server.Enabled && grant.Enabled,
			Source:             grant.ScopeType,
			ToolAllow:          decodeStringList(grant.ToolAllow),
			ToolDeny:           decodeStringList(grant.ToolDeny),
			CredentialSource:   credentialSource,
			HasCredential:      hasCredRow && (cred.APIKey != "" || len(cred.Headers) > 0 || len(cred.Env) > 0),
			ContextGrant:       true,
			ContextCredentials: hasCredRow,
		})
	}
	return rows, nil
}

func (h *ChannelInstancesHandler) listChannelCLIRows(r *http.Request, inst *store.ChannelInstanceData, scope store.ChannelContextScope, userID string) ([]channelCapabilityDTO, error) {
	if h.secureCLIStore == nil {
		return []channelCapabilityDTO{}, nil
	}
	binaries, err := h.secureCLIStore.ListForAgent(r.Context(), inst.AgentID)
	if err != nil {
		return nil, err
	}
	contextGrants := map[uuidString]store.SecureCLIContextGrant{}
	if h.cliContextStore != nil {
		for _, chainedScope := range store.ChannelContextScopeChainFromContext(r.Context()) {
			grants, err := h.cliContextStore.ListContextGrants(r.Context(), inst.ID, chainedScope.ScopeType, chainedScope.ScopeKey)
			if err != nil {
				return nil, err
			}
			for _, grant := range grants {
				contextGrants[uuidString(grant.BinaryID.String())] = grant
			}
		}
	}
	contextCreds := map[uuidString]store.SecureCLIContextCredentials{}
	if h.cliContextStore != nil {
		for _, chainedScope := range store.ChannelContextScopeChainFromContext(r.Context()) {
			creds, err := h.cliContextStore.ListContextCredentials(r.Context(), inst.ID, chainedScope.ScopeType, chainedScope.ScopeKey)
			if err != nil {
				return nil, err
			}
			for _, cred := range creds {
				contextCreds[uuidString(cred.BinaryID.String())] = cred
			}
		}
	}
	rows := make([]channelCapabilityDTO, 0, len(binaries))
	seen := map[uuidString]bool{}
	for _, b := range binaries {
		idKey := uuidString(b.ID.String())
		seen[idKey] = true
		source := "agent"
		if b.IsGlobal {
			source = "global"
		}
		credentialSource := source
		hasCredential := len(b.EncryptedEnv) > 0
		contextGrant := false
		if grant, ok := contextGrants[idKey]; ok {
			source = grant.ScopeType
			contextGrant = true
			b.Enabled = b.Enabled && grant.Enabled
		}
		contextCredential := false
		if cred, ok := contextCreds[idKey]; ok {
			credentialSource = cred.ScopeType
			hasCredential = len(cred.EncryptedEnv) > 0
			contextCredential = true
		}
		if userID != "" {
			if creds, err := h.secureCLIStore.GetUserCredentials(r.Context(), b.ID, userID); err == nil && creds != nil && len(creds.EncryptedEnv) > 0 {
				credentialSource = "user"
				hasCredential = true
			}
		}
		rows = append(rows, channelCapabilityDTO{
			Type:               "secure_cli",
			ID:                 b.ID.String(),
			Name:               b.BinaryName,
			DisplayName:        b.Description,
			Enabled:            b.Enabled,
			Source:             source,
			CredentialSource:   credentialSource,
			HasCredential:      hasCredential,
			ContextGrant:       contextGrant,
			ContextCredentials: contextCredential,
		})
	}
	for _, grant := range contextGrants {
		idKey := uuidString(grant.BinaryID.String())
		if seen[idKey] {
			continue
		}
		binary, err := h.secureCLIStore.Get(r.Context(), grant.BinaryID)
		if err != nil || binary == nil {
			continue
		}
		_, hasCredRow := contextCreds[idKey]
		credentialSource := grant.ScopeType
		if cred, ok := contextCreds[idKey]; ok {
			credentialSource = cred.ScopeType
		}
		rows = append(rows, channelCapabilityDTO{
			Type:               "secure_cli",
			ID:                 binary.ID.String(),
			Name:               binary.BinaryName,
			DisplayName:        binary.Description,
			Enabled:            binary.Enabled && grant.Enabled,
			Source:             grant.ScopeType,
			CredentialSource:   credentialSource,
			HasCredential:      hasCredRow,
			ContextGrant:       true,
			ContextCredentials: hasCredRow,
		})
	}
	return rows, nil
}

type uuidString string

func decodeStringList(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	_ = json.Unmarshal(raw, &list)
	return list
}
