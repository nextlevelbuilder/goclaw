package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VaultSecretSetTool stores a secret value the agent already holds. The value
// arrives as a tool argument, so it has already passed through the model
// context; the agent loop masks it in stored history, traces and events, but
// it cannot be unseen. browser capture_secret and request_secret avoid that.
type VaultSecretSetTool struct{}

func NewVaultSecretSetTool() *VaultSecretSetTool { return &VaultSecretSetTool{} }

func (t *VaultSecretSetTool) Name() string { return "vault_secret_set" }

func (t *VaultSecretSetTool) Description() string {
	return "Store a secret value you already have (for example a token returned by an API) so later tool calls can use it " +
		"through the placeholder {{secret:name}} instead of the raw value. Last resort: the value you pass here is already in " +
		"this conversation. Prefer browser act capture_secret (reads a token from the page straight into the vault) or " +
		"request_secret (the user types it into a secure form); never ask the user to paste a secret into the chat. " +
		"scope=vault saves it pending until a tenant admin approves it and its domains; scope=session keeps it in memory for this " +
		"chat only (at most 20 per chat). Only works when writing secrets is enabled for this agent. The name must be new."
}

func (t *VaultSecretSetTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "New identifier: 1-64 lowercase letters, digits, '-' or '_' (for example app-session-token).",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "The secret value (max 8 KB). For kind login this is the password.",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{store.SecretKindAPIKey, store.SecretKindLogin},
				"description": "api_key for a single token (default); login for a username and password.",
			},
			"username": map[string]any{
				"type":        "string",
				"description": "Username, only for kind login.",
			},
			"domains": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Hosts the secret may be sent to, such as api.example.com or *.example.com.",
			},
			"scope": map[string]any{
				"type":        "string",
				"enum":        []string{secretvault.ScopeSession, "vault"},
				"description": "vault: save it (pending until a tenant admin approves); session: this chat only, never saved.",
			},
		},
		"required": []string{"name", "value", "domains", "scope"},
	}
}

func (t *VaultSecretSetTool) Execute(ctx context.Context, args map[string]any) *Result {
	value, _ := args["value"].(string)
	username, _ := args["username"].(string)
	payload := store.SecretVaultPayload{Username: username, Secret: value}

	rt := secretvault.RuntimeFrom(ctx)
	if rt == nil || rt.Store == nil {
		return ErrorResult("The secret vault is not available in this run.")
	}
	// Known to the redactor before anything below can echo it.
	rt.Redactor.AddPayload(payload)

	name, _ := args["name"].(string)
	kind, _ := args["kind"].(string)
	scope, _ := args["scope"].(string)
	ns := secretvault.NewSecret{Name: name, Kind: kind, Domains: stringList(args["domains"])}

	switch scope {
	case secretvault.ScopeSession:
		if err := rt.SetSessionSecret(ctx, ns, payload); err != nil {
			return writeErrorResult(err)
		}
		return NewResult(fmt.Sprintf("Stored %q for this chat session only (status: %s). Use the placeholder %s; its value is hidden from you.",
			name, secretvault.WriteSession, placeholder(name)))
	case "vault":
		status, err := rt.CreatePending(ctx, ns, payload)
		if err != nil {
			return writeErrorResult(err)
		}
		return NewResult(writeStatusText(name, status))
	default:
		return ErrorResult(`scope must be "session" or "vault"`)
	}
}

// writeStatusText is the whole result of a vault write: a status and the
// placeholder, never the value.
func writeStatusText(name, status string) string {
	if status == secretvault.WriteActive {
		return fmt.Sprintf("Secret %q saved and approved by a tenant admin (status: active). Use the placeholder %s.", name, placeholder(name))
	}
	return fmt.Sprintf("Secret %q saved (status: pending). It cannot be used until a tenant admin approves it in the Secret Vault page; then use the placeholder %s.",
		name, placeholder(name))
}

func writeErrorResult(err error) *Result {
	switch {
	case errors.Is(err, secretvault.ErrWriteDisabled):
		return ErrorResult("Writing secrets is not enabled for this agent. Ask the user to enable it in the Secret Vault page, or use request_secret.")
	case errors.Is(err, secretvault.ErrWriteUnsupported):
		return ErrorResult("Agents cannot save secrets in this edition. Use request_secret instead.")
	}
	return ErrorResult("Secret not stored: " + err.Error())
}

// RegisterVaultWriteTools registers vault_secret_set and vault_grant, except
// in an edition without a surface to approve agent-written secrets (Lite),
// where they would only produce entries nobody can activate.
func RegisterVaultWriteTools(reg *Registry) {
	if !secretvault.AgentWriteSupported() {
		return
	}
	reg.Register(NewVaultSecretSetTool())
	reg.Register(NewVaultGrantTool())
}
