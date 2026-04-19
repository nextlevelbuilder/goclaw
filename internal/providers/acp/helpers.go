package acp

import (
	"context"
	"io"
	"strings"
	"sync"
)

// contextKey is the unexported type for context values set by this package.
type contextKey string

const goclawSessionCtxKey contextKey = "goclaw_session"

// WithGoclawSession attaches the goclaw conversation session key to ctx so
// that ACP-level logs can correlate the ACP session ID with the goclaw session.
func WithGoclawSession(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, goclawSessionCtxKey, key)
}

// goclawSessionFromCtx extracts the goclaw session key injected by WithGoclawSession.
// Returns "" if not set.
func goclawSessionFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(goclawSessionCtxKey).(string)
	return v
}

// sensitiveEnvPrefixes lists env var prefixes stripped from ACP subprocesses.
var sensitiveEnvPrefixes = []string{
	"GOCLAW", "CLAUDE", "ANTHROPIC", "OPENAI",
	"DATABASE", "POSTGRES", "MYSQL", "REDIS", "MONGO",
	"AWS_", "AZURE_", "GOOGLE_", "GCP_",
	"GITHUB_", "GH_", "GITLAB_", "BITBUCKET_",
	"DOCKER_", "REGISTRY_",
	"STRIPE_", "TWILIO_", "SENDGRID_",
	"SSH_", "GPG_",
}

// allowedEnvExact lists env vars explicitly allowed even if they match sensitive prefixes.
// These are required for Google/GCP authentication in ACP subprocesses (e.g., Gemini).
var allowedEnvExact = map[string]bool{
	"GOOGLE_API_KEY":                 true,
	"GOOGLE_APPLICATION_CREDENTIALS": true,
	"GOOGLE_CLOUD_PROJECT":           true,
	"GCP_PROJECT":                    true,
}

// sensitiveEnvExact lists exact env var names stripped from ACP subprocesses.
var sensitiveEnvExact = map[string]bool{
	"DB_DSN": true, "PGPASSWORD": true, "PGUSER": true, "PGHOST": true,
	"NPM_TOKEN": true, "NPM_CONFIG_TOKEN": true,
	"HOMEBREW_GITHUB_API_TOKEN": true,
	"CODECOV_TOKEN": true, "COVERALLS_REPO_TOKEN": true,
	"SENTRY_DSN": true, "SENTRY_AUTH_TOKEN": true,
	"SECRET_KEY": true, "JWT_SECRET": true,
}

// filterACPEnv strips sensitive env vars from the subprocess environment.
// Explicitly allowed vars (allowedEnvExact) pass through even if they match sensitive prefixes.
func filterACPEnv(environ []string) []string {
	var filtered []string
	for _, e := range environ {
		key, _, _ := strings.Cut(e, "=")
		upper := strings.ToUpper(key)
		if allowedEnvExact[upper] {
			filtered = append(filtered, e)
			continue
		}
		if sensitiveEnvExact[upper] {
			continue
		}
		skip := false
		for _, prefix := range sensitiveEnvPrefixes {
			if strings.HasPrefix(upper, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// limitedWriter captures up to max bytes of output for diagnostics.
type limitedWriter struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.max - len(w.buf)
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		w.buf = append(w.buf, p...)
	}
	return len(p), nil
}

func (w *limitedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// Ensure limitedWriter satisfies io.Writer.
var _ io.Writer = (*limitedWriter)(nil)
