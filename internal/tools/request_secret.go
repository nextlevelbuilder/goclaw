package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// RequestSecretTool asks the user, through the Web UI Secure Form, for a
// credential the agent needs. The value goes from the user's browser straight
// to the vault (or session memory); the tool only ever returns a status and
// the placeholder to use.
type RequestSecretTool struct{}

func NewRequestSecretTool() *RequestSecretTool { return &RequestSecretTool{} }

func (t *RequestSecretTool) Name() string { return "request_secret" }

func (t *RequestSecretTool) Description() string {
	return "Ask the user for a credential (API key, or username and password) through a secure form that you cannot read. " +
		"On success you receive only a placeholder such as {{secret:name}}; put it where the credential belongs in a tool call " +
		"and the system substitutes the real value. Never ask the user to paste secrets into the chat. " +
		"Works only in the Web UI. State which domains the credential is for."
}

func (t *RequestSecretTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Short identifier: 1-64 lowercase letters, digits, '-' or '_' (for example github-token).",
			},
			"kind": map[string]any{
				"type":        "string",
				"enum":        []string{store.SecretKindAPIKey, store.SecretKindLogin},
				"description": "api_key for a single token; login for a username and password.",
			},
			"domains": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Hosts the credential may be sent to, such as api.github.com or *.example.com.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why you need it, shown to the user.",
			},
			"hints": map[string]any{
				"type":        "string",
				"description": "Optional note about where to find the credential, shown to the user.",
			},
		},
		"required": []string{"name", "kind", "domains", "reason"},
	}
}

func (t *RequestSecretTool) Execute(ctx context.Context, args map[string]any) *Result {
	rt := secretvault.RuntimeFrom(ctx)
	if rt == nil || rt.Store == nil || rt.Forms == nil {
		return ErrorResult("The secret vault is not available in this run.")
	}
	if ToolChannelFromCtx(ctx) != "ws" {
		return ErrorResult("Secure forms only work in the Web UI. Ask the user to continue this task from the Web UI.")
	}

	name, _ := args["name"].(string)
	kind, _ := args["kind"].(string)
	reason, _ := args["reason"].(string)
	hints, _ := args["hints"].(string)
	domains := stringList(args["domains"])

	domains, err := store.NormalizeSecretDomains(domains)
	if err == nil {
		err = store.ValidateSecretVaultEntry(&store.SecretVaultEntry{Name: name, Kind: kind, AllowedDomains: domains, Hints: hints})
	}
	if err != nil {
		return ErrorResult("Invalid request: " + err.Error())
	}

	if available(ctx, rt, name) {
		return NewResult(fmt.Sprintf("Secret %q is already available. Use the placeholder %s.", name, placeholder(name)))
	}

	res, err := rt.Forms.RequestForm(ctx, secretvault.FormSpec{
		Kind:       kind,
		Name:       name,
		Domains:    domains,
		Reason:     reason,
		Hints:      hints,
		AgentID:    rt.AgentID,
		AgentKey:   ToolAgentKeyFromCtx(ctx),
		SessionKey: rt.SessionKey,
		Store:      rt.Store,
		OnSecret: func(p store.SecretVaultPayload) {
			rt.Redactor.AddPayload(p)
			rt.MarkApproved(name)
		},
	})
	switch {
	case errors.Is(err, secretvault.ErrNoUI):
		return ErrorResult("No Web UI session of the user is open, so the secure form cannot be shown. Ask the user to open the Web UI.")
	case err != nil:
		return ErrorResult("The secure form could not be opened.")
	}

	switch res.Status {
	case secretvault.StatusSubmitted:
		return NewResult(fmt.Sprintf("The user provided secret %q. Use the placeholder %s where the credential belongs; its value is hidden from you.", name, placeholder(name)))
	case secretvault.StatusTimeout:
		return ErrorResult(fmt.Sprintf("The user did not answer the request for %q in time.", name))
	default:
		return ErrorResult(fmt.Sprintf("The user declined to provide %q.", name))
	}
}

func placeholder(name string) string { return "{{secret:" + name + "}}" }

// available reports whether name is already usable by this run: granted in the
// vault or provided earlier for this session.
func available(ctx context.Context, rt *secretvault.Runtime, name string) bool {
	if metas, err := rt.Store.ListGrantedMeta(ctx, rt.AgentID); err == nil {
		if slices.ContainsFunc(metas, func(e store.SecretVaultEntry) bool { return e.Name == name }) {
			return true
		}
	}
	if rt.SessionKey == "" {
		return false
	}
	_, _, ok := rt.Forms.Sessions.Get(store.TenantIDFromContext(ctx), rt.SessionKey, name)
	return ok
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
