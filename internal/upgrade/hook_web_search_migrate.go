// Package upgrade provides data migration hooks for Go-level data transformations.
// This file contains the hook that migrates legacy inline web_search API keys
// from two sources into config_secrets:
//
//  1. Inline keys in config.json5 (tools.web.{exa,tavily,brave}.api_key)
//     → config_secrets under MasterTenantID.
//
//  2. Inline api_key fields embedded in builtin_tool_tenant_configs.settings JSON
//     for tool_name='web_search', per tenant
//     → config_secrets under each tenant, stripping the inline field from settings.
//
// Hook name: 055_web_search_legacy_keys_to_config_secrets
// Idempotent: tracked in data_migrations table; re-runs are no-ops.
// PG-only: SQLite/desktop edition never had inline legacy config path.
package upgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/titanous/json5"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const webSearchMigrateHookName = "055_web_search_legacy_keys_to_config_secrets"

// migrateWebSearchInlineKeys is the data hook body.
// It reads config.json5 as raw JSON5 (map[string]any) to avoid any dependency
// on config.Config struct fields deleted in phase 01.
func migrateWebSearchInlineKeys(ctx context.Context, db *sql.DB) error {
	encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	if encKey == "" {
		slog.Warn("web_search migrate: no GOCLAW_ENCRYPTION_KEY, skipping key migration")
		return nil
	}

	// Part A: migrate inline keys from config.json5 → config_secrets (MasterTenantID)
	migrateConfigJSON5Keys(ctx, db, encKey)

	// Part B: migrate inline api_key from builtin_tool_tenant_configs.settings blobs
	migrateSettingsBlobKeys(ctx, db, encKey)

	return nil
}

// migrateConfigJSON5Keys extracts tools.web.{exa,tavily,brave}.api_key from
// config.json5 (raw JSON5 parse) and inserts into config_secrets under MasterTenantID.
// Skips if the row already exists (do not overwrite user-saved secrets).
func migrateConfigJSON5Keys(ctx context.Context, db *sql.DB, encKey string) {
	cfgPath := os.Getenv("GOCLAW_CONFIG")
	if cfgPath == "" {
		return // no config path — nothing to migrate
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		slog.Warn("web_search migrate: cannot read config file, skipping config.json5 path", "error", err)
		return
	}

	// Parse as raw map to avoid dependency on deleted config.Tools.Web struct.
	var raw map[string]any
	if err := json5.Unmarshal(data, &raw); err != nil {
		slog.Warn("web_search migrate: cannot parse config file, skipping config.json5 path", "error", err)
		return
	}

	// Walk: raw["tools"]["web"][name]["api_key"]
	tools, _ := raw["tools"].(map[string]any)
	if tools == nil {
		return
	}
	web, _ := tools["web"].(map[string]any)
	if web == nil {
		return
	}

	for _, name := range []string{"exa", "tavily", "brave"} {
		provider, _ := web[name].(map[string]any)
		if provider == nil {
			continue
		}
		apiKey, _ := provider["api_key"].(string)
		if apiKey == "" {
			continue
		}

		secretKey := "tools.web." + name + ".api_key"

		// Check if secret already exists for MasterTenantID — do not overwrite.
		var one int
		checkErr := db.QueryRowContext(ctx,
			`SELECT 1 FROM config_secrets WHERE key=$1 AND tenant_id=$2 LIMIT 1`,
			secretKey, store.MasterTenantID).Scan(&one)
		if checkErr == nil {
			continue // row exists — skip
		}
		if checkErr != sql.ErrNoRows {
			slog.Warn("web_search migrate: check failed for config.json5 key",
				"key", secretKey, "error", checkErr)
			continue
		}

		enc, encErr := crypto.Encrypt(apiKey, encKey)
		if encErr != nil {
			slog.Warn("web_search migrate: encrypt failed for config.json5 key",
				"key", secretKey, "error", encErr)
			continue
		}

		_, insErr := db.ExecContext(ctx,
			`INSERT INTO config_secrets (key, value, updated_at, tenant_id)
			 VALUES ($1, $2, NOW(), $3)
			 ON CONFLICT (key, tenant_id) DO NOTHING`,
			secretKey, []byte(enc), store.MasterTenantID)
		if insErr != nil {
			slog.Warn("web_search migrate: insert failed for config.json5 key",
				"key", secretKey, "error", insErr)
			continue
		}

		slog.Warn("web_search: migrating inline key from config.json5 to config_secrets; future edits should use the Tools UI",
			"provider", name)
	}
}

// settingsRow holds parsed data from builtin_tool_tenant_configs for a single tenant.
type settingsRow struct {
	tenantID uuid.UUID
	settings map[string]any
	changed  bool
}

// migrateSettingsBlobKeys scans builtin_tool_tenant_configs for web_search rows
// that still have inline api_key fields inside the settings JSON blob.
// For each provider key found: inserts into config_secrets, then strips the
// api_key field from the settings blob. Idempotent — if api_key already absent,
// loop simply skips.
func migrateSettingsBlobKeys(ctx context.Context, db *sql.DB, encKey string) {
	rows, err := db.QueryContext(ctx,
		`SELECT tenant_id, settings FROM builtin_tool_tenant_configs WHERE tool_name = $1`,
		"web_search")
	if err != nil {
		slog.Warn("web_search migrate: legacy settings scan failed", "error", err)
		return
	}
	defer rows.Close()

	var toUpdate []settingsRow

	for rows.Next() {
		var tenantID uuid.UUID
		var settingsJSON []byte
		if err := rows.Scan(&tenantID, &settingsJSON); err != nil {
			continue
		}

		var settings map[string]any
		if err := json.Unmarshal(settingsJSON, &settings); err != nil {
			continue
		}

		entry := settingsRow{tenantID: tenantID, settings: settings}

		for _, name := range []string{"exa", "tavily", "brave"} {
			sub, ok := settings[name].(map[string]any)
			if !ok {
				continue
			}
			keyVal, _ := sub["api_key"].(string)
			if keyVal == "" || keyVal == "***" {
				continue
			}

			secretKey := "tools.web." + name + ".api_key"

			// Check if secret already exists for this tenant.
			var one int
			checkErr := db.QueryRowContext(ctx,
				`SELECT 1 FROM config_secrets WHERE key=$1 AND tenant_id=$2 LIMIT 1`,
				secretKey, tenantID).Scan(&one)
			if checkErr == nil {
				// Existing secret wins — just strip the inline field from settings.
				delete(sub, "api_key")
				entry.changed = true
				continue
			}

			// No secret yet — encrypt and insert.
			enc, encErr := crypto.Encrypt(keyVal, encKey)
			if encErr != nil {
				slog.Warn("web_search migrate: encrypt failed for settings-blob key",
					"key", secretKey, "tenant", tenantID, "error", encErr)
				continue
			}

			_, insErr := db.ExecContext(ctx,
				`INSERT INTO config_secrets (key, value, updated_at, tenant_id)
				 VALUES ($1, $2, NOW(), $3)
				 ON CONFLICT (key, tenant_id) DO NOTHING`,
				secretKey, []byte(enc), tenantID)
			if insErr != nil {
				slog.Warn("web_search migrate: insert failed for settings-blob key",
					"key", secretKey, "tenant", tenantID, "error", insErr)
				continue
			}

			slog.Warn("legacy inline web_search key migrated from builtin_tool_tenant_configs.settings",
				"key", secretKey, "tenant", tenantID)

			delete(sub, "api_key")
			entry.changed = true
		}

		if entry.changed {
			toUpdate = append(toUpdate, entry)
		}
	}
	rows.Close()

	// Write back cleaned settings blobs.
	for _, e := range toUpdate {
		newJSON, err := json.Marshal(e.settings)
		if err != nil {
			continue
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE builtin_tool_tenant_configs SET settings=$1, updated_at=NOW()
			 WHERE tenant_id=$2 AND tool_name=$3`,
			newJSON, e.tenantID, "web_search"); err != nil {
			slog.Warn("web_search migrate: settings rewrite failed",
				"tenant", e.tenantID, "error", err)
		}
	}
}
