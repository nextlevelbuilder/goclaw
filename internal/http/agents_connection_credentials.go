package http

import (
	"encoding/json"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
)

// The per-agent connected-agent credential handlers that used to live here were
// removed when the tenant-level connection catalogue replaced them; see
// connections.go. What remains is the provider→injection mapping, which the
// tenant-level handlers (and the login flow) share.

// injectForConnectionCfg maps (provider, credType) to the sandbox injection
// descriptor ("env:VAR"). Empty = unsupported combination, which callers turn
// into a 400.
//
// Non-Claude providers resolve through the ADAPTER LAYER rather than a table
// here. That layer already declares each CLI's accepted credential env vars, and
// keeping a second copy meant a provider could be fully supported for delegation
// — binary baked into the sandbox image, argv known — while saving its key still
// failed with "does not support a api_key credential". That was the actual state
// before this change: only claude_code was mapped, so Codex/Aider/Gemini/generic
// connections could be created but never given a credential, i.e. never used.
//
// cfg is the connection's config blob. It matters for provider="generic", whose
// credential env var can ONLY come from config, and for any connection that
// overrides cred_env.
func injectForConnectionCfg(provider, credType string, cfg json.RawMessage) string {
	switch strings.ToLower(provider) {
	case "claude_code", "claude", "claudecode":
		// Kept explicit: Claude Code accepts BOTH kinds and they map to different
		// vars, so deriving from CredEnv order would hand an api_key the OAuth
		// variable (CLAUDE_CODE_OAUTH_TOKEN is first by preference).
		switch credType {
		case "api_key":
			return "env:ANTHROPIC_API_KEY"
		case "oauth":
			return "env:CLAUDE_CODE_OAUTH_TOKEN"
		}
		return ""
	}

	// Only Claude Code has a subscription OAuth login flow today; every other CLI
	// authenticates with a key.
	if credType != "api_key" {
		return ""
	}
	spec, err := cliagent.Resolve(provider, cfg)
	if err != nil || len(spec.CredEnv) == 0 {
		return ""
	}
	return "env:" + spec.CredEnv[0]
}

// injectForConnection is the config-less form, for callers that only know the
// provider (the OAuth login preflight, which is Claude-only anyway).
func injectForConnection(provider, credType string) string {
	return injectForConnectionCfg(provider, credType, nil)
}
