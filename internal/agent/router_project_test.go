package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// mockProvider implements the Provider interface for testing.
type mockProvider struct{}

func (m *mockProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, req providers.ChatRequest, onChunk func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (m *mockProvider) DefaultModel() string { return "test-model" }
func (m *mockProvider) Name() string          { return "test-provider" }

// mockAgent implements the Agent interface for testing.
type mockAgent struct {
	id string
}

func (m *mockAgent) ID() string                                              { return m.id }
func (m *mockAgent) Run(_ context.Context, _ RunRequest) (*RunResult, error) { return nil, nil }
func (m *mockAgent) IsRunning() bool                                         { return false }
func (m *mockAgent) Model() string                                           { return "test-model" }
func (m *mockAgent) ProviderName() string                                    { return "test-provider" }
func (m *mockAgent) Provider() providers.Provider                            { return &mockProvider{} }

func TestGetForProject_EmptyProjectFallsBackToGet(t *testing.T) {
	r := NewRouter()
	r.SetResolver(func(agentKey string, opts ResolveOpts) (Agent, error) {
		return &mockAgent{id: agentKey}, nil
	})

	ag, err := r.GetForProject("sdlc-assistant", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ag.ID() != "sdlc-assistant" {
		t.Errorf("got agent ID %q, want 'sdlc-assistant'", ag.ID())
	}
}

func TestGetForProject_DifferentProjectsSeparateCache(t *testing.T) {
	callCount := 0

	r := NewRouter()
	r.SetResolver(func(agentKey string, opts ResolveOpts) (Agent, error) {
		callCount++
		return &mockAgent{id: agentKey + ":" + opts.ProjectID}, nil
	})

	ag1, err := r.GetForProject("sdlc-assistant", "uuid-xpos", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ag2, err := r.GetForProject("sdlc-assistant", "uuid-payment", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ag1.ID() == ag2.ID() {
		t.Error("different projects should get different cached agents")
	}
	if callCount != 2 {
		t.Errorf("resolver should be called twice (once per project), got %d", callCount)
	}
}

func TestGetForProject_SameProjectUsesCache(t *testing.T) {
	callCount := 0

	r := NewRouter()
	r.SetResolver(func(agentKey string, opts ResolveOpts) (Agent, error) {
		callCount++
		return &mockAgent{id: agentKey}, nil
	})

	_, _ = r.GetForProject("sdlc-assistant", "uuid-xpos", nil)
	_, _ = r.GetForProject("sdlc-assistant", "uuid-xpos", nil)

	if callCount != 1 {
		t.Errorf("resolver should be called once (cached), got %d", callCount)
	}
}

func TestGetForProject_NoProjectAndWithProject_SeparateCache(t *testing.T) {
	callCount := 0

	r := NewRouter()
	r.SetResolver(func(agentKey string, opts ResolveOpts) (Agent, error) {
		callCount++
		suffix := ""
		if opts.ProjectID != "" {
			suffix = ":" + opts.ProjectID
		}
		return &mockAgent{id: agentKey + suffix}, nil
	})

	ag1, _ := r.GetForProject("sdlc-assistant", "", nil)
	ag2, _ := r.GetForProject("sdlc-assistant", "uuid-xpos", nil)

	if ag1.ID() == ag2.ID() {
		t.Error("no-project and with-project should get different agents")
	}
	if callCount != 2 {
		t.Errorf("expected 2 resolver calls, got %d", callCount)
	}
}
