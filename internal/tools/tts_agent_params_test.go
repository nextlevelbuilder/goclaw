package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tts"
)

// stubProvider is a test TTS provider that captures the last opts it received.
type stubProvider struct {
	name      string
	failUntil int   // fail the first N calls, succeed thereafter
	calls     int
	lastOpts  tts.Options
	shouldErr bool // if true, always fail
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Synthesize(_ context.Context, _ string, opts tts.Options) (*tts.SynthResult, error) {
	s.calls++
	s.lastOpts = opts
	if s.shouldErr || s.calls <= s.failUntil {
		return nil, errors.New("stub: synthesize failed")
	}
	return &tts.SynthResult{Audio: []byte("audio"), Extension: "mp3"}, nil
}

// buildSnapCtx injects an AgentAudioSnapshot with the given otherConfig JSON
// into a background context — mirrors how dispatch.go wires ctx before tool
// execution.
func buildSnapCtx(t *testing.T, agentID uuid.UUID, otherConfig map[string]any) context.Context {
	t.Helper()
	raw, err := json.Marshal(otherConfig)
	if err != nil {
		t.Fatalf("marshal other_config: %v", err)
	}
	snap := store.AgentAudioSnapshot{AgentID: agentID, OtherConfig: raw}
	return store.WithAgentAudio(context.Background(), snap)
}

// TestTtsTool_NoTTSParams_NoBehaviorChange is the characterization test:
// when other_config has no tts_params, opts.Params must remain nil.
func TestTtsTool_NoTTSParams_NoBehaviorChange(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{name: "openai"}
	mgr := tts.NewManager(tts.ManagerConfig{Primary: "openai"})
	mgr.RegisterTTS(stub)

	tool := NewTtsTool(mgr)

	agentID := uuid.New()
	// other_config without tts_params
	ctx := buildSnapCtx(t, agentID, map[string]any{
		"tts_voice_id": "alloy",
	})

	result := tool.Execute(ctx, map[string]any{"text": "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	// Opts.Params must be nil — no agent params applied.
	if len(stub.lastOpts.Params) != 0 {
		t.Errorf("expected empty opts.Params, got %v", stub.lastOpts.Params)
	}
}

// TestTtsTool_TTSParams_MiniMax verifies that generic tts_params are adapted
// to MiniMax-native keys before the Synthesize call.
func TestTtsTool_TTSParams_MiniMax(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{name: "minimax"}
	mgr := tts.NewManager(tts.ManagerConfig{Primary: "minimax"})
	mgr.RegisterTTS(stub)

	tool := NewTtsTool(mgr)

	agentID := uuid.New()
	ctx := buildSnapCtx(t, agentID, map[string]any{
		"tts_params": map[string]any{
			"speed":   1.1,
			"emotion": "happy",
		},
	})

	result := tool.Execute(ctx, map[string]any{"text": "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	// MiniMax uses flat "speed" and "emotion" (same as generic keys).
	if stub.lastOpts.Params["speed"] != 1.1 {
		t.Errorf("want speed=1.1, got %v", stub.lastOpts.Params["speed"])
	}
	if stub.lastOpts.Params["emotion"] != "happy" {
		t.Errorf("want emotion=happy, got %v", stub.lastOpts.Params["emotion"])
	}
}

// TestTtsTool_TTSParams_ElevenLabs verifies that generic tts_params are
// adapted to ElevenLabs nested keys.
func TestTtsTool_TTSParams_ElevenLabs(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{name: "elevenlabs"}
	mgr := tts.NewManager(tts.ManagerConfig{Primary: "elevenlabs"})
	mgr.RegisterTTS(stub)

	tool := NewTtsTool(mgr)

	agentID := uuid.New()
	ctx := buildSnapCtx(t, agentID, map[string]any{
		"tts_params": map[string]any{
			"speed": 1.0,
			"style": 0.3,
		},
	})

	result := tool.Execute(ctx, map[string]any{"text": "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	// ElevenLabs uses nested keys.
	if stub.lastOpts.Params["voice_settings.speed"] != 1.0 {
		t.Errorf("want voice_settings.speed=1.0, got %v", stub.lastOpts.Params["voice_settings.speed"])
	}
	if stub.lastOpts.Params["voice_settings.style"] != 0.3 {
		t.Errorf("want voice_settings.style=0.3, got %v", stub.lastOpts.Params["voice_settings.style"])
	}
	// Emotion should NOT appear (not supported by ElevenLabs).
	if _, ok := stub.lastOpts.Params["emotion"]; ok {
		t.Errorf("emotion should not appear in ElevenLabs opts")
	}
}

// TestTtsTool_TTSParams_Finding1_FallbackGetsOwnAdaptation is the critical
// Finding #1 regression test: when primary ElevenLabs fails, MiniMax fallback
// must receive flat "speed" (NOT "voice_settings.speed").
func TestTtsTool_TTSParams_Finding1_FallbackGetsOwnAdaptation(t *testing.T) {
	t.Parallel()

	// Primary ElevenLabs always fails.
	elStub := &stubProvider{name: "elevenlabs", shouldErr: true}
	// Fallback MiniMax succeeds.
	mmStub := &stubProvider{name: "minimax"}

	mgr := tts.NewManager(tts.ManagerConfig{Primary: "elevenlabs"})
	mgr.RegisterTTS(elStub)
	mgr.RegisterTTS(mmStub)

	tool := NewTtsTool(mgr)

	agentID := uuid.New()
	ctx := buildSnapCtx(t, agentID, map[string]any{
		"tts_params": map[string]any{
			"speed": 1.2,
		},
	})

	result := tool.Execute(ctx, map[string]any{"text": "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	// MiniMax must have been called and received flat "speed", NOT "voice_settings.speed".
	if mmStub.calls == 0 {
		t.Fatal("MiniMax fallback was never called")
	}
	if mmStub.lastOpts.Params["speed"] != 1.2 {
		t.Errorf("want MiniMax speed=1.2, got %v", mmStub.lastOpts.Params["speed"])
	}
	if _, hasNested := mmStub.lastOpts.Params["voice_settings.speed"]; hasNested {
		t.Errorf("MiniMax must not receive voice_settings.speed (ElevenLabs-native key bleed)")
	}
}

// TestTtsTool_TTSParams_OpenAI verifies speed passes through and emotion/style dropped.
func TestTtsTool_TTSParams_OpenAI(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{name: "openai"}
	mgr := tts.NewManager(tts.ManagerConfig{Primary: "openai"})
	mgr.RegisterTTS(stub)

	tool := NewTtsTool(mgr)

	agentID := uuid.New()
	ctx := buildSnapCtx(t, agentID, map[string]any{
		"tts_params": map[string]any{
			"speed":   1.5,
			"emotion": "happy", // unsupported by openai — should be dropped
		},
	})

	result := tool.Execute(ctx, map[string]any{"text": "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	if stub.lastOpts.Params["speed"] != 1.5 {
		t.Errorf("want speed=1.5, got %v", stub.lastOpts.Params["speed"])
	}
	if _, ok := stub.lastOpts.Params["emotion"]; ok {
		t.Errorf("emotion should be dropped for openai, got %v", stub.lastOpts.Params["emotion"])
	}
}

// TestTtsTool_TTSParams_Gemini_AllDropped verifies Gemini receives no agent overrides.
func TestTtsTool_TTSParams_Gemini_AllDropped(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{name: "gemini"}
	mgr := tts.NewManager(tts.ManagerConfig{Primary: "gemini"})
	mgr.RegisterTTS(stub)

	tool := NewTtsTool(mgr)

	agentID := uuid.New()
	ctx := buildSnapCtx(t, agentID, map[string]any{
		"tts_params": map[string]any{
			"speed":   1.0,
			"emotion": "happy",
			"style":   0.5,
		},
	})

	result := tool.Execute(ctx, map[string]any{"text": "hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	if len(stub.lastOpts.Params) != 0 {
		t.Errorf("Gemini should receive no agent override params, got %v", stub.lastOpts.Params)
	}
}

// TestAdaptAgentParams_MaybeApply verifies manager_auto.go path via Manager.MaybeApply:
// agent tts_params must be applied when auto=always.
func TestAdaptAgentParams_MaybeApply(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{name: "minimax"}
	mgr := audio.NewManager(audio.ManagerConfig{
		Primary: "minimax",
		Auto:    audio.AutoAlways,
	})
	mgr.RegisterTTS(stub)

	agentID := uuid.New()
	raw, _ := json.Marshal(map[string]any{
		"tts_params": map[string]any{"speed": 0.8, "emotion": "excited"},
	})
	snap := store.AgentAudioSnapshot{AgentID: agentID, OtherConfig: raw}
	ctx := store.WithAgentAudio(context.Background(), snap)

	res, ok := mgr.MaybeApply(ctx, "Hello, this is a test sentence for TTS synthesis.", "", false, "final")
	if !ok {
		t.Fatal("MaybeApply returned ok=false")
	}
	if res == nil {
		t.Fatal("MaybeApply returned nil result")
	}
	// Verify stub received the adapted params.
	if stub.lastOpts.Params["speed"] != 0.8 {
		t.Errorf("want speed=0.8 in MiniMax, got %v", stub.lastOpts.Params["speed"])
	}
	if stub.lastOpts.Params["emotion"] != "excited" {
		t.Errorf("want emotion=excited in MiniMax, got %v", stub.lastOpts.Params["emotion"])
	}
}
