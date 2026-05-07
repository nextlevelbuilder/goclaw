package channels

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// stubProvider records the last ChatRequest so tests can assert what
// system prompt + options got sent.
type stubProvider struct {
	mu      sync.Mutex
	lastReq providers.ChatRequest
	resp    *providers.ChatResponse
	respErr error
}

func (p *stubProvider) Name() string         { return "stub" }
func (p *stubProvider) DefaultModel() string { return "stub-model" }
func (p *stubProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.mu.Lock()
	p.lastReq = req
	p.mu.Unlock()
	return p.resp, p.respErr
}
func (p *stubProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.resp, p.respErr
}

// stubMemoryQueryer captures Search + Put calls.
type stubMemoryQueryer struct {
	mu          sync.Mutex
	searchCalls []string
	searchOut   []MemorySnippet
	searchFn    func(q string) ([]MemorySnippet, error)
	putPaths    []string
	putErr      error
}

func (m *stubMemoryQueryer) Search(_ context.Context, q, _, _ string, _ MemorySearchOpts) ([]MemorySnippet, error) {
	m.mu.Lock()
	m.searchCalls = append(m.searchCalls, q)
	fn := m.searchFn
	m.mu.Unlock()
	if fn != nil {
		return fn(q)
	}
	return m.searchOut, nil
}
func (m *stubMemoryQueryer) PutDocument(_ context.Context, _, _, path, _ string) error {
	m.mu.Lock()
	m.putPaths = append(m.putPaths, path)
	m.mu.Unlock()
	return m.putErr
}

func TestBuildVoiceTranscriptSummarizer_NilCfg(t *testing.T) {
	if BuildVoiceTranscriptSummarizer(nil) != nil {
		t.Error("expected nil for nil cfg")
	}
}

func TestBuildVoiceTranscriptSummarizer_BasicCall(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{Content: "  the summary  "}}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider: p,
		Model:    "stub-model",
	})
	got, err := fn(context.Background(), "alice: hi\nbob: hey")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "the summary" {
		t.Errorf("got %q, want trimmed summary", got)
	}
	if len(p.lastReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(p.lastReq.Messages))
	}
	if !strings.Contains(p.lastReq.Messages[0].Content, "summarizing a Discord voice channel") {
		t.Errorf("default system prompt not used: %q", p.lastReq.Messages[0].Content)
	}
}

func TestBuildVoiceTranscriptSummarizer_SkillBodyOverridesPrompt(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{Content: "ok"}}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider:  p,
		Model:     "stub-model",
		SkillBody: "Custom skill instructions for this team's vault.",
	})
	_, _ = fn(context.Background(), "alice: hi")
	got := p.lastReq.Messages[0].Content
	if !strings.HasPrefix(got, "Custom skill instructions") {
		t.Errorf("skill body not used as prompt; got: %q", got)
	}
	if !strings.Contains(got, "Do not call memory_search") {
		t.Errorf("non-negotiable no-tool guard missing from prompt: %q", got)
	}
}

func TestBuildVoiceTranscriptSummarizer_MemoryContextInjected(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{Content: "ok"}}
	mem := &stubMemoryQueryer{
		searchOut: []MemorySnippet{
			{Path: "wiki/contributors/alice.md", Snippet: "Alice is the lead engineer", Score: 0.9},
		},
	}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider:      p,
		Model:         "stub-model",
		MemoryStore:   mem,
		MemoryAgentID: "agent-uuid",
	})
	_, _ = fn(context.Background(), "alice: hi everyone\nbob: hey")
	got := p.lastReq.Messages[0].Content
	if !strings.Contains(got, "Memory context") || !strings.Contains(got, "Alice is the lead engineer") {
		t.Errorf("memory context not injected; got: %q", got)
	}
	// Must have searched for the transcript head + each speaker.
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if len(mem.searchCalls) < 2 {
		t.Errorf("expected at least 2 memory searches (topical + per-speaker); got %d: %v",
			len(mem.searchCalls), mem.searchCalls)
	}
}

func TestBuildVoiceTranscriptSummarizer_OrgContextLookupsInjected(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{Content: "ok"}}
	mem := &stubMemoryQueryer{
		searchFn: func(q string) ([]MemorySnippet, error) {
			switch {
			case strings.Contains(q, "controller-rs"):
				return []MemorySnippet{{Path: "memory/projects/controller-rs.md", Snippet: "controller-rs signs sessions for controller integrations.", Score: 0.95}}, nil
			case strings.Contains(q, "Starknet"):
				return []MemorySnippet{{Path: "memory/projects/starknet.md", Snippet: "Starknet is the target L2 ecosystem for these flows.", Score: 0.94}}, nil
			default:
				return nil, nil
			}
		},
	}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider:      p,
		Model:         "stub-model",
		MemoryStore:   mem,
		MemoryAgentID: "agent-uuid",
	})
	_, _ = fn(context.Background(), "alice: the repo controller-rs needs Starknet API context for the paymaster summary")
	got := p.lastReq.Messages[0].Content
	if !strings.Contains(got, "controller-rs signs sessions") {
		t.Fatalf("expected controller-rs context injected; prompt: %q", got)
	}
	if !strings.Contains(got, "Starknet is the target L2") {
		t.Fatalf("expected Starknet context injected; prompt: %q", got)
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if !containsSearchCall(mem.searchCalls, "controller-rs product project organization context") {
		t.Fatalf("expected controller-rs context lookup, got calls: %v", mem.searchCalls)
	}
	if !containsSearchCall(mem.searchCalls, "Starknet product project organization context") {
		t.Fatalf("expected Starknet context lookup, got calls: %v", mem.searchCalls)
	}
}

func Test_contextLookupQueries_extractsProjectLikeTerms(t *testing.T) {
	got := contextLookupQueries("alice: the repo katana needs controller-rs integration with Starknet API and GoClaw")
	if !containsString(got, "katana product project organization context") {
		t.Fatalf("expected cue-based lowercase project lookup, got %v", got)
	}
	if !containsString(got, "controller-rs product project organization context") {
		t.Fatalf("expected hyphenated project lookup, got %v", got)
	}
	if !containsString(got, "Starknet product project organization context") {
		t.Fatalf("expected capitalized project lookup, got %v", got)
	}
	if !containsString(got, "GoClaw product project organization context") {
		t.Fatalf("expected camel-case project lookup, got %v", got)
	}
}

func TestBuildVoiceTranscriptSummarizer_PersistsToMemoryAndDisk(t *testing.T) {
	tmp := t.TempDir()
	p := &stubProvider{resp: &providers.ChatResponse{Content: "the summary text"}}
	mem := &stubMemoryQueryer{}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider:         p,
		Model:            "stub-model",
		MemoryStore:      mem,
		MemoryAgentID:    "agent-uuid",
		SessionOutputDir: "voice-sessions",
		MemoryWorkspace:  tmp,
	})
	_, err := fn(context.Background(), "alice: hi\nbob: hey")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	if len(mem.putPaths) == 0 || !strings.HasPrefix(mem.putPaths[0], "memory/voice-sessions/") {
		t.Errorf("PutDocument not called with expected path; got: %v", mem.putPaths)
	}
	// File should also exist on disk.
	matches, _ := filepath.Glob(filepath.Join(tmp, "memory", "voice-sessions", "*", "*-session.md"))
	if len(matches) == 0 {
		t.Errorf("expected summary file on disk under %s", tmp)
	} else {
		body, _ := os.ReadFile(matches[0])
		if !strings.Contains(string(body), "the summary text") {
			t.Errorf("disk file missing summary body: %q", string(body))
		}
		if !strings.Contains(string(body), "type: voice-session") {
			t.Errorf("disk file missing frontmatter: %q", string(body))
		}
	}
}

func containsSearchCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBuildVoiceTranscriptSummarizer_EmptyTranscriptError(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{Content: "ok"}}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider: p,
		Model:    "stub-model",
	})
	_, err := fn(context.Background(), "   ")
	if err == nil || !strings.Contains(err.Error(), "empty transcript") {
		t.Errorf("expected empty transcript error, got %v", err)
	}
}

func TestBuildVoiceTranscriptSummarizer_ProviderErrorPropagates(t *testing.T) {
	p := &stubProvider{respErr: errors.New("boom")}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider: p,
		Model:    "stub-model",
	})
	_, err := fn(context.Background(), "alice: hi")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected wrapped err, got %v", err)
	}
}

func TestBuildVoiceTranscriptSummarizer_ToolCallResponseFallsBackToStats(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls:    []providers.ToolCall{{Name: "memory_search"}},
	}}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider: p,
		Model:    "stub-model",
	})
	got, err := fn(context.Background(), "alice: hi")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Fatalf("tool-call response should be discarded so caller falls back to stats, got %q", got)
	}
}

func TestBuildVoiceTranscriptSummarizer_DSMLToolMarkupFallsBackToStats(t *testing.T) {
	p := &stubProvider{resp: &providers.ChatResponse{Content: `<｜DSML｜tool_calls>
<｜DSML｜invoke name="memory_search">
<｜DSML｜parameter name="query" string="true">gabe cartridge contributor</｜DSML｜parameter>
</｜DSML｜invoke>
</｜DSML｜tool_calls>`}}
	fn := BuildVoiceTranscriptSummarizer(&VoiceTranscriptSummarizerConfig{
		Provider: p,
		Model:    "stub-model",
	})
	got, err := fn(context.Background(), "alice: hi")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Fatalf("DSML tool markup should be discarded so caller falls back to stats, got %q", got)
	}
}
