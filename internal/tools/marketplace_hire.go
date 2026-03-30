package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/registry"
)

// MarketplaceHireTool downloads and installs agent teams from the GoClaw Hub marketplace.
type MarketplaceHireTool struct {
	client       *registry.Client
	gatewayURL   string // e.g. http://localhost:18790
	gatewayToken string
}

// NewMarketplaceHireTool creates a new marketplace hire tool.
func NewMarketplaceHireTool(client *registry.Client, gatewayURL, gatewayToken string) *MarketplaceHireTool {
	return &MarketplaceHireTool{
		client:       client,
		gatewayURL:   gatewayURL,
		gatewayToken: gatewayToken,
	}
}

func (t *MarketplaceHireTool) Name() string {
	return "marketplace_hire"
}

func (t *MarketplaceHireTool) Description() string {
	return "Hire (download and install) an agent team from GoClaw Hub marketplace. This will download the team and import it into your GoClaw instance. Use this when the user wants to hire, install, or get a specific team."
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

	// Get listing details
	listing, err := t.client.GetListing(ctx, slug)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to get team details: %v", err))
	}

	// Download bundle using wget (Go's HTTP client has TLS stream issues in some envs)
	dlDir := "/app/workspace/marketplace"
	if d := os.Getenv("GOCLAW_WORKSPACE_DIR"); d != "" {
		dlDir = filepath.Join(d, "marketplace")
	}
	if _, err := os.Stat(dlDir); err != nil {
		dlDir = filepath.Join(os.TempDir(), "marketplace")
	}
	os.MkdirAll(dlDir, 0777)
	os.Chmod(dlDir, 0777)

	filename := fmt.Sprintf("goclaw-team-%s.tar.gz", slug)
	bundlePath := filepath.Join(dlDir, filename)
	os.Remove(bundlePath) // clean any stale file

	dlURL := t.client.BaseURL + "/registry/download/" + slug
	cmd := exec.CommandContext(ctx, "wget", "-q", "-O", bundlePath, dlURL,
		"--header=X-Registry-Key: "+t.client.APIKey,
		"--timeout=30")
	if output, err := cmd.CombinedOutput(); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to download team '%s': %v (%s)", listing.Title, err, strings.TrimSpace(string(output))))
	}

	info, err := os.Stat(bundlePath)
	if err != nil || info.Size() == 0 {
		return ErrorResult(fmt.Sprintf("Download produced empty file for '%s'", listing.Title))
	}

	// Import into GoClaw by POSTing to the local team import endpoint
	importResult, err := t.importBundle(bundlePath)
	if err != nil {
		// Download worked but import failed — still tell user about the download
		var result strings.Builder
		result.WriteString(fmt.Sprintf("Downloaded **%s** but import failed: %v\n\n", listing.Title, err))
		result.WriteString(fmt.Sprintf("Bundle saved at: %s (%.1f KB)\n", bundlePath, float64(info.Size())/1024.0))
		result.WriteString("You can import manually via the Teams page.\n")
		return NewResult(result.String())
	}

	// Build success message
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Successfully hired and imported: **%s**\n\n", listing.Title))

	if listing.Tagline != "" {
		result.WriteString(fmt.Sprintf("%s\n\n", listing.Tagline))
	}

	if len(listing.Agents) > 0 {
		result.WriteString("**Team members now active:**\n")
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

	if listing.CreatorName != "" {
		result.WriteString(fmt.Sprintf("By: %s\n", listing.CreatorName))
	}

	if importResult != "" {
		result.WriteString(fmt.Sprintf("\n%s\n", importResult))
	}

	result.WriteString("\nThe team is now available in your Teams page!\n")

	// Clean up bundle file after successful import
	os.Remove(bundlePath)

	return NewResult(result.String())
}

// importBundle POSTs the tar.gz to GoClaw's own /v1/teams/import endpoint.
func (t *MarketplaceHireTool) importBundle(bundlePath string) (string, error) {
	if t.gatewayURL == "" || t.gatewayToken == "" {
		return "", fmt.Errorf("gateway URL or token not configured for auto-import")
	}

	// Read the bundle file
	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		return "", fmt.Errorf("read bundle: %w", err)
	}

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filepath.Base(bundlePath))
	if err != nil {
		return "", fmt.Errorf("create form: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(bundleData)); err != nil {
		return "", fmt.Errorf("write form: %w", err)
	}
	writer.Close()

	// POST to local GoClaw import endpoint
	req, err := http.NewRequest("POST", t.gatewayURL+"/v1/teams/import", &buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.gatewayToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("import request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("import failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}
