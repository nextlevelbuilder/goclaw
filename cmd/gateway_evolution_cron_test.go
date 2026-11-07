package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeEvolutionTenants struct {
	tenants []store.TenantData
	err     error
}

func (f fakeEvolutionTenants) ListTenants(context.Context) ([]store.TenantData, error) {
	return f.tenants, f.err
}

// fakeEvolutionAgents mimics the fail-closed, tenant-scoped AgentStore.List.
type fakeEvolutionAgents struct {
	byTenant map[uuid.UUID][]store.AgentData
	failFor  uuid.UUID
}

func (f fakeEvolutionAgents) List(ctx context.Context, _ string) ([]store.AgentData, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, store.ErrTenantRequired
	}
	if tid == f.failFor {
		return nil, errors.New("boom")
	}
	return f.byTenant[tid], nil
}

func TestListAgentsAcrossTenants(t *testing.T) {
	tenantA, tenantB, tenantC := uuid.New(), uuid.New(), uuid.New()
	agentA := store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, TenantID: tenantA}
	agentB := store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, TenantID: tenantB}
	agentC := store.AgentData{BaseModel: store.BaseModel{ID: uuid.New()}, TenantID: tenantC}

	tenants := fakeEvolutionTenants{tenants: []store.TenantData{
		{ID: tenantA, Status: store.TenantStatusActive},
		{ID: tenantB, Status: store.TenantStatusActive},
		{ID: tenantC, Status: "suspended"},
	}}
	agents := fakeEvolutionAgents{byTenant: map[uuid.UUID][]store.AgentData{
		tenantA: {agentA}, tenantB: {agentB}, tenantC: {agentC},
	}}

	t.Run("collects every active tenant and skips inactive ones", func(t *testing.T) {
		got, err := listAgentsAcrossTenants(context.Background(), tenants, agents)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != 2 || got[0].ID != agentA.ID || got[1].ID != agentB.ID {
			t.Fatalf("got %d agents %+v, want agents of tenants A and B only", len(got), got)
		}
	})

	t.Run("one failing tenant does not hide the others", func(t *testing.T) {
		failing := agents
		failing.failFor = tenantA
		got, err := listAgentsAcrossTenants(context.Background(), tenants, failing)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != 1 || got[0].ID != agentB.ID {
			t.Fatalf("got %+v, want only tenant B's agent", got)
		}
	})

	t.Run("tenant listing failure is returned", func(t *testing.T) {
		_, err := listAgentsAcrossTenants(context.Background(), fakeEvolutionTenants{err: errors.New("db down")}, agents)
		if err == nil {
			t.Fatal("want error when tenants cannot be listed")
		}
	})
}
