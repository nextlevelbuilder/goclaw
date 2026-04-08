package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/registry"
)

// MarketplaceSearchTool searches the GoClaw Hub marketplace for pre-built AI agent teams.
type MarketplaceSearchTool struct {
	client *registry.Client
}

// NewMarketplaceSearchTool creates a new marketplace search tool.
func NewMarketplaceSearchTool(client *registry.Client) *MarketplaceSearchTool {
	return &MarketplaceSearchTool{
		client: client,
	}
}

func (t *MarketplaceSearchTool) Name() string {
	return "marketplace_search"
}

func (t *MarketplaceSearchTool) Description() string {
	return "Search GoClaw Hub marketplace for pre-built AI agent teams. Use this when the user wants to find, browse, or discover agent teams they can hire."
}

func (t *MarketplaceSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query to find relevant agent teams (e.g., 'web development', 'data analysis', 'content creation')",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Filter by category slug (optional)",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Filter by listing type: 'team', 'agent', 'skill' (optional)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *MarketplaceSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query is required")
	}

	category, _ := args["category"].(string)
	listingType, _ := args["type"].(string)

	listings, err := t.client.Search(ctx, query, category, listingType)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Search failed: %v", err))
	}

	if len(listings) == 0 {
		return NewResult(fmt.Sprintf("No teams found for query: %s", query))
	}

	slog.Info("marketplace: search", "query", query, "results", len(listings))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d agent team%s for '%s':\n\n", len(listings), plural(len(listings)), query))

	for i, listing := range listings {
		sb.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, listing.Title, listing.Slug))
		if listing.Tagline != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", listing.Tagline))
		}

		// Team composition
		agentCount := listing.AgentCount()
		if agentCount > 0 {
			sb.WriteString(fmt.Sprintf("   👥 %d agent%s", agentCount, plural(agentCount)))
			if agentCount <= 3 {
				// Show agent names for small teams
				var names []string
				for _, agent := range listing.Agents {
					name := agent.DisplayName
					if agent.Emoji != "" {
						name = agent.Emoji + " " + name
					}
					names = append(names, name)
				}
				sb.WriteString(fmt.Sprintf(": %s", strings.Join(names, ", ")))
			}
			sb.WriteString("\n")
		}

		// Pricing
		if listing.PriceCents > 0 {
			price := float64(listing.PriceCents) / 100.0
			sb.WriteString(fmt.Sprintf("   💰 $%.2f", price))
			if listing.PricingModel != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", listing.PricingModel))
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString("   🆓 Free\n")
		}

		// Stats
		if listing.DownloadCount > 0 || listing.ReviewCount > 0 {
			sb.WriteString("   📊 ")
			if listing.DownloadCount > 0 {
				sb.WriteString(fmt.Sprintf("%d download%s", listing.DownloadCount, plural(listing.DownloadCount)))
			}
			if listing.ReviewCount > 0 {
				if listing.DownloadCount > 0 {
					sb.WriteString(" • ")
				}
				sb.WriteString(fmt.Sprintf("%.1f★ (%d review%s)", listing.AvgRating, listing.ReviewCount, plural(listing.ReviewCount)))
			}
			sb.WriteString("\n")
		}

		// Creator
		if listing.CreatorName != "" {
			sb.WriteString(fmt.Sprintf("   👤 By %s", listing.CreatorName))
			if listing.CreatorSlug != "" && listing.CreatorSlug != listing.CreatorName {
				sb.WriteString(fmt.Sprintf(" (@%s)", listing.CreatorSlug))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}

	sb.WriteString("💡 To hire a team, use the marketplace_hire tool with the team's slug.\n")

	return NewResult(sb.String())
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
