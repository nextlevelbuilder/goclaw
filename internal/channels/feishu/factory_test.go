package feishu

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type factoryAgentStore struct {
	store.AgentStore
	agent     *store.AgentData
	gotKey    string
	gotTenant uuid.UUID
}

func (f *factoryAgentStore) GetByKey(ctx context.Context, agentKey string) (*store.AgentData, error) {
	f.gotKey = agentKey
	f.gotTenant = store.TenantIDFromContext(ctx)
	return f.agent, nil
}

func TestFactory_MissingAppID(t *testing.T) {
	creds, _ := json.Marshal(map[string]string{"app_secret": "s"})
	_, err := Factory("test", creds, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing app_id")
	}
}

func TestFactory_MissingAppSecret(t *testing.T) {
	creds, _ := json.Marshal(map[string]string{"app_id": "a"})
	_, err := Factory("test", creds, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing app_secret")
	}
}

func TestFactory_EmptyCreds(t *testing.T) {
	_, err := Factory("test", nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty credentials")
	}
}

func TestFactory_InvalidCredsJSON(t *testing.T) {
	_, err := Factory("test", []byte("not-json{"), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid credentials JSON")
	}
}

func TestFactory_InvalidConfigJSON(t *testing.T) {
	creds, _ := json.Marshal(map[string]string{"app_id": "a", "app_secret": "s"})
	_, err := Factory("test", creds, []byte("not-json{"), nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid config JSON")
	}
}

func TestFactory_ValidMinimal(t *testing.T) {
	creds, _ := json.Marshal(map[string]string{
		"app_id":     "cli_test_app",
		"app_secret": "test_secret_123",
	})
	ch, err := Factory("feishu-test", creds, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestFactory_ValidWithConfig(t *testing.T) {
	creds, _ := json.Marshal(map[string]string{
		"app_id":             "cli_test_app",
		"app_secret":         "test_secret_123",
		"verification_token": "vtok",
		"encrypt_key":        "ekey",
	})
	cfg, _ := json.Marshal(map[string]any{
		"domain":           "feishu",
		"connection_mode":  "webhook",
		"dm_policy":        "open",
		"group_policy":     "allowlist",
		"text_chunk_limit": 2000,
		"media_max_mb":     10,
	})
	ch, err := Factory("feishu-full", creds, cfg, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestFactory_DefaultGroupPolicy(t *testing.T) {
	// When group_policy is empty in config, Factory should default to "pairing".
	creds, _ := json.Marshal(map[string]string{
		"app_id":     "cli_test_app2",
		"app_secret": "secret2",
	})
	cfg, _ := json.Marshal(map[string]any{}) // empty config
	ch, err := Factory("feishu-default-policy", creds, cfg, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("channel must not be nil")
	}
}

func TestFactoryWithPendingStore_Valid(t *testing.T) {
	factory := FactoryWithPendingStore(nil)
	if factory == nil {
		t.Fatal("FactoryWithPendingStore returned nil factory func")
	}

	creds, _ := json.Marshal(map[string]string{
		"app_id":     "cli_ws_app",
		"app_secret": "ws_secret",
	})
	ch, err := factory("feishu-ws", creds, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestFactoryWithPendingStore_MissingCreds(t *testing.T) {
	factory := FactoryWithPendingStore(nil)
	creds, _ := json.Marshal(map[string]string{"app_id": "only-id"})
	_, err := factory("feishu-bad", creds, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing app_secret")
	}
}

func TestFactoryWithPendingStore_InvalidCredsJSON(t *testing.T) {
	factory := FactoryWithPendingStore(nil)
	_, err := factory("feishu-bad", []byte("{bad"), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFactoryWithPendingStore_InvalidConfigJSON(t *testing.T) {
	factory := FactoryWithPendingStore(nil)
	creds, _ := json.Marshal(map[string]string{"app_id": "a", "app_secret": "b"})
	_, err := factory("feishu-bad", creds, []byte("{bad"), nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid config JSON")
	}
}

func TestFactoryWithStoresAndAudio_WiresWriterDependencies(t *testing.T) {
	agentID := uuid.New()
	tenantID := uuid.New()
	agentStore := &factoryAgentStore{agent: &store.AgentData{
		BaseModel: store.BaseModel{ID: agentID},
		AgentKey:  "writer-agent",
	}}
	configPermStore := &fakeConfigPermStore{}
	factory := FactoryWithStoresAndAudio(agentStore, configPermStore, nil, nil)
	creds, _ := json.Marshal(map[string]string{
		"app_id":     "cli_store_app",
		"app_secret": "store_secret",
	})

	raw, err := factory("feishu-with-stores", creds, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ch, ok := raw.(*Channel)
	if !ok {
		t.Fatalf("factory returned %T, want *Channel", raw)
	}
	if ch.agentStore != agentStore {
		t.Fatal("agent store was not wired into Feishu channel")
	}
	if ch.configPermStore != configPermStore {
		t.Fatal("config permission store was not wired into Feishu channel")
	}

	srv, replies := captureReplies(t)
	ch.client = NewLarkClient("app", "secret", srv.URL)
	ch.botOpenID = "ou_fake_bot"
	ch.SetAgentID("writer-agent")
	ch.SetTenantID(tenantID)

	handled := ch.maybeHandleWriterCommand(context.Background(), &messageContext{
		ChatID:    "oc_factory_group",
		MessageID: "om_factory_command",
		SenderID:  "ou_alice",
		ChatType:  "group",
		Content:   "/addwriter @_user_1",
		Mentions:  []mentionInfo{{Key: "@_user_1", OpenID: "ou_alice", Name: "Alice"}},
	})
	if !handled {
		t.Fatal("expected /addwriter to be handled")
	}
	assertReplyContains(t, *replies, "Added Alice as a file writer")
	if agentStore.gotKey != "writer-agent" {
		t.Fatalf("agent lookup key = %q, want writer-agent", agentStore.gotKey)
	}
	if agentStore.gotTenant != tenantID {
		t.Fatalf("agent lookup tenant = %s, want %s", agentStore.gotTenant, tenantID)
	}
	writers, err := configPermStore.ListFileWriters(context.Background(), agentID, "group:feishu-with-stores:oc_factory_group")
	if err != nil {
		t.Fatalf("list file writers: %v", err)
	}
	if len(writers) != 1 || writers[0].UserID != "ou_alice" {
		t.Fatalf("writers = %+v, want ou_alice", writers)
	}
}
