package upgrade

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

const workspaceNormalizeHookName = "072_normalize_agent_workspaces"

// normalizeAgentWorkspaces rewrites stale `agents.workspace` values that
// won't resolve on the current host. Triggered automatically when the
// deployment backend changes shape (Docker → bare-metal, host path drift).
//
// A workspace is considered stale when it is non-portable on the new host:
//   - prefixed with `/app/workspace/` (Docker container path persisted across
//     a Docker → bare-metal migration; `/app` does not exist on the host)
//   - prefixed with `~` (Go does not expand tildes — Mkdir would create a
//     literal `~` directory in cwd)
//
// Stale rows are rewritten to `{configured_base}/{agent_key}`, where the
// base is read with the same precedence used by the gateway:
// `GOCLAW_WORKSPACE` env > config file `Agents.Defaults.Workspace` >
// default `~/.goclaw/workspace` (with tilde expansion).
//
// Idempotent: rows already at the proposed value are skipped.
// Conservative: absolute, non-stale paths are left untouched so operators
// who intentionally placed an agent in a custom directory keep their config.
func normalizeAgentWorkspaces(ctx context.Context, db *sql.DB) error {
	base := resolveWorkspaceBase()
	if base == "" {
		slog.Warn("workspace normalization: no base configured, skipping")
		return nil
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, agent_key, workspace FROM agents WHERE deleted_at IS NULL`)
	if err != nil {
		return fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	type pending struct {
		id, key, current, proposed string
	}
	var todo []pending
	for rows.Next() {
		var id, key, current string
		if err := rows.Scan(&id, &key, &current); err != nil {
			return fmt.Errorf("scan agent row: %w", err)
		}
		if !isStaleWorkspace(current) {
			continue
		}
		proposed := filepath.Join(base, key)
		if current == proposed {
			continue
		}
		todo = append(todo, pending{id, key, current, proposed})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate agent rows: %w", err)
	}

	for _, p := range todo {
		if _, err := db.ExecContext(ctx,
			`UPDATE agents SET workspace = $1, updated_at = NOW() WHERE id = $2`,
			p.proposed, p.id,
		); err != nil {
			return fmt.Errorf("update agent %q: %w", p.key, err)
		}
		slog.Info("workspace normalized",
			"agent_key", p.key,
			"from", p.current,
			"to", p.proposed,
		)
	}

	slog.Info("workspace normalization complete",
		"rewritten", len(todo),
		"base", base,
	)
	return nil
}

// isStaleWorkspace reports whether the stored path is non-portable on the
// current host. See normalizeAgentWorkspaces for the full set of patterns.
func isStaleWorkspace(ws string) bool {
	ws = strings.TrimSpace(ws)
	if ws == "" {
		return false
	}
	if ws == "/app/workspace" || strings.HasPrefix(ws, "/app/workspace/") {
		return true
	}
	if strings.HasPrefix(ws, "~") {
		return true
	}
	return false
}

// resolveWorkspaceBase returns the configured workspace base directory.
// Matches the gateway's precedence so the hook normalizes to whatever the
// next startup will use.
func resolveWorkspaceBase() string {
	if v := strings.TrimSpace(os.Getenv("GOCLAW_WORKSPACE")); v != "" {
		return strings.TrimRight(config.ExpandHome(v), "/")
	}
	cfgPath := os.Getenv("GOCLAW_CONFIG")
	if cfgPath == "" {
		cfgPath = "config.json"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Warn("workspace normalization: load config failed, using default",
			"path", cfgPath, "error", err)
		return strings.TrimRight(config.ExpandHome("~/.goclaw/workspace"), "/")
	}
	return strings.TrimRight(cfg.WorkspacePath(), "/")
}
