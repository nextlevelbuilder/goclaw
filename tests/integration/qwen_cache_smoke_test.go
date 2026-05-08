//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestQwenCacheSmoke verifies cache_control:ephemeral is honored by DashScope
// for multiple Qwen model variants. Uses a fresh per-run salt so call 1 always
// creates a new cache entry; call 2 hits it.
//
// Run:
//
//	DASHSCOPE_API_KEY=<key> \
//	  go test -tags=integration ./tests/integration/ -run TestQwenCacheSmoke -v -timeout 5m
//
// Optional:
//
//	DASHSCOPE_API_BASE=...      (default coding-intl)
//	DASHSCOPE_MODELS=a,b,c      (comma-list; defaults to a curated set)
func TestQwenCacheSmoke(t *testing.T) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("set DASHSCOPE_API_KEY")
	}
	apiBase := os.Getenv("DASHSCOPE_API_BASE")
	if apiBase == "" {
		apiBase = "https://coding-intl.dashscope.aliyuncs.com/v1"
	}

	models := []string{"qwen3-coder-plus", "qwen3-max", "qwen-plus", "qwen-turbo"}
	if v := os.Getenv("DASHSCOPE_MODELS"); v != "" {
		models = strings.Split(v, ",")
	}

	// Per-run salt so the cache prefix is unique to this test run; call 1 will
	// always be a cache miss (cache_creation > 0), call 2 a hit.
	salt := time.Now().Format("20060102T150405.000000000")

	type result struct {
		model    string
		ok       bool
		err      string
		create   int
		readHit  int
		prompt2  int
		hitRate  float64
	}
	var results []result

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			r := result{model: model}
			defer func() { results = append(results, r) }()

			p := providers.NewOpenAIProvider("qwen-smoke", apiKey, apiBase, model).
				WithProviderType("bailian")

			// ~6K-token stable prefix, salted per run + per model so cache is fresh.
			stableSys := fmt.Sprintf("You are an expert assistant for run=%s model=%s. ", salt, model) +
				repeat("Provide thorough technically accurate answers about software engineering. Discuss architecture trade-offs performance security observability and maintenance. Cite design patterns and explain why they apply. ", 200) +
				providers.CacheBoundaryMarker +
				"\nDynamic suffix: " + time.Now().Format(time.RFC3339Nano)

			req := providers.ChatRequest{
				Model: model,
				Messages: []providers.Message{
					{Role: "system", Content: stableSys},
					{Role: "user", Content: "Reply with one word: ok"},
				},
				Options: map[string]any{
					providers.OptMaxTokens: 8,
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			// Verify wire format wraps system content.
			body := p.BuildRequestBodyForTest(model, req, false)
			if msgs, ok := body["messages"].([]map[string]any); ok && len(msgs) > 0 {
				if _, isBlocks := msgs[0]["content"].([]map[string]any); !isBlocks {
					t.Fatalf("system content not wrapped as blocks; isDashScope() likely false")
				}
			}

			t.Logf("call 1 (cache create) model=%s...", model)
			resp1, err := p.Chat(ctx, req)
			if err != nil {
				r.err = "call1: " + err.Error()
				t.Fatalf("call 1: %v", err)
			}
			r.create = resp1.Usage.CacheCreationTokens
			t.Logf("  call 1 prompt=%d cached=%d create=%d",
				resp1.Usage.PromptTokens, resp1.Usage.CacheReadTokens, resp1.Usage.CacheCreationTokens)

			time.Sleep(3 * time.Second)

			t.Logf("call 2 (expected hit)...")
			resp2, err := p.Chat(ctx, req)
			if err != nil {
				r.err = "call2: " + err.Error()
				t.Fatalf("call 2: %v", err)
			}
			r.readHit = resp2.Usage.CacheReadTokens
			r.prompt2 = resp2.Usage.PromptTokens
			t.Logf("  call 2 prompt=%d cached=%d create=%d",
				resp2.Usage.PromptTokens, resp2.Usage.CacheReadTokens, resp2.Usage.CacheCreationTokens)

			if r.prompt2 > 0 {
				r.hitRate = float64(r.readHit) / float64(r.prompt2)
			}

			// Per-model assertions: at least one of (create > 0 on call 1) OR
			// (cached > 0 on call 2 with hit rate >= 80%) must hold. Both
			// indicate cache_control plumbing works for this model.
			if r.create == 0 && r.readHit == 0 {
				t.Errorf("model=%s: cache appears UNSUPPORTED (no create, no hit)", model)
				return
			}
			if r.readHit > 0 && r.hitRate < 0.80 {
				t.Errorf("model=%s: hit rate %.1f%% < 80%%", model, r.hitRate*100)
				return
			}
			r.ok = true
		})
	}

	// Final summary table for easy copy/paste into validation report.
	t.Log("\n=== Qwen cache support summary ===")
	t.Logf("%-22s %-6s %-12s %-12s %-12s %-10s", "MODEL", "OK", "CREATE_TOK", "HIT_TOK", "PROMPT2", "HIT_RATE")
	for _, r := range results {
		ok := "PASS"
		if !r.ok {
			ok = "FAIL"
		}
		t.Logf("%-22s %-6s %-12d %-12d %-12d %-9.1f%%",
			r.model, ok, r.create, r.readHit, r.prompt2, r.hitRate*100)
	}
}

func repeat(s string, n int) string {
	var b strings.Builder
	b.Grow(len(s) * n)
	for i := 0; i < n; i++ {
		b.WriteString(s)
	}
	return b.String()
}
