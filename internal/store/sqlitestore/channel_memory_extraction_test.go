//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestChannelMemoryCreateItemDedupesAcrossRuns(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.New()
	agentID := uuid.New()
	channelID := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 't-channel-memory', 'active')`, tenantID.String()); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO agents (id, tenant_id, agent_key, display_name, owner_id, provider, model)
		VALUES (?, ?, 'agent-channel-memory', 'Agent', 'owner', 'openai', 'gpt-4o')`, agentID.String(), tenantID.String()); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO channel_instances (id, tenant_id, name, channel_type, agent_id)
		VALUES (?, ?, 'discord', 'discord', ?)`, channelID.String(), tenantID.String(), agentID.String()); err != nil {
		t.Fatalf("insert channel instance: %v", err)
	}

	ctx := store.WithTenantID(context.Background(), tenantID)
	extractions := NewSQLiteChannelMemoryExtractionStore(db)
	runA := &store.ChannelMemoryExtractionRun{
		ChannelInstanceID: channelID,
		ChannelName:       "discord",
		AgentID:           agentID,
		UserID:            "user-1",
		HistoryKey:        "group-a",
		SourceStartID:     "msg-1",
		SourceEndID:       "msg-2",
	}
	if err := extractions.CreateRun(ctx, runA); err != nil {
		t.Fatalf("CreateRun A: %v", err)
	}
	runB := &store.ChannelMemoryExtractionRun{
		ChannelInstanceID: channelID,
		ChannelName:       "discord",
		AgentID:           agentID,
		UserID:            "user-1",
		HistoryKey:        "group-a",
		SourceStartID:     "msg-3",
		SourceEndID:       "msg-4",
	}
	if err := extractions.CreateRun(ctx, runB); err != nil {
		t.Fatalf("CreateRun B: %v", err)
	}

	itemA := &store.ChannelMemoryExtractionItem{
		RunID:             runA.ID,
		ChannelInstanceID: channelID,
		AgentID:           agentID,
		UserID:            "user-1",
		ItemHash:          "stable-hash",
		ItemType:          "todos",
		Summary:           "Follow up on rollout checklist.",
		SourceID:          "channel:stable-hash",
	}
	if err := extractions.CreateItem(ctx, itemA); err != nil {
		t.Fatalf("CreateItem A: %v", err)
	}
	itemB := &store.ChannelMemoryExtractionItem{
		RunID:             runB.ID,
		ChannelInstanceID: channelID,
		AgentID:           agentID,
		UserID:            "user-1",
		ItemHash:          "stable-hash",
		ItemType:          "todos",
		Summary:           "Follow up on rollout checklist.",
		SourceID:          "channel:stable-hash",
	}
	if err := extractions.CreateItem(ctx, itemB); err != nil {
		t.Fatalf("CreateItem B: %v", err)
	}
	if itemB.ID != itemA.ID {
		t.Fatalf("duplicate item ID = %s, want existing %s", itemB.ID, itemA.ID)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM channel_memory_extraction_items WHERE tenant_id = ? AND channel_instance_id = ? AND item_hash = ?`,
		tenantID.String(), channelID.String(), "stable-hash").Scan(&count); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if count != 1 {
		t.Fatalf("duplicate item count = %d, want 1", count)
	}
}
