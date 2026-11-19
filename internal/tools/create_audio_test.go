package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

type countingSFXProvider struct {
	calls atomic.Int32
}

func (p *countingSFXProvider) Name() string { return "fake" }

func (p *countingSFXProvider) GenerateSFX(_ context.Context, _ audio.SFXOptions) (*audio.AudioResult, error) {
	p.calls.Add(1)
	return &audio.AudioResult{Audio: []byte("sfx"), Extension: "mp3", Provider: "fake"}, nil
}

// UpdateManager must route later Execute calls to the new manager, so a
// manager rebuilt after the startup DB overlay reaches the registered tool.
func TestCreateAudioTool_UpdateManagerSwapsManager(t *testing.T) {
	tool := NewCreateAudioTool(audio.NewManager(audio.ManagerConfig{}))
	ctx := WithToolWorkspace(context.Background(), t.TempDir())
	args := map[string]any{"prompt": "rain", "type": "sound_effect"}

	res := tool.Execute(ctx, args)
	if !res.IsError || !strings.Contains(res.ForLLM, "no sfx providers registered") {
		t.Fatalf("before swap: want no-provider error, got %q", res.ForLLM)
	}

	fake := &countingSFXProvider{}
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterSFX(fake)
	tool.UpdateManager(mgr)

	res = tool.Execute(ctx, args)
	if res.IsError {
		t.Fatalf("after swap: want success, got %q", res.ForLLM)
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("after swap: fake provider calls = %d, want 1", fake.calls.Load())
	}
}

// Swapping the manager while Execute runs must not race (run with -race).
func TestCreateAudioTool_UpdateManagerConcurrentWithExecute(t *testing.T) {
	fake := &countingSFXProvider{}
	newMgr := func() *audio.Manager {
		m := audio.NewManager(audio.ManagerConfig{})
		m.RegisterSFX(fake)
		return m
	}
	tool := NewCreateAudioTool(newMgr())
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			tool.UpdateManager(newMgr())
		}
	}()
	for range 50 {
		tool.Execute(ctx, map[string]any{"prompt": "rain", "type": "sound_effect"})
	}
	<-done
}
