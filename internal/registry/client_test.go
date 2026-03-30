package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Search(t *testing.T) {
	// Mock server that returns sample marketplace listings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/registry/search" {
			t.Errorf("Expected path /registry/search, got %s", r.URL.Path)
		}

		if r.Header.Get("X-Registry-Key") == "" {
			t.Errorf("Expected X-Registry-Key header")
		}

		query := r.URL.Query().Get("q")
		if query == "" {
			t.Errorf("Expected query parameter")
		}

		// Mock response
		listings := []Listing{
			{
				Slug:         "web-dev-team",
				Title:        "Web Development Team",
				Tagline:      "Full-stack web development specialists",
				ListingType:  "team",
				PriceCents:   2000,
				PricingModel: "one-time",
				CreatorName:  "GoClaw Hub",
				Agents: []Agent{
					{
						AgentKey:    "frontend-dev",
						DisplayName: "Frontend Developer",
						Emoji:       "🎨",
						Role:        "UI/UX Specialist",
						Skills:      []string{"React", "CSS", "TypeScript"},
						Model:       "sonnet",
					},
					{
						AgentKey:    "backend-dev",
						DisplayName: "Backend Developer",
						Emoji:       "⚙️",
						Role:        "API Architect",
						Skills:      []string{"Node.js", "PostgreSQL", "Docker"},
						Model:       "sonnet",
					},
				},
			},
		}

		response := PaginatedResponse{
			Meta: struct {
				Total int `json:"total"`
			}{Total: 1},
		}

		data, _ := json.Marshal(listings)
		response.Data = json.RawMessage(data)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	listings, err := client.Search(context.Background(), "web development", "", "")

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(listings) != 1 {
		t.Fatalf("Expected 1 listing, got %d", len(listings))
	}

	listing := listings[0]
	if listing.Slug != "web-dev-team" {
		t.Errorf("Expected slug 'web-dev-team', got '%s'", listing.Slug)
	}

	if listing.Title != "Web Development Team" {
		t.Errorf("Expected title 'Web Development Team', got '%s'", listing.Title)
	}

	if len(listing.Agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(listing.Agents))
	}

	if listing.AgentCount() != 2 {
		t.Errorf("Expected AgentCount() to return 2, got %d", listing.AgentCount())
	}
}

func TestClient_Download(t *testing.T) {
	expectedContent := "mock team bundle data"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/registry/download/") {
			t.Errorf("Expected path to start with /registry/download/, got %s", r.URL.Path)
		}

		if r.Header.Get("X-Registry-Key") == "" {
			t.Errorf("Expected X-Registry-Key header")
		}

		w.Header().Set("Content-Type", "application/gzip")
		w.Write([]byte(expectedContent))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	reader, err := client.Download(context.Background(), "test-team", "")

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	defer reader.Close()

	buf := make([]byte, len(expectedContent))
	n, err := reader.Read(buf)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("Read failed: %v", err)
	}

	if n == 0 {
		t.Fatalf("No data read from response")
	}

	if string(buf[:n]) != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, string(buf[:n]))
	}
}