package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/registry"
)

// MarketplaceCheckUpdatesTool checks for updates to installed marketplace teams.
type MarketplaceCheckUpdatesTool struct {
	client *registry.Client
	db     *sql.DB
}

// NewMarketplaceCheckUpdatesTool creates a new marketplace update checker tool.
func NewMarketplaceCheckUpdatesTool(client *registry.Client, db *sql.DB) *MarketplaceCheckUpdatesTool {
	return &MarketplaceCheckUpdatesTool{
		client: client,
		db:     db,
	}
}

func (t *MarketplaceCheckUpdatesTool) Name() string {
	return "marketplace_check_updates"
}

func (t *MarketplaceCheckUpdatesTool) Description() string {
	return "Check for updates to marketplace teams installed from GoClaw Hub. Shows which teams have newer versions available."
}

func (t *MarketplaceCheckUpdatesTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

type hubSettings struct {
	HubSlug    string `json:"hub_slug"`
	HubVersion int    `json:"hub_version"`
}

func (t *MarketplaceCheckUpdatesTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.db == nil {
		slog.Warn("marketplace_check_updates: db is nil")
		return ErrorResult("Database not available")
	}

	// Find all teams with hub_slug in settings
	rows, err := t.db.QueryContext(ctx,
		`SELECT id, name, settings FROM agent_teams WHERE settings::text LIKE '%hub_slug%'`)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to query teams: %v", err))
	}
	defer rows.Close()

	type teamInfo struct {
		id       string
		name     string
		slug     string
		version  int
	}

	var teams []teamInfo
	for rows.Next() {
		var id, name string
		var settingsRaw []byte
		if err := rows.Scan(&id, &name, &settingsRaw); err != nil {
			continue
		}
		var hs hubSettings
		if err := json.Unmarshal(settingsRaw, &hs); err != nil || hs.HubSlug == "" {
			continue
		}
		teams = append(teams, teamInfo{id: id, name: name, slug: hs.HubSlug, version: hs.HubVersion})
	}

	slog.Info("marketplace_check_updates", "teams_found", len(teams), "db_nil", t.db == nil)

	if len(teams) == 0 {
		return NewResult("No marketplace teams installed. Use marketplace_search to find teams.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Checking %d marketplace team(s) for updates...\n\n", len(teams)))

	updatesAvailable := 0
	for _, team := range teams {
		info, err := t.client.CheckUpdate(ctx, team.slug, team.version)
		if err != nil {
			sb.WriteString(fmt.Sprintf("- **%s** (%s): Failed to check — %v\n", team.name, team.slug, err))
			continue
		}

		if info.HasUpdate {
			updatesAvailable++
			sb.WriteString(fmt.Sprintf("- **%s** (%s): Update available! v%d → v%d\n", team.name, team.slug, team.version, info.LatestVersion))
		} else {
			sb.WriteString(fmt.Sprintf("- **%s** (%s): Up to date (v%d)\n", team.name, team.slug, team.version))
		}
	}

	if updatesAvailable > 0 {
		sb.WriteString(fmt.Sprintf("\n%d update(s) available. To update a team, say \"hire {slug} from marketplace\" again.\n", updatesAvailable))
	} else {
		sb.WriteString("\nAll teams are up to date.\n")
	}

	return NewResult(sb.String())
}
