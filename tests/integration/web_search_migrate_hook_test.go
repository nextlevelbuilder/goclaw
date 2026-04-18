//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
)

// TestWebSearchMigrateHook_ConfigJSON5Path tests migration of inline keys
// from config.json5 to config_secrets (sub-test A).
func TestWebSearchMigrateHook_ConfigJSON5Path(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Create a temp config.json5 with inline Brave key
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json5"
	configContent := []byte(`{
	  "tools": {
	    "web": {
	      "brave": {
	        "api_key": "brave-key-from-config-json5"
	      }
	    }
  }
}`)

	if err := os.WriteFile(configPath, configContent, 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	// Set environment variables for the hook
	oldConfigPath := os.Getenv("GOCLAW_CONFIG")
	oldEncKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	defer func() {
		os.Setenv("GOCLAW_CONFIG", oldConfigPath)
		os.Setenv("GOCLAW_ENCRYPTION_KEY", oldEncKey)
	}()

	os.Setenv("GOCLAW_CONFIG", configPath)
	os.Setenv("GOCLAW_ENCRYPTION_KEY", testEncryptionKey)

	resetWebSearchMigrateHook(t, db)

	// Run the migration hook
	if _, err := upgrade.RunPendingHooks(ctx, db); err != nil {
		t.Fatalf("RunPendingHooks: %v", err)
	}

	// Verify row was created in config_secrets
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
		"tools.web.brave.api_key", store.MasterTenantID).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 secret row, got %d", count)
	}

	// Verify the encrypted value can be decrypted
	var encValue []byte
	err = db.QueryRowContext(ctx,
		`SELECT value FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
		"tools.web.brave.api_key", store.MasterTenantID).Scan(&encValue)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}

	decrypted, err := crypto.Decrypt(string(encValue), testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != "brave-key-from-config-json5" {
		t.Errorf("decrypted value mismatch: got %q", decrypted)
	}

	// Re-run the hook (should be idempotent)
	if _, err := upgrade.RunPendingHooks(ctx, db); err != nil && err.Error() != "no pending hooks" {
		// It's OK if there are no pending hooks on re-run
	}

	// Verify still only 1 row (no duplicate created)
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
		"tools.web.brave.api_key", store.MasterTenantID).Scan(&count)
	if err != nil {
		t.Fatalf("count query after re-run: %v", err)
	}
	if count != 1 {
		t.Errorf("expected still 1 secret row after re-run, got %d", count)
	}

	// Cleanup
	db.ExecContext(ctx,
		`DELETE FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
		"tools.web.brave.api_key", store.MasterTenantID)
}

// TestWebSearchMigrateHook_SettingsBlobLegacy tests migration of inline keys
// from builtin_tool_tenant_configs.settings JSON blob (sub-test B).
func TestWebSearchMigrateHook_SettingsBlobLegacy(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	// Pre-seed a builtin_tool_tenant_configs row with inline Exa key in settings
	settingsBlob := map[string]interface{}{
		"exa": map[string]interface{}{
			"api_key": "legacy-exa-key-from-settings",
			"enabled": true,
		},
		"provider_order": []string{"exa", "duckduckgo"},
	}
	settingsJSON, err := json.Marshal(settingsBlob)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tenant_id, tool_name, settings, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (tenant_id, tool_name) DO NOTHING`,
		tenantID, "web_search", settingsJSON)
	if err != nil {
		t.Fatalf("insert tool config: %v", err)
	}

	t.Cleanup(func() {
		db.ExecContext(ctx,
			`DELETE FROM builtin_tool_tenant_configs WHERE tenant_id=$1 AND tool_name=$2`,
			tenantID, "web_search")
		db.ExecContext(ctx,
			`DELETE FROM config_secrets WHERE tenant_id=$1 AND key=$2`,
			tenantID, "tools.web.exa.api_key")
	})

	// Set encryption key for the hook
	oldEncKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	defer os.Setenv("GOCLAW_ENCRYPTION_KEY", oldEncKey)
	os.Setenv("GOCLAW_ENCRYPTION_KEY", testEncryptionKey)

	resetWebSearchMigrateHook(t, db)

	// Run the migration hook
	if _, err := upgrade.RunPendingHooks(context.Background(), db); err != nil {
		t.Fatalf("RunPendingHooks: %v", err)
	}

	// Verify secret was created
	var encValue []byte
	err = db.QueryRowContext(ctx,
		`SELECT value FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
		"tools.web.exa.api_key", tenantID).Scan(&encValue)
	if err == sql.ErrNoRows {
		t.Fatalf("secret not created")
	} else if err != nil {
		t.Fatalf("read secret: %v", err)
	}

	// Verify decrypted value matches
	decrypted, err := crypto.Decrypt(string(encValue), testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != "legacy-exa-key-from-settings" {
		t.Errorf("decrypted value mismatch: got %q", decrypted)
	}

	// Verify the api_key field was removed from settings blob
	var settingsAfter []byte
	err = db.QueryRowContext(ctx,
		`SELECT settings FROM builtin_tool_tenant_configs WHERE tenant_id=$1 AND tool_name=$2`,
		tenantID, "web_search").Scan(&settingsAfter)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var settingsMap map[string]interface{}
	if err := json.Unmarshal(settingsAfter, &settingsMap); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}

	// Check that exa section no longer has api_key
	exaSection, ok := settingsMap["exa"].(map[string]interface{})
	if ok && exaSection["api_key"] != nil {
		t.Errorf("api_key should have been stripped from settings, but found: %v", exaSection["api_key"])
	}

	// Verify other fields in exa section are preserved
	if exaSection != nil && exaSection["enabled"] != true {
		t.Errorf("enabled field should be preserved in settings")
	}

	// Verify provider_order is still present
	if _, ok := settingsMap["provider_order"]; !ok {
		t.Errorf("provider_order should be preserved in settings")
	}
}

// TestWebSearchMigrateHook_ExistingSecretNoOverwrite tests that existing
// secrets are not overwritten by the migration (sub-test C).
func TestWebSearchMigrateHook_ExistingSecretNoOverwrite(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	// Pre-seed an existing secret with one value
	existingKey := "existing-brave-key-from-ui"
	encExisting, err := crypto.Encrypt(existingKey, testEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt existing: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO config_secrets (key, value, updated_at, tenant_id)
		 VALUES ($1, $2, NOW(), $3)`,
		"tools.web.brave.api_key", []byte(encExisting), tenantID)
	if err != nil {
		t.Fatalf("insert existing secret: %v", err)
	}

	t.Cleanup(func() {
		db.ExecContext(ctx,
			`DELETE FROM config_secrets WHERE tenant_id=$1 AND key=$2`,
			tenantID, "tools.web.brave.api_key")
		db.ExecContext(ctx,
			`DELETE FROM builtin_tool_tenant_configs WHERE tenant_id=$1 AND tool_name=$2`,
			tenantID, "web_search")
	})

	// Pre-seed settings blob with a DIFFERENT Brave key
	settingsBlob := map[string]interface{}{
		"brave": map[string]interface{}{
			"api_key": "different-brave-key-from-settings",
			"enabled": true,
		},
	}
	settingsJSON, err := json.Marshal(settingsBlob)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO builtin_tool_tenant_configs (tenant_id, tool_name, settings, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (tenant_id, tool_name) DO NOTHING`,
		tenantID, "web_search", settingsJSON)
	if err != nil {
		t.Fatalf("insert tool config: %v", err)
	}

	// Set encryption key for the hook
	oldEncKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	defer os.Setenv("GOCLAW_ENCRYPTION_KEY", oldEncKey)
	os.Setenv("GOCLAW_ENCRYPTION_KEY", testEncryptionKey)

	resetWebSearchMigrateHook(t, db)

	// Run the migration hook
	if _, err := upgrade.RunPendingHooks(context.Background(), db); err != nil {
		t.Fatalf("RunPendingHooks: %v", err)
	}

	// Verify the existing secret was NOT overwritten
	var encValue []byte
	err = db.QueryRowContext(ctx,
		`SELECT value FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
		"tools.web.brave.api_key", tenantID).Scan(&encValue)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}

	decrypted, err := crypto.Decrypt(string(encValue), testEncryptionKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// Should still have the EXISTING key value, not the settings-blob value
	if decrypted != existingKey {
		t.Errorf("existing secret was overwritten. Expected %q, got %q", existingKey, decrypted)
	}

	// Verify the api_key field was still removed from settings (cleanup)
	var settingsAfter []byte
	err = db.QueryRowContext(ctx,
		`SELECT settings FROM builtin_tool_tenant_configs WHERE tenant_id=$1 AND tool_name=$2`,
		tenantID, "web_search").Scan(&settingsAfter)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var settingsMap map[string]interface{}
	if err := json.Unmarshal(settingsAfter, &settingsMap); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}

	braveSection, ok := settingsMap["brave"].(map[string]interface{})
	if ok && braveSection["api_key"] != nil {
		t.Errorf("api_key should have been stripped from settings even when existing secret exists")
	}
}

// resetWebSearchMigrateHook deletes the data_migrations row for the web_search
// migrate hook so RunPendingHooks re-executes it. testDB is shared across tests,
// so without this the hook runs only once per test-binary invocation.
func resetWebSearchMigrateHook(t *testing.T, db *sql.DB) {
	t.Helper()
	// Ensure table exists — first test run creates it via RunPendingHooks,
	// but reset must work on a fresh DB too.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS data_migrations (
			name       VARCHAR(255) PRIMARY KEY,
			version    INT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("ensure data_migrations: %v", err)
	}
	if _, err := db.Exec(
		`DELETE FROM data_migrations WHERE name=$1`,
		"055_web_search_legacy_keys_to_config_secrets"); err != nil {
		t.Fatalf("reset hook: %v", err)
	}
}
