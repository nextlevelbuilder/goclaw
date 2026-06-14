package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// captureMeowStore records UpsertChannel calls; all other methods are no-ops.
// Lets us verify SeedMeowChannels without a database.
type captureMeowStore struct {
	store.MeowStore
	upserts []store.MpChannel
}

func (c *captureMeowStore) UpsertChannel(_ context.Context, ch *store.MpChannel) error {
	ch.ID = uuid.New()
	c.upserts = append(c.upserts, *ch)
	return nil
}

func TestSeedMeowChannels(t *testing.T) {
	cs := &captureMeowStore{}
	n, err := SeedMeowChannels(context.Background(), cs, store.MasterTenantID)
	if err != nil {
		t.Fatalf("SeedMeowChannels: %v", err)
	}
	if n != 8 || len(cs.upserts) != 8 {
		t.Fatalf("expected 8 channels seeded, got n=%d upserts=%d", n, len(cs.upserts))
	}

	byHandle := map[string]store.MpChannel{}
	for _, ch := range cs.upserts {
		if ch.TenantID != store.MasterTenantID {
			t.Errorf("%s seeded under wrong tenant %s", ch.Handle, ch.TenantID)
		}
		if ch.BrandKey == "" || len(ch.ButtonSet) == 0 || string(ch.ButtonSet) == "[]" {
			t.Errorf("%s missing brand_key or buttons", ch.Handle)
		}
		if !ch.Enabled {
			t.Errorf("%s should be enabled", ch.Handle)
		}
		byHandle[ch.Handle] = ch
	}

	// Pre-launch channel must be launched=false; everything else launched=true.
	for h, ch := range byHandle {
		want := h != "@MonkeyMatgo"
		if ch.Launched != want {
			t.Errorf("%s launched=%v, want %v", h, ch.Launched, want)
		}
	}
	if mm, ok := byHandle["@MonkeyMatgo"]; !ok || mm.Launched {
		t.Error("@MonkeyMatgo must be present and launched=false")
	}
	// Mascot channels per the plan.
	for _, h := range []string{"@onedollar_project", "@OneJackpotOfficial", "@monkeytimeofficial", "@MonkeyMatgo"} {
		if !byHandle[h].HasMascot {
			t.Errorf("%s should have has_mascot=true", h)
		}
	}
}

// fakeAgentStore implements only GetByKey + Create.
type fakeAgentStore struct {
	store.AgentStore
	existing *store.AgentData
	created  []*store.AgentData
}

func (f *fakeAgentStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	if f.existing != nil && f.existing.AgentKey == key {
		return f.existing, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeAgentStore) Create(_ context.Context, a *store.AgentData) error {
	f.created = append(f.created, a)
	f.existing = a
	return nil
}

func TestSeedMeowAgent(t *testing.T) {
	fs := &fakeAgentStore{}

	agent, created, err := SeedMeowAgent(context.Background(), fs, store.MasterTenantID, "anthropic", "claude-x")
	if err != nil || !created {
		t.Fatalf("first seed: created=%v err=%v", created, err)
	}
	if agent.AgentKey != MeowAgentKey || agent.TenantID != store.MasterTenantID || agent.AgentType != "predefined" {
		t.Fatalf("unexpected agent: key=%s tenant=%s type=%s", agent.AgentKey, agent.TenantID, agent.AgentType)
	}
	// Tool allowlist must reference ONLY real registered tools.
	pol := agent.ParseToolsConfig()
	if pol == nil || len(pol.Allow) != 1 || pol.Allow[0] != "publish_channel_post" {
		t.Fatalf("tools allowlist wrong: %+v", pol)
	}

	// Idempotent: second call creates nothing.
	_, created2, err := SeedMeowAgent(context.Background(), fs, store.MasterTenantID, "anthropic", "claude-x")
	if err != nil || created2 {
		t.Fatalf("second seed should be no-op: created=%v err=%v", created2, err)
	}
	if len(fs.created) != 1 {
		t.Fatalf("expected exactly 1 Create, got %d", len(fs.created))
	}
}
