package bootstrap

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MeowAgentKey is the agent_key of the predefined Meow autopilot agent.
const MeowAgentKey = "meow"

// meowAgentTools is the Meow agent's tool allowlist. Every entry MUST be a real
// registered tool — publish_channel_post is registered at gateway startup when
// the Meow store is present.
var meowAgentTools = []string{"publish_channel_post"}

// SeedMeowAgent ensures the predefined Meow agent exists under the owner tenant
// with a tool allowlist restricted to meowAgentTools. Idempotent: if the agent
// already exists it is returned unchanged (created=false). provider/model seed
// the scaffold (typically copied from the default agent); they can be empty for
// a placeholder until configured. The caller seeds context files for a freshly
// created agent (created=true).
func SeedMeowAgent(ctx context.Context, agentStore store.AgentStore, tenantID uuid.UUID, provider, model string) (agent *store.AgentData, created bool, err error) {
	ctx = store.WithTenantID(ctx, tenantID)
	if existing, gerr := agentStore.GetByKey(ctx, MeowAgentKey); gerr == nil && existing != nil {
		return existing, false, nil
	}
	toolsCfg, err := json.Marshal(config.ToolPolicySpec{Allow: meowAgentTools})
	if err != nil {
		return nil, false, fmt.Errorf("marshal meow tools_config: %w", err)
	}
	agent = &store.AgentData{
		BaseModel:   store.BaseModel{ID: uuid.New()},
		TenantID:    tenantID,
		AgentKey:    MeowAgentKey,
		DisplayName: "Meow",
		OwnerID:     "system",
		Provider:    provider,
		Model:       model,
		AgentType:   "predefined",
		ToolsConfig: toolsCfg,
	}
	if err := agentStore.Create(ctx, agent); err != nil {
		return nil, false, fmt.Errorf("create meow agent: %w", err)
	}
	return agent, true, nil
}

// meowChannelsSeed is the repo-bundled lightweight channel registry. The full
// per-channel briefs/calendars/assets live in each source project's
// marketing/meow/ folder (not in this repo); only the runtime registry the
// gateway needs to publish lives here.
//
//go:embed meow_channels.json
var meowChannelsSeed []byte

// meowChannelSeed is one row of the bundled registry.
type meowChannelSeed struct {
	Handle       string            `json:"handle"`
	BrandKey     string            `json:"brand_key"`
	TZ           string            `json:"tz"`
	HasMascot    bool              `json:"has_mascot"`
	Launched     bool              `json:"launched"`
	SubsBaseline int               `json:"subs_baseline"`
	Buttons      []json.RawMessage `json:"buttons"`
}

// SeedMeowChannels upserts the bundled channel registry into mp_channels under
// the given owner tenant. Idempotent: re-running updates existing rows in place
// (keyed on tenant_id+handle) and preserves their ids. Returns the row count.
func SeedMeowChannels(ctx context.Context, ms store.MeowStore, tenantID uuid.UUID) (int, error) {
	var seeds []meowChannelSeed
	if err := json.Unmarshal(meowChannelsSeed, &seeds); err != nil {
		return 0, fmt.Errorf("parse meow channel registry: %w", err)
	}

	ctx = store.WithTenantID(ctx, tenantID)
	for _, s := range seeds {
		buttons, err := json.Marshal(s.Buttons)
		if err != nil {
			return 0, fmt.Errorf("marshal buttons for %s: %w", s.Handle, err)
		}
		ch := &store.MpChannel{
			TenantID:     tenantID,
			Handle:       s.Handle,
			BrandKey:     s.BrandKey,
			TZ:           s.TZ,
			HasMascot:    s.HasMascot,
			Launched:     s.Launched,
			Enabled:      true,
			ButtonSet:    buttons,
			SubsBaseline: s.SubsBaseline,
		}
		if err := ms.UpsertChannel(ctx, ch); err != nil {
			return 0, fmt.Errorf("upsert channel %s: %w", s.Handle, err)
		}
	}
	return len(seeds), nil
}
