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

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		requests++
		// The mock caps every page at 20, whatever limit was requested, so the
		// 120 models can only be returned by following after_id/has_more.
		limit := 20
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n < limit {
			limit = n
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
	if requests != total/20 {
		t.Errorf("upstream saw %d requests, want %d", requests, total/20)
	}
}

// TestFetchAnthropicModelsKeepsEarlierPagesOnLaterFailure checks that a failing
// later page returns what was already collected, while a failing first page is an error.
func TestFetchAnthropicModelsKeepsEarlierPagesOnLaterFailure(t *testing.T) {
	serve := func(failFrom int) *httptest.Server {
		calls := 0
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls >= failFrom {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     []map[string]string{{"id": "claude-a", "display_name": "A"}},
				"has_more": true,
				"last_id":  "claude-a",
			})
		}))
	}

	later := serve(2)
	defer later.Close()
	models, err := fetchAnthropicModels(context.Background(), "k", later.URL+"/v1")
	if err != nil || len(models) != 1 || models[0].ID != "claude-a" {
		t.Fatalf("later-page failure: got %v, %v; want the first page and no error", models, err)
	}

	first := serve(1)
	defer first.Close()
	if _, err := fetchAnthropicModels(context.Background(), "k", first.URL+"/v1"); err == nil {
		t.Fatal("first-page failure: want an error")
	}
}
