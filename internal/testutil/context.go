package testutil

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TenantCtx returns a context seeded with the given tenant UUID.
// Panics on malformed uuid strings to keep test call-sites concise.
func TenantCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

// UserCtx returns a context with tenant + user identities set.
func UserCtx(tenantID uuid.UUID, userID string) context.Context {
	ctx := store.WithTenantID(context.Background(), tenantID)
	return store.WithUserID(ctx, userID)
}

// AgentCtx returns a context with tenant + agent identities set.
func AgentCtx(tenantID, agentID uuid.UUID) context.Context {
	ctx := store.WithTenantID(context.Background(), tenantID)
	return store.WithAgentID(ctx, agentID)
}

// FullCtx returns a context with tenant + user + agent identities set.
func FullCtx(tenantID uuid.UUID, userID string, agentID uuid.UUID) context.Context {
	ctx := store.WithTenantID(context.Background(), tenantID)
	ctx = store.WithUserID(ctx, userID)
	return store.WithAgentID(ctx, agentID)
}

// MustParseUUID is a helper for tests to turn a literal into uuid.UUID.
// Tests die loudly on malformed input; production code should never touch this.
func MustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic("testutil.MustParseUUID: " + err.Error())
	}
	return id
}
