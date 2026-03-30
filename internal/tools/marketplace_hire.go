package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/registry"
)

// MarketplaceHireTool downloads and installs agent teams from the GoClaw Hub marketplace.
type MarketplaceHireTool struct {
	client *registry.Client
	// TODO: importFn will be wired when we have access to the team store
	// importFn func(ctx context.Context, reader io.Reader) error
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
				"description": "The team slug from search results (e.g., 'web-dev-experts', 'data-scientists')",
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

	// First, get the listing details to show what we're downloading
	listing, err := t.client.GetListing(ctx, slug)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to get team details: %v", err))
	}

	// Download the team bundle
	goclawVersion := "" // TODO: Could detect version from build info
	reader, err := t.client.Download(ctx, slug, goclawVersion)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to download team '%s': %v", listing.Title, err))
	}
	defer reader.Close()

	// Save to a temporary file for now
	// TODO: In the future, this will call the team import function directly
	tempDir := os.TempDir()
	filename := fmt.Sprintf("goclaw-team-%s.tar.gz", slug)
	tempFile := filepath.Join(tempDir, filename)

	outFile, err := os.Create(tempFile)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to create temp file: %v", err))
	}
	defer outFile.Close()

	bytesWritten, err := io.Copy(outFile, reader)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to save team bundle: %v", err))
	}

	// Build success message
	var result strings.Builder
	result.WriteString(fmt.Sprintf("✅ Successfully hired team: **%s**\n\n", listing.Title))

	if listing.Tagline != "" {
		result.WriteString(fmt.Sprintf("%s\n\n", listing.Tagline))
	}

	// Show team composition
	if len(listing.Agents) > 0 {
		result.WriteString("👥 **Your new team members:**\n")
		for _, agent := range listing.Agents {
			result.WriteString(fmt.Sprintf("• %s", agent.DisplayName))
			if agent.Emoji != "" {
				result.WriteString(fmt.Sprintf(" %s", agent.Emoji))
			}
			if agent.Role != "" {
				result.WriteString(fmt.Sprintf(" (%s)", agent.Role))
			}
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	// Show download info
	result.WriteString(fmt.Sprintf("📦 Bundle downloaded: %s (%.1f KB)\n", tempFile, float64(bytesWritten)/1024.0))

	// Creator acknowledgment
	if listing.CreatorName != "" {
		result.WriteString(fmt.Sprintf("👤 Created by %s\n", listing.CreatorName))
	}

	result.WriteString("\n🚀 **Next steps:**\n")
	result.WriteString("• The team bundle has been downloaded and is ready for installation\n")
	result.WriteString("• Team members will be available in your agent list once imported\n")
	result.WriteString("• Each agent comes with their specialized skills and knowledge\n")

	// TODO: When team import is wired, replace the above with:
	// if err := t.importFn(ctx, reader); err != nil {
	//     return ErrorResult(fmt.Sprintf("Failed to import team: %v", err))
	// }
	// result.WriteString("• Team members are now active and ready to work!\n")

	return NewResult(result.String())
}