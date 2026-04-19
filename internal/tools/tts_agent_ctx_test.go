package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ctxWithAgentAudio returns a context carrying an AgentAudioSnapshot whose
// OtherConfig has the given tts_voice_id / tts_model_id keys (either may be
// empty — empty means "not set by agent config").
func ctxWithAgentAudio(t *testing.T, voiceID, modelID string) context.Context {
	t.Helper()
	cfg := map[string]string{}
	if voiceID != "" {
		cfg["tts_voice_id"] = voiceID
	}
	if modelID != "" {
		cfg["tts_model_id"] = modelID
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store.WithAgentAudio(context.Background(), store.AgentAudioSnapshot{
		AgentID:     uuid.New(),
		OtherConfig: raw,
	})
}

func TestResolveVoiceAndModel_ArgsWinOverAgent(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("elevenlabs"))
	ctx := ctxWithAgentAudio(t, "AGENT_V", "AGENT_M")
	v, m := tool.resolveVoiceAndModel(ctx, "ARG_V", "ARG_M")
	if v != "ARG_V" {
		t.Errorf("voice: got %q, want ARG_V (args must win)", v)
	}
	if m != "ARG_M" {
		t.Errorf("model: got %q, want ARG_M (args must win)", m)
	}
}

func TestResolveVoiceAndModel_AgentWinsOverTenantWhenArgsEmpty(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("elevenlabs"))
	ctx := ctxWithAgentAudio(t, "AGENT_V", "AGENT_M")
	ctx = WithBuiltinToolSettings(ctx, BuiltinToolSettings{
		"tts": rawJSON(t, map[string]string{"default_voice_id": "TENANT_V", "default_model": "TENANT_M"}),
	})
	v, m := tool.resolveVoiceAndModel(ctx, "", "")
	if v != "AGENT_V" {
		t.Errorf("voice: got %q, want AGENT_V (agent > tenant)", v)
	}
	if m != "AGENT_M" {
		t.Errorf("model: got %q, want AGENT_M (agent > tenant)", m)
	}
}

func TestResolveVoiceAndModel_TenantFallbackWhenAgentSilent(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("elevenlabs"))
	// No agent audio ctx, only tenant settings.
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"tts": rawJSON(t, map[string]string{"default_voice_id": "TENANT_V", "default_model": "TENANT_M"}),
	})
	v, m := tool.resolveVoiceAndModel(ctx, "", "")
	if v != "TENANT_V" {
		t.Errorf("voice: got %q, want TENANT_V", v)
	}
	if m != "TENANT_M" {
		t.Errorf("model: got %q, want TENANT_M", m)
	}
}

func TestResolveVoiceAndModel_EmptyAllMeansDefault(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("elevenlabs"))
	v, m := tool.resolveVoiceAndModel(context.Background(), "", "")
	if v != "" {
		t.Errorf("voice: got %q, want empty (no sources → provider default)", v)
	}
	if m != "" {
		t.Errorf("model: got %q, want empty (no sources → provider default)", m)
	}
}

func TestResolveVoiceAndModel_PartialAgentConfig(t *testing.T) {
	tool := NewTtsTool(makeTTSManager("elevenlabs"))
	// Agent only provides voice; model must fall to tenant.
	ctx := ctxWithAgentAudio(t, "AGENT_V", "")
	ctx = WithBuiltinToolSettings(ctx, BuiltinToolSettings{
		"tts": rawJSON(t, map[string]string{"default_model": "TENANT_M"}),
	})
	v, m := tool.resolveVoiceAndModel(ctx, "", "")
	if v != "AGENT_V" {
		t.Errorf("voice: got %q, want AGENT_V", v)
	}
	if m != "TENANT_M" {
		t.Errorf("model: got %q, want TENANT_M (agent silent → tenant)", m)
	}
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
