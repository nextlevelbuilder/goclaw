package tools

import (
	"context"
	"log/slog"
	"strings"
)

// staticCredentialEnvKeys are always stripped from fall-through exec env.
// These cover the most common provider / CLI credentials so an agent that
// runs a non-credentialed command cannot exfiltrate host secrets via the
// child process environment.
var staticCredentialEnvKeys = []string{
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"GH_ENTERPRISE_TOKEN",
	"GH_CONFIG_DIR",
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"NPM_TOKEN",
	"DOCKER_PASSWORD",
	"DOCKER_AUTH",
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"GOOGLE_APPLICATION_CREDENTIALS",
	"HUGGINGFACE_TOKEN",
	"HF_TOKEN",
	"GCP_SA_KEY",
	"AZURE_CLIENT_SECRET",
	"CLOUDFLARE_API_TOKEN",
}

// scrubCredentialEnv returns env with any KEY=VALUE pair removed whose key
// matches staticCredentialEnvKeys or any key in dynamicKeys. Comparison is
// case-sensitive per POSIX — env names are case-sensitive.
func scrubCredentialEnv(env []string, dynamicKeys []string) []string {
	if len(env) == 0 {
		return env
	}
	deny := make(map[string]struct{}, len(staticCredentialEnvKeys)+len(dynamicKeys))
	for _, k := range staticCredentialEnvKeys {
		deny[k] = struct{}{}
	}
	for _, k := range dynamicKeys {
		if k == "" {
			continue
		}
		deny[k] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			out = append(out, kv)
			continue
		}
		if _, drop := deny[kv[:i]]; drop {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// credentialEnvKeys returns the union of static deny-list keys and any
// dynamic keys discovered via the secure-cli registry (encrypted_env for
// each enabled binary scoped to the caller's tenant).
//
// No caching: tenant scope comes from ctx, and a shared ExecTool instance
// serves all tenants. Caching would require a per-tenant map with
// invalidation on binary Create/Update/Delete — out of scope here (YAGNI).
// This path runs only on host fall-through exec, which is already the
// slow path. One ListEnabled query is acceptable.
//
// Fails soft: store errors fall back to the static list — env scrubbing
// never blocks exec on a DB hiccup.
func (t *ExecTool) credentialEnvKeys(ctx context.Context) []string {
	if t.secureCLIStore == nil {
		return staticCredentialEnvKeys
	}

	bins, err := t.secureCLIStore.ListEnabled(ctx)
	if err != nil {
		slog.Warn("security.credentialed_env_scrub_list_error", "error", err)
		return staticCredentialEnvKeys
	}

	seen := make(map[string]struct{}, len(staticCredentialEnvKeys))
	merged := make([]string, 0, len(staticCredentialEnvKeys))
	for _, k := range staticCredentialEnvKeys {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		merged = append(merged, k)
	}
	for _, b := range bins {
		for _, k := range extractJSONTopKeys(b.EncryptedEnv) {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			merged = append(merged, k)
		}
	}
	return merged
}

// extractJSONTopKeys returns the top-level string keys of a JSON object
// without unmarshalling values. Tolerant: returns nil on any parse error.
// Kept local to avoid pulling json into env_scrub for a tiny helper.
func extractJSONTopKeys(data []byte) []string {
	s := strings.TrimSpace(string(data))
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return nil
	}
	var keys []string
	inner := s[1 : len(s)-1]
	i := 0
	for i < len(inner) {
		// skip whitespace + commas
		for i < len(inner) && (inner[i] == ' ' || inner[i] == '\n' || inner[i] == '\t' || inner[i] == '\r' || inner[i] == ',') {
			i++
		}
		if i >= len(inner) || inner[i] != '"' {
			return keys
		}
		i++ // consume opening quote
		start := i
		for i < len(inner) && inner[i] != '"' {
			if inner[i] == '\\' && i+1 < len(inner) {
				i += 2
				continue
			}
			i++
		}
		if i > len(inner) {
			return keys
		}
		keys = append(keys, inner[start:i])
		// advance past closing quote + optional spaces + colon + value
		if i < len(inner) {
			i++
		}
		depth := 0
		inStr := false
		for i < len(inner) {
			c := inner[i]
			if inStr {
				if c == '\\' && i+1 < len(inner) {
					i += 2
					continue
				}
				if c == '"' {
					inStr = false
				}
				i++
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
			if c == ',' && depth == 0 {
				i++
				break
			}
			i++
		}
	}
	return keys
}
