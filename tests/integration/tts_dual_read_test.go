//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestTTSConfig_Store_RoundTrip_LegacyKeys verifies that legacy flat keys
// stored in system_configs can be read back (PG backend).
func TestTTSConfig_Store_RoundTrip_LegacyKeys(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	ctx := store.WithTenantID(context.Background(), tenantID)

	configStore := pg.NewPGSystemConfigStore(db)

	t.Cleanup(func() {
		db.Exec("DELETE FROM system_configs WHERE key LIKE 'tts.%'")
	})

	// Set legacy flat keys
	if err := configStore.Set(ctx, "tts.provider", "openai"); err != nil {
		t.Fatalf("Set provider: %v", err)
	}
	if err := configStore.Set(ctx, "tts.openai.voice", "alloy"); err != nil {
		t.Fatalf("Set voice: %v", err)
	}

	// Read them back
	provider, err := configStore.Get(ctx, "tts.provider")
	if err != nil {
		t.Fatalf("Get provider: %v", err)
	}
	if provider != "openai" {
		t.Errorf("provider: got %q, want openai", provider)
	}

	voice, err := configStore.Get(ctx, "tts.openai.voice")
	if err != nil {
		t.Fatalf("Get voice: %v", err)
	}
	if voice != "alloy" {
		t.Errorf("voice: got %q, want alloy", voice)
	}
}

// TestTTSConfig_Store_RoundTrip_ParamsBlob verifies that params blob
// stored in system_configs can be read back (PG backend).
func TestTTSConfig_Store_RoundTrip_ParamsBlob(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	ctx := store.WithTenantID(context.Background(), tenantID)

	configStore := pg.NewPGSystemConfigStore(db)

	t.Cleanup(func() {
		db.Exec("DELETE FROM system_configs WHERE key LIKE 'tts.%'")
	})

	// Set provider and params blob
	configStore.Set(ctx, "tts.provider", "elevenlabs")
	paramsBlob := `{"voice_settings":{"stability":0.8}}`
	configStore.Set(ctx, "tts.elevenlabs.params", paramsBlob)

	// Read them back
	provider, _ := configStore.Get(ctx, "tts.provider")
	if provider != "elevenlabs" {
		t.Errorf("provider: got %q, want elevenlabs", provider)
	}

	retrieved, _ := configStore.Get(ctx, "tts.elevenlabs.params")
	if retrieved != paramsBlob {
		t.Errorf("params blob: got %q, want %q", retrieved, paramsBlob)
	}
}

// TestTTSConfig_Store_Dual_LegacyAndBlob verifies that both legacy keys and
// params blob can coexist in system_configs (PG backend).
func TestTTSConfig_Store_Dual_LegacyAndBlob(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	ctx := store.WithTenantID(context.Background(), tenantID)

	configStore := pg.NewPGSystemConfigStore(db)

	t.Cleanup(func() {
		db.Exec("DELETE FROM system_configs WHERE key LIKE 'tts.%'")
	})

	// Set both legacy and blob
	configStore.Set(ctx, "tts.provider", "minimax")
	configStore.Set(ctx, "tts.minimax.voice", "Wise_Woman")
	configStore.Set(ctx, "tts.minimax.params", `{"speed":1.5}`)

	// Retrieve both
	voice, _ := configStore.Get(ctx, "tts.minimax.voice")
	if voice != "Wise_Woman" {
		t.Errorf("voice: got %q, want Wise_Woman", voice)
	}

	params, _ := configStore.Get(ctx, "tts.minimax.params")
	if params != `{"speed":1.5}` {
		t.Errorf("params: got %q, want {\"speed\":1.5}", params)
	}
}

// TestTTSConfig_Store_DisjointUnion verifies disjoint storage and retrieval
// (legacy has only voice, blob has only params - PG backend).
func TestTTSConfig_Store_DisjointUnion(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	ctx := store.WithTenantID(context.Background(), tenantID)

	configStore := pg.NewPGSystemConfigStore(db)

	t.Cleanup(func() {
		db.Exec("DELETE FROM system_configs WHERE key LIKE 'tts.%'")
	})

	// Set disjoint: legacy voice, blob params
	configStore.Set(ctx, "tts.provider", "edge")
	configStore.Set(ctx, "tts.edge.voice", "en-US-AriaNeural")
	configStore.Set(ctx, "tts.edge.params", `{"rate":0.8,"pitch":1.2}`)

	// Retrieve
	voice, _ := configStore.Get(ctx, "tts.edge.voice")
	if voice != "en-US-AriaNeural" {
		t.Errorf("voice: got %q", voice)
	}

	params, _ := configStore.Get(ctx, "tts.edge.params")
	if params != `{"rate":0.8,"pitch":1.2}` {
		t.Errorf("params: got %q", params)
	}
}

// TestTTSConfig_Store_MultiTenant verifies tenant isolation in system_configs
// (PG backend).
func TestTTSConfig_Store_MultiTenant(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	configStore := pg.NewPGSystemConfigStore(db)

	t.Cleanup(func() {
		db.Exec("DELETE FROM system_configs WHERE key LIKE 'tts.%'")
	})

	// Tenant A sets openai
	configStore.Set(ctxA, "tts.provider", "openai")

	// Tenant B sets elevenlabs
	configStore.Set(ctxB, "tts.provider", "elevenlabs")

	// Verify isolation
	providerA, _ := configStore.Get(ctxA, "tts.provider")
	if providerA != "openai" {
		t.Errorf("tenant A: got %q, want openai", providerA)
	}

	providerB, _ := configStore.Get(ctxB, "tts.provider")
	if providerB != "elevenlabs" {
		t.Errorf("tenant B: got %q, want elevenlabs", providerB)
	}
}
