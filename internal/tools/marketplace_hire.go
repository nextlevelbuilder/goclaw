package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/registry"
)

// MarketplaceHireTool downloads and installs agent teams from the GoClaw Hub marketplace.
type MarketplaceHireTool struct {
	client *registry.Client
}

// NewMarketplaceHireTool creates a new marketplace hire tool.
func NewMarketplaceHireTool(client *registry.Client) *MarketplaceHireTool {
	return &MarketplaceHireTool{
		client: client,
	}
}

func (t *MarketplaceHireTool) Name() string {
	return "marketplace_hire"
}

func (t *MarketplaceHireTool) Description() string {
	return "Hire (download and install) an agent team from GoClaw Hub marketplace. Use this when the user wants to hire, install, or get a specific team."
}

func (t *MarketplaceHireTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{
				"type":        "string",
				"description": "The team slug from search results (e.g., 'content-squad')",
			},
		},
		"required": []string{"slug"},
	}
}

func (t *MarketplaceHireTool) Execute(ctx context.Context, args map[string]any) *Result {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return ErrorResult("slug is required")
	}

	// Get listing details first
	listing, err := t.client.GetListing(ctx, slug)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to get team details: %v", err))
	}

	// Download using wget — Go's HTTP client has TLS stream issues in some environments
	tempDir := os.TempDir()
	filename := fmt.Sprintf("goclaw-team-%s.tar.gz", slug)
	tempFile := filepath.Join(tempDir, filename)

	dlURL := t.client.BaseURL + "/registry/download/" + slug
	cmd := exec.CommandContext(ctx, "wget", "-q", "-O", tempFile, dlURL,
		"--header=X-Registry-Key: "+t.client.APIKey,
		"--timeout=30")
	if output, err := cmd.CombinedOutput(); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to download team '%s': %v (%s)", listing.Title, err, string(output)))
	}

	// Verify the file
	info, err := os.Stat(tempFile)
	if err != nil || info.Size() == 0 {
		return ErrorResult(fmt.Sprintf("Download produced empty file for '%s'", listing.Title))
	}

	// Build success message
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Successfully hired team: **%s**\n\n", listing.Title))

	if listing.Tagline != "" {
		result.WriteString(fmt.Sprintf("%s\n\n", listing.Tagline))
	}

	if len(listing.Agents) > 0 {
		result.WriteString("**Your new team members:**\n")
		for _, agent := range listing.Agents {
			name := agent.DisplayName
			if agent.Emoji != "" {
				name = agent.Emoji + " " + name
			}
			if agent.Role != "" {
				name += " (" + agent.Role + ")"
			}
			result.WriteString(fmt.Sprintf("- %s\n", name))
		}
		result.WriteString("\n")
	}

	result.WriteString(fmt.Sprintf("Bundle: %s (%.1f KB)\n", tempFile, float64(info.Size())/1024.0))

	if listing.CreatorName != "" {
		result.WriteString(fmt.Sprintf("By: %s\n", listing.CreatorName))
	}

	result.WriteString("\nThe team bundle has been downloaded and is ready for import.\n")

	return NewResult(result.String())
}
