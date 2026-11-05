package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
)

// VaultGrantTool lets another agent of the same tenant use a secret this agent
// created. Every grant needs a live yes from the user in the Web UI.
type VaultGrantTool struct{}

func NewVaultGrantTool() *VaultGrantTool { return &VaultGrantTool{} }

func (t *VaultGrantTool) Name() string { return "vault_grant" }

func (t *VaultGrantTool) Description() string {
	return "Let another agent use a vault secret that you created (with browser capture_secret or vault_secret_set). " +
		"The user must approve each grant in the Web UI; without an open Web UI the grant is refused. " +
		"Only works when writing secrets is enabled for this agent."
}

func (t *VaultGrantTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"secret": map[string]any{
				"type":        "string",
				"description": "Name of the secret (plain name, not a placeholder).",
			},
			"agent_key": map[string]any{
				"type":        "string",
				"description": "Key of the agent that should be allowed to use it.",
			},
		},
		"required": []string{"secret", "agent_key"},
	}
}

func (t *VaultGrantTool) Execute(ctx context.Context, args map[string]any) *Result {
	rt := secretvault.RuntimeFrom(ctx)
	if rt == nil || rt.Store == nil {
		return ErrorResult("The secret vault is not available in this run.")
	}
	name, _ := args["secret"].(string)
	agentKey, _ := args["agent_key"].(string)
	name, agentKey = strings.TrimSpace(name), strings.TrimSpace(agentKey)
	if name == "" || agentKey == "" {
		return ErrorResult("secret and agent_key are required.")
	}
	if err := rt.GrantToAgent(ctx, name, agentKey); err != nil {
		return writeErrorResult(err)
	}
	return NewResult(fmt.Sprintf("Agent %q may now use %s (approved by the user).", agentKey, placeholder(name)))
}
