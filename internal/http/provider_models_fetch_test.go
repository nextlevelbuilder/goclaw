package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestFetchAnthropicModelsFollowsPagination serves 120 models in pages and checks
// that all of them are returned, not just the first page.
func TestFetchAnthropicModelsFollowsPagination(t *testing.T) {
	const total = 120
	all := make([]map[string]string, total)
	for i := range all {
		all[i] = map[string]string{"id": fmt.Sprintf("claude-model-%03d", i), "display_name": fmt.Sprintf("Model %03d", i)}
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		limit := 20 // Anthropic's default page size
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 1000 {
				limit = n
			} else if err == nil {
				limit = 1000
			}
		}
		start := 0
		if after := r.URL.Query().Get("after_id"); after != "" {
			for i, m := range all {
				if m["id"] == after {
					start = i + 1
				}
			}
		}
		end := start + limit
		if end > total {
			end = total
		}
		page := all[start:end]
		resp := map[string]any{"data": page, "has_more": end < total}
		if len(page) > 0 {
			resp["last_id"] = page[len(page)-1]["id"]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	models, err := fetchAnthropicModels(context.Background(), "test-key", upstream.URL+"/v1")
	if err != nil {
		t.Fatalf("fetchAnthropicModels: %v", err)
	}
	if len(models) != total {
		t.Fatalf("got %d models, want %d", len(models), total)
	}
	if models[total-1].ID != "claude-model-119" {
		t.Errorf("last model = %q, want claude-model-119", models[total-1].ID)
	}
}
