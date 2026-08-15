package acp

import (
	"strings"
	"sync"
	"testing"
)

// --- filterACPEnv tests ---

func TestFilterACPEnv_RemovesSensitivePrefixes(t *testing.T) {
	cases := []struct {
		name    string
		env     []string
		removed []string
		kept    []string
	}{
		{
			name:    "GOCLAW prefix",
			env:     []string{"GOCLAW_TOKEN=abc", "HOME=/root"},
			removed: []string{"GOCLAW_TOKEN=abc"},
			kept:    []string{"HOME=/root"},
		},
		{
			name:    "ANTHROPIC prefix",
			env:     []string{"ANTHROPIC_API_KEY=sk-ant-123", "PATH=/usr/bin"},
			removed: []string{"ANTHROPIC_API_KEY=sk-ant-123"},
			kept:    []string{"PATH=/usr/bin"},
		},
		{
			name:    "OPENAI prefix",
			env:     []string{"OPENAI_API_KEY=sk-123", "TERM=xterm"},
			removed: []string{"OPENAI_API_KEY=sk-123"},
			kept:    []string{"TERM=xterm"},
		},
		{
			name:    "DATABASE prefix",
			env:     []string{"DATABASE_URL=postgres://host/db", "USER=alice"},
			removed: []string{"DATABASE_URL=postgres://host/db"},
			kept:    []string{"USER=alice"},
		},
		{
			name:    "AWS_ prefix",
			env:     []string{"AWS_ACCESS_KEY_ID=AKIA123", "LANG=en_US.UTF-8"},
			removed: []string{"AWS_ACCESS_KEY_ID=AKIA123"},
			kept:    []string{"LANG=en_US.UTF-8"},
		},
		{
			name:    "GITHUB_ prefix",
			env:     []string{"GITHUB_TOKEN=ghp_xxx", "SHELL=/bin/zsh"},
			removed: []string{"GITHUB_TOKEN=ghp_xxx"},
			kept:    []string{"SHELL=/bin/zsh"},
		},
		{
			name:    "SSH_ prefix",
			env:     []string{"SSH_AUTH_SOCK=/tmp/agent.123", "EDITOR=vim"},
			removed: []string{"SSH_AUTH_SOCK=/tmp/agent.123"},
			kept:    []string{"EDITOR=vim"},
		},
		{
			name:    "STRIPE_ prefix",
			env:     []string{"STRIPE_SECRET_KEY=sk_live_abc", "TZ=UTC"},
			removed: []string{"STRIPE_SECRET_KEY=sk_live_abc"},
			kept:    []string{"TZ=UTC"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filtered := filterACPEnv(tc.env)
			filteredSet := make(map[string]bool, len(filtered))
			for _, e := range filtered {
				filteredSet[e] = true
			}
			for _, r := range tc.removed {
				if filteredSet[r] {
					t.Errorf("expected %q to be removed but it was kept", r)
				}
			}
			for _, k := range tc.kept {
				if !filteredSet[k] {
					t.Errorf("expected %q to be kept but it was removed", k)
				}
			}
		})
	}
}

func TestFilterACPEnv_RemovesExactNames(t *testing.T) {
	exactCases := []string{
		"DB_DSN=postgres://localhost/db",
		"PGPASSWORD=secret",
		"PGUSER=admin",
		"PGHOST=db.internal",
		"NPM_TOKEN=npm_abc123",
		"NPM_CONFIG_TOKEN=token",
		"HOMEBREW_GITHUB_API_TOKEN=ghp_abc",
		"CODECOV_TOKEN=xxx",
		"COVERALLS_REPO_TOKEN=yyy",
		"SENTRY_DSN=https://sentry.io/...",
		"SENTRY_AUTH_TOKEN=zzz",
		"SECRET_KEY=supersecret",
		"JWT_SECRET=jwtkey",
	}
	for _, envVar := range exactCases {
		key := strings.SplitN(envVar, "=", 2)[0]
		t.Run(key, func(t *testing.T) {
			filtered := filterACPEnv([]string{envVar, "SAFE_VAR=ok"})
			for _, f := range filtered {
				if strings.HasPrefix(f, key+"=") {
					t.Errorf("expected %q to be removed but it was kept", key)
				}
			}
		})
	}
}

func TestFilterACPEnv_EmptyInput(t *testing.T) {
	result := filterACPEnv(nil)
	if result != nil && len(result) != 0 {
		t.Errorf("expected nil/empty result for nil input, got %v", result)
	}
}

func TestFilterACPEnv_AllSafe(t *testing.T) {
	env := []string{"HOME=/root", "USER=alice", "TERM=xterm-256color", "LANG=en_US.UTF-8"}
	filtered := filterACPEnv(env)
	if len(filtered) != len(env) {
		t.Errorf("expected all %d vars kept, got %d", len(env), len(filtered))
	}
}

func TestFilterACPEnv_CaseInsensitive(t *testing.T) {
	// keys are uppercased before comparison
	env := []string{"goclaw_token=abc", "SAFE=ok"}
	filtered := filterACPEnv(env)
	for _, f := range filtered {
		if strings.HasPrefix(strings.ToUpper(f), "GOCLAW_") {
			t.Errorf("expected lowercase goclaw_token to be stripped, got %q", f)
		}
	}
}

func TestFilterACPEnv_NoEqualsSign(t *testing.T) {
	// Entries with no "=" should be treated as key with empty value; key is full string
	env := []string{"PLAINVAR", "HOME=/home/user"}
	filtered := filterACPEnv(env)
	// PLAINVAR is not sensitive, should be kept
	found := false
	for _, f := range filtered {
		if f == "PLAINVAR" {
			found = true
		}
	}
	if !found {
		t.Error("expected PLAINVAR (no =) to be kept")
	}
}

// --- limitedWriter tests ---

func TestLimitedWriter_CapsToBudget(t *testing.T) {
	lw := &limitedWriter{max: 10}
	n, err := lw.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Write truncates p to remaining capacity before return, so n == min(len(p), max)
	if n != 10 {
		t.Errorf("expected n=10 (capped to max), got %d", n)
	}
	// Only the first 10 bytes are kept
	if got := lw.String(); got != "hello worl" {
		t.Errorf("expected %q, got %q", "hello worl", got)
	}
}

func TestLimitedWriter_StopsAfterFull(t *testing.T) {
	lw := &limitedWriter{max: 5}
	lw.Write([]byte("12345"))
	lw.Write([]byte("extra"))
	if got := lw.String(); got != "12345" {
		t.Errorf("expected %q, got %q", "12345", got)
	}
}

func TestLimitedWriter_ExactMax(t *testing.T) {
	lw := &limitedWriter{max: 4}
	lw.Write([]byte("ab"))
	lw.Write([]byte("cd"))
	if got := lw.String(); got != "abcd" {
		t.Errorf("expected %q, got %q", "abcd", got)
	}
}

func TestLimitedWriter_ZeroMax(t *testing.T) {
	lw := &limitedWriter{max: 0}
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if got := lw.String(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestLimitedWriter_ConcurrentWrites(t *testing.T) {
	lw := &limitedWriter{max: 1000}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			lw.Write([]byte("data"))
		})
	}
	wg.Wait()
	// No panic and output <= max
	s := lw.String()
	if len(s) > 1000 {
		t.Errorf("expected len <= 1000, got %d", len(s))
	}
}

func TestLimitedWriter_ImplementsWriter(t *testing.T) {
	// compile-time check already in helpers.go but verify via interface assignment
	var _ interface{ Write([]byte) (int, error) } = (*limitedWriter)(nil)
}
