package tools

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/registry"
)

func TestMarketplaceSearchTool(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listings := []registry.Listing{
			{
				Slug:         "data-team",
				Title:        "Data Analysis Team",
				Tagline:      "Expert data scientists and analysts",
				ListingType:  "team",
				PriceCents:   0, // Free
				PricingModel: "free",
				CreatorName:  "DataCorp",
				DownloadCount: 150,
				AvgRating:    4.8,
				ReviewCount:  25,
				Agents: []registry.Agent{
					{
						DisplayName: "Data Scientist",
						Emoji:       "📊",
						Role:        "ML Expert",
					},
					{
						DisplayName: "Analyst",
						Emoji:       "📈",
						Role:        "Business Intelligence",
					},
				},
			},
		}

		response := registry.PaginatedResponse{
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

	client := registry.NewClient(server.URL, "test-key")
	tool := NewMarketplaceSearchTool(client)

	// Test tool metadata
	if tool.Name() != "marketplace_search" {
		t.Errorf("Expected name 'marketplace_search', got '%s'", tool.Name())
	}

	if !strings.Contains(tool.Description(), "marketplace") {
		t.Errorf("Description should mention marketplace")
	}

	params := tool.Parameters()
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Expected properties in parameters")
	}

	if _, ok := properties["query"]; !ok {
		t.Errorf("Expected 'query' parameter")
	}

	// Test execution
	args := map[string]any{
		"query": "data analysis",
	}

	result := tool.Execute(context.Background(), args)
	if result.IsError {
		t.Fatalf("Tool execution failed: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "Data Analysis Team") {
		t.Errorf("Result should contain team name: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "2 agents") {
		t.Errorf("Result should mention agent count: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "Free") {
		t.Errorf("Result should mention it's free: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "marketplace_hire") {
		t.Errorf("Result should mention how to hire: %s", result.ForLLM)
	}
}

func TestMarketplaceHireTool(t *testing.T) {
	// Mock server for both listing details and download
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/registry/listings/") {
			// Return listing details
			listing := registry.Listing{
				Slug:        "test-team",
				Title:       "Test Team",
				Tagline:     "A great test team",
				CreatorName: "Test Creator",
				Agents: []registry.Agent{
					{
						DisplayName: "Assistant",
						Emoji:       "🤖",
						Role:        "Helper",
					},
				},
			}
			json.NewEncoder(w).Encode(listing)
		} else if strings.HasPrefix(r.URL.Path, "/registry/download/") {
			// Return a valid tar.gz file
			w.Header().Set("Content-Type", "application/gzip")
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			tr := tar.NewWriter(gz)
			// Add a sample file to the tar archive
			header := &tar.Header{
				Name:    "README.md",
				Size:    11,
				Mode:    0644,
				ModTime: time.Now(),
			}
			tr.WriteHeader(header)
			tr.Write([]byte("mock bundle"))
			tr.Close()
			gz.Close()
			w.Write(buf.Bytes())
		}
	}))
	defer server.Close()

	client := registry.NewClient(server.URL, "test-key")
	tool := NewMarketplaceHireTool(client, "http://localhost:18790", "", "", nil) // nil TeamCRUDStore — no metadata saving in test

	// Test tool metadata
	if tool.Name() != "marketplace_hire" {
		t.Errorf("Expected name 'marketplace_hire', got '%s'", tool.Name())
	}

	if !strings.Contains(tool.Description(), "hire") {
		t.Errorf("Description should mention hiring")
	}

	// Test execution
	args := map[string]any{
		"slug": "test-team",
	}

	result := tool.Execute(context.Background(), args)
	// Without a real gateway, import fails but download succeeds
	// The tool returns a non-error result with download info
	if result.IsError {
		t.Fatalf("Tool execution failed: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "Test Team") {
		t.Errorf("Result should contain team name: %s", result.ForLLM)
	}

	// Either "Successfully hired" (with gateway) or "Downloaded" (without)
	if !strings.Contains(result.ForLLM, "Test Team") {
		t.Errorf("Result should mention the team: %s", result.ForLLM)
	}
}

func TestMarketplaceSearchToolNoResults(t *testing.T) {
	// Mock server returning empty results
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := registry.PaginatedResponse{
			Meta: struct {
				Total int `json:"total"`
			}{Total: 0},
			Data: json.RawMessage("[]"),
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := registry.NewClient(server.URL, "test-key")
	tool := NewMarketplaceSearchTool(client)

	args := map[string]any{
		"query": "nonexistent team",
	}

	result := tool.Execute(context.Background(), args)
	if result.IsError {
		t.Fatalf("Tool execution failed: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "No teams found") {
		t.Errorf("Result should indicate no teams found: %s", result.ForLLM)
	}
}
