# GoClaw Hub Marketplace Integration

This document describes the GoClaw Hub marketplace integration that allows users to discover, search, and hire pre-built AI agent teams from the GoClaw Hub marketplace.

## Overview

The marketplace integration provides two new tools:

1. **`marketplace_search`** - Search for agent teams in the marketplace
2. **`marketplace_hire`** - Download and hire agent teams

## Configuration

Add the marketplace configuration to your `config.json`:

```json
{
  "tools": {
    "marketplace": {
      "enabled": true,
      "api_base": "https://hub-api.vibery.app/v1",
      "api_key": "your-hub-api-key-here"
    }
  }
}
```

### Configuration Options

- **`enabled`** (boolean): Enable marketplace tools (default: false)
- **`api_base`** (string): Hub API base URL (default: "https://hub-api.vibery.app/v1")
- **`api_key`** (string): Hub API key for authentication

### Environment Variable

You can also set the API key via environment variable:

```bash
export GOCLAW_MARKETPLACE_API_KEY=your-hub-api-key-here
```

The environment variable takes precedence over the config file setting.

## Tools

### marketplace_search

Search the GoClaw Hub marketplace for pre-built AI agent teams.

**Parameters:**
- `query` (required): Search query to find relevant agent teams
- `category` (optional): Filter by category slug
- `type` (optional): Filter by listing type ("team", "agent", "skill")

**Example usage:**
```
Use marketplace_search to find web development teams
```

**Example response:**
```
Found 2 agent teams for 'web development':

1. **Full-Stack Web Team** (fullstack-web-team)
   Complete web development solution with frontend and backend experts
   👥 3 agents: 🎨 Frontend Dev, ⚙️ Backend Dev, 🗄️ Database Expert
   💰 $20.00 (one-time)
   📊 145 downloads • 4.7★ (23 reviews)
   👤 By WebDevCorp (@webdevcorp)

2. **React Specialists** (react-specialists)
   Expert React developers for modern web applications
   👥 2 agents: ⚛️ React Expert, 🎨 UI Designer
   🆓 Free
   📊 89 downloads • 4.9★ (15 reviews)
   👤 By ReactTeam (@reactteam)

💡 To hire a team, use the marketplace_hire tool with the team's slug.
```

### marketplace_hire

Hire (download and install) an agent team from the GoClaw Hub marketplace.

**Parameters:**
- `slug` (required): The team slug from search results

**Example usage:**
```
Use marketplace_hire with slug "fullstack-web-team"
```

**Example response:**
```
✅ Successfully hired team: **Full-Stack Web Team**

Complete web development solution with frontend and backend experts

👥 **Your new team members:**
• Frontend Developer 🎨 (UI/UX Specialist)
• Backend Developer ⚙️ (API Architect)
• Database Expert 🗄️ (Data Specialist)

📦 Bundle downloaded: /tmp/goclaw-team-fullstack-web-team.tar.gz (2.3 KB)
👤 Created by WebDevCorp

🚀 **Next steps:**
• The team bundle has been downloaded and is ready for installation
• Team members will be available in your agent list once imported
• Each agent comes with their specialized skills and knowledge
```

## Registry Client API

The `internal/registry` package provides a Go client for the GoClaw Hub API:

### Client Creation

```go
import "github.com/nextlevelbuilder/goclaw/internal/registry"

client := registry.NewClient("https://hub-api.vibery.app/v1", "your-api-key")
```

### Available Methods

```go
// Search listings
listings, err := client.Search(ctx, "web development", "development", "team")

// Get listing details
listing, err := client.GetListing(ctx, "team-slug")

// Download team bundle
reader, err := client.Download(ctx, "team-slug", "1.0.0")

// Check for updates
updateInfo, err := client.CheckUpdate(ctx, "team-slug", 1)

// List categories
categories, err := client.Categories(ctx)
```

### Data Structures

```go
type Listing struct {
    Slug             string   `json:"slug"`
    Title            string   `json:"title"`
    Tagline          string   `json:"tagline"`
    ListingType      string   `json:"listing_type"`
    PriceCents       int      `json:"price_cents"`
    PricingModel     string   `json:"pricing_model"`
    DownloadCount    int      `json:"download_count"`
    AvgRating        float64  `json:"avg_rating"`
    ReviewCount      int      `json:"review_count"`
    CreatorSlug      string   `json:"creator_slug"`
    CreatorName      string   `json:"creator_display_name"`
    Agents           []Agent  `json:"agents"`
}

type Agent struct {
    AgentKey    string   `json:"agent_key"`
    DisplayName string   `json:"display_name"`
    Emoji       string   `json:"emoji"`
    Role        string   `json:"role"`
    Skills      []string `json:"skills"`
    Model       string   `json:"model_default"`
}
```

## Hub API Endpoints

The integration uses the following GoClaw Hub API endpoints:

- `GET /registry/search` - Search listings
- `GET /registry/listings/{slug}` - Get listing details
- `GET /registry/download/{slug}` - Download team bundle
- `GET /registry/check-update` - Check for updates
- `GET /registry/categories` - List categories

All requests require the `X-Registry-Key` header for authentication.

## Bundle Format

Downloaded team bundles are in tar.gz format, compatible with GoClaw's team export format. The bundles contain:

- Agent configurations
- Skills and knowledge
- Team structure and relationships
- Metadata and documentation

## Future Enhancements

- **Automatic Team Import**: Currently bundles are downloaded to temp files. Future versions will integrate with GoClaw's team import functionality for seamless installation.
- **Update Notifications**: Check for team updates and notify users
- **Local Team Registry**: Track installed teams and their versions
- **Payment Integration**: Support for paid team purchases through the Hub

## Troubleshooting

### Common Issues

1. **API Key Not Found**
   - Ensure `GOCLAW_MARKETPLACE_API_KEY` environment variable is set
   - Or configure `tools.marketplace.api_key` in config.json

2. **Network Errors**
   - Check internet connection
   - Verify `api_base` URL is correct
   - Check firewall settings

3. **Search Returns No Results**
   - Try different search terms
   - Check if the category filter is too restrictive
   - Browse available categories first

### Debug Logging

Enable debug logging to see API requests:

```bash
export GOCLAW_LOG_LEVEL=debug
```

This will log all HTTP requests to the Hub API for troubleshooting.