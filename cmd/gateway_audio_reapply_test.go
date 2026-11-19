package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// newAudioToolsRegistry registers the tts and create_audio tools on a manager
// built from cfg, the way setupToolRegistry does before the DB overlay.
func newAudioToolsRegistry(cfg *config.Config) (*tools.Registry, *tools.TtsTool) {
	mgr := buildAudioManager(cfg)
	reg := tools.NewRegistry()
	reg.Register(tools.NewCreateAudioTool(mgr))
	ttsTool := tools.NewTtsTool(mgr)
	reg.Register(ttsTool)
	return reg, ttsTool
}

// A TTS provider configured only through DB secrets and system_configs must be
// registered and become primary once the manager is rebuilt after the overlay.
// Before the rebuild the manager only knows the file config, so Edge (always
// registered) is primary.
func TestReapplyAudioManager_ProviderPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		file        func(*config.Config)
		secrets     map[string]string
		sysConfigs  map[string]string
		wantPrimary string
		wantMiniMax bool
	}{
		{
			name: "dashboard-only provider becomes primary",
			secrets: map[string]string{
				"tts.minimax.api_key":  "test-key",
				"tts.minimax.group_id": "test-group",
			},
			sysConfigs:  map[string]string{"tts.provider": "minimax"},
			wantPrimary: "minimax",
			wantMiniMax: true,
		},
		{
			name:        "no DB values keeps edge",
			wantPrimary: "edge",
		},
		{
			name: "file provider wins when DB leaves tts.provider empty",
			file: func(c *config.Config) {
				c.Tts.Provider = "minimax"
				c.Tts.MiniMax.APIKey = "file-key"
			},
			sysConfigs:  map[string]string{"tts.provider": "", "tts.auto": "always"},
			wantPrimary: "minimax",
			wantMiniMax: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			if tc.file != nil {
				tc.file(cfg)
			}
			reg, ttsTool := newAudioToolsRegistry(cfg)

			cfg.ApplyDBSecrets(tc.secrets)
			cfg.ApplySystemConfigs(tc.sysConfigs)
			mgr := reapplyAudioManager(cfg, reg, ttsTool)

			if got := mgr.PrimaryProvider(); got != tc.wantPrimary {
				t.Fatalf("primary = %q, want %q", got, tc.wantPrimary)
			}
			if _, ok := mgr.GetProvider("minimax"); ok != tc.wantMiniMax {
				t.Fatalf("minimax registered = %v, want %v", ok, tc.wantMiniMax)
			}
			if _, ok := mgr.GetProvider("edge"); !ok {
				t.Fatal("edge must stay registered as fallback")
			}
		})
	}
}

// The rebuilt manager must reach the registered tts tool: a provider whose key
// arrives only from DB secrets is selectable by name after the rebuild.
func TestReapplyAudioManager_SwapsTtsToolManager(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "test server", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Tts.MiniMax.APIBase = srv.URL
	reg, ttsTool := newAudioToolsRegistry(cfg)
	args := map[string]any{"text": "hello", "provider": "minimax"}

	res := ttsTool.Execute(context.Background(), args)
	if !res.IsError || hits.Load() != 0 {
		t.Fatalf("before rebuild: want provider-not-found without a request, got %q (hits=%d)", res.ForLLM, hits.Load())
	}

	cfg.ApplyDBSecrets(map[string]string{"tts.minimax.api_key": "test-key"})
	reapplyAudioManager(cfg, reg, ttsTool)

	ttsTool.Execute(context.Background(), args)
	if hits.Load() == 0 {
		t.Fatal("after rebuild: tts tool did not call the minimax provider")
	}
}

// An ElevenLabs key from DB secrets also enables the SFX and Music providers,
// and the registered create_audio tool must see them after the rebuild.
func TestReapplyAudioManager_SwapsCreateAudioManager(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "test server", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Tts.ElevenLabs.BaseURL = srv.URL
	reg, ttsTool := newAudioToolsRegistry(cfg)
	createAudio, ok := reg.Get("create_audio")
	if !ok {
		t.Fatal("create_audio not registered")
	}

	for _, kind := range []string{"sound_effect", "music"} {
		res := createAudio.Execute(context.Background(), map[string]any{"prompt": "rain", "type": kind})
		if !res.IsError || hits.Load() != 0 {
			t.Fatalf("%s before rebuild: want no-provider error without a request, got %q (hits=%d)", kind, res.ForLLM, hits.Load())
		}
	}

	cfg.ApplyDBSecrets(map[string]string{"tts.elevenlabs.api_key": "test-key"})
	mgr := reapplyAudioManager(cfg, reg, ttsTool)
	if _, ok := mgr.GetProvider("elevenlabs"); !ok {
		t.Fatal("elevenlabs TTS not registered after rebuild")
	}

	for _, kind := range []string{"sound_effect", "music"} {
		before := hits.Load()
		createAudio.Execute(context.Background(), map[string]any{"prompt": "rain", "type": kind})
		if hits.Load() == before {
			t.Fatalf("%s after rebuild: create_audio did not call the elevenlabs provider", kind)
		}
	}
}
