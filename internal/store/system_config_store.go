package store

import "context"

// SystemConfigStore manages per-tenant configuration settings.
// Non-secret, plain-text key-value pairs. Use ConfigSecretsStore for secrets.
//
// STRICT tenant isolation — there is NO MasterTenantID fallback or master/tenant
// merge. Every operation requires a tenant_id in context and reads/writes only
// that exact tenant's rows; a missing tenant is an error, not a fallback. The
// PG and SQLite implementations (internal/store/pg/system_configs.go,
// internal/store/sqlitestore/system-configs.go) enforce this. Do NOT reintroduce
// a fallback/merge here or in any implementation — cross-tenant reads were the
// exact leak Phase 7 closed (see internal/teamworkconfig.Resolver, which layers
// its own file-config defaults precisely because this store never does).
type SystemConfigStore interface {
	// Get returns the config value for the tenant in ctx. Returns an error when
	// no tenant is in context or the key is absent for that tenant.
	Get(ctx context.Context, key string) (string, error)
	// Set stores a config value for the tenant in ctx. Returns an error when no
	// tenant is in context (it does NOT fall back to any master tenant).
	Set(ctx context.Context, key, value string) error
	// Delete removes a config value for the tenant in ctx.
	Delete(ctx context.Context, key string) error
	// List returns all configs for the tenant in ctx (that tenant's rows only —
	// no master merge). Returns an error when no tenant is in context.
	List(ctx context.Context) (map[string]string, error)
}
