package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// fakeActivityIndicatorChannel is a minimal test implementation of ActivityIndicatorChannel.
// It records all OnActivityEvent calls for assertion in tests.
type fakeActivityIndicatorChannel struct {
	*BaseChannel
	mu              sync.Mutex
	calls           []ActivityEventCall // recorded calls with timestamps
	callTimes       []time.Time         // exact timing of each call
	shouldBeginFail bool                // if true, future calls return error
}

type ActivityEventCall struct {
	ChatID     string
	StatusCode string
	Timestamp  time.Time
}

func newFakeActivityIndicatorChannel(name string) *fakeActivityIndicatorChannel {
	return &fakeActivityIndicatorChannel{
		BaseChannel: NewBaseChannel(name, bus.New(), nil),
		calls:       []ActivityEventCall{},
		callTimes:   []time.Time{},
	}
}

func (c *fakeActivityIndicatorChannel) Start(context.Context) error {
	c.SetRunning(true)
	return nil
}

func (c *fakeActivityIndicatorChannel) Stop(context.Context) error {
	c.SetRunning(false)
	return nil
}

func (c *fakeActivityIndicatorChannel) Send(context.Context, bus.OutboundMessage) error {
	return nil
}

func (c *fakeActivityIndicatorChannel) OnActivityEvent(ctx context.Context, chatID, statusCode string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.calls = append(c.calls, ActivityEventCall{
		ChatID:     chatID,
		StatusCode: statusCode,
		Timestamp:  now,
	})
	c.callTimes = append(c.callTimes, now)

	if c.shouldBeginFail {
		return errors.New("activity indicator test error")
	}
	return nil
}

func (c *fakeActivityIndicatorChannel) GetCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *fakeActivityIndicatorChannel) GetCalls() []ActivityEventCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Return a copy to avoid race on inspection
	calls := make([]ActivityEventCall, len(c.calls))
	copy(calls, c.calls)
	return calls
}

// TestResolveToolActivityStatus_SearchTools tests substring mapping for search/web tools.
func TestResolveToolActivityStatus_SearchTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"web_search lowercase", "web_search", ActivityStatusSearching},
		{"Web_Search uppercase", "Web_Search", ActivityStatusSearching},
		{"browser tool", "browser_automation", ActivityStatusSearching},
		{"fetch_url", "fetch_url", ActivityStatusSearching},
		{"search_documents", "search_documents", ActivityStatusSearching},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != tt.want {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

// TestResolveToolActivityStatus_ReadDocTools tests file/memory/docs tools.
// Note: "vault_search" and similar tools containing "search" substring will match the
// search pattern first (order-dependent), so they return SEARCHING not READING_DOCS.
// This is expected behavior due to the switch case order in resolveToolActivityStatus.
func TestResolveToolActivityStatus_ReadDocTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"read_file", "read_file", ActivityStatusReadingDoc},
		{"list_files", "list_files", ActivityStatusReadingDoc},
		{"vault_only", "vault", ActivityStatusReadingDoc},
		{"memory_store", "memory_store", ActivityStatusReadingDoc},
		{"docs_retrieve", "docs_retrieve", ActivityStatusReadingDoc},
		{"skill_invoke", "skill_invoke", ActivityStatusReadingDoc},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != tt.want {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

// TestResolveToolActivityStatus_GeneratingTools tests image/tts/media generation tools.
func TestResolveToolActivityStatus_GeneratingTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"create_image", "create_image", ActivityStatusGenerating},
		{"image_edit", "image_edit", ActivityStatusGenerating},
		{"tts_speech", "tts_speech", ActivityStatusGenerating},
		{"speech_to_text", "speech_to_text", ActivityStatusGenerating},
		{"video_generator", "video_generator", ActivityStatusGenerating},
		{"music_compose", "music_compose", ActivityStatusGenerating},
		{"generate_diagram", "generate_diagram", ActivityStatusGenerating},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != tt.want {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

// TestResolveToolActivityStatus_ConnectingTools tests MCP/Bitrix/CRM tools.
// Note: tools with "search" in the name (like "crm_contact_search") will match the
// search pattern first due to order-dependent switch logic, returning SEARCHING instead.
// Only tools with mcp/bitrix/crm (without search) return CONNECTING.
func TestResolveToolActivityStatus_ConnectingTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"mcp_bx24_crm_deal_list", "mcp_bx24_crm_deal_list", ActivityStatusConnecting},
		{"bitrix_api", "bitrix_api", ActivityStatusConnecting},
		{"crm_direct_call", "crm_direct_call", ActivityStatusConnecting},
		{"mcp_custom_tool", "mcp_custom_tool", ActivityStatusConnecting},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != tt.want {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

// TestResolveToolActivityStatus_ProcessingTools tests delegation/subagent/team tools.
func TestResolveToolActivityStatus_ProcessingTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"delegate", "delegate", ActivityStatusProcessing},
		{"subagent_invoke", "subagent_invoke", ActivityStatusProcessing},
		{"team_create_task", "team_create_task", ActivityStatusProcessing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != tt.want {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

// TestResolveToolActivityStatus_UnknownTools tests default fallback for unmapped tools.
func TestResolveToolActivityStatus_UnknownTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
	}{
		{"unknown_tool", "unknown_tool"},
		{"foo_bar", "foo_bar"},
		{"empty_string", ""},
		{"single_char", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != ActivityStatusProcessing {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want default %q", tt.toolName, got, ActivityStatusProcessing)
			}
		})
	}
}

// TestResolveToolActivityStatus_CaseInsensitive tests case-insensitive matching.
func TestResolveToolActivityStatus_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		want     string
	}{
		{"MCP_BX24_CRM", "MCP_BX24_CRM", ActivityStatusConnecting},
		{"WEB_SEARCH", "WEB_SEARCH", ActivityStatusSearching},
		{"Read_File", "Read_File", ActivityStatusReadingDoc},
		{"CREATE_IMAGE", "CREATE_IMAGE", ActivityStatusGenerating},
		{"DELEGATE", "DELEGATE", ActivityStatusProcessing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveToolActivityStatus(tt.toolName)
			if got != tt.want {
				t.Errorf("resolveToolActivityStatus(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

// TestManagerFireActivity_Throttle tests that fireActivity respects the 5-second throttle.
func TestManagerFireActivity_Throttle(t *testing.T) {
	mgr := NewManager(bus.New())
	fakeChannel := newFakeActivityIndicatorChannel("test")

	// Create RunContext with all required fields
	rc := &RunContext{
		ChannelName:       "test",
		ChatID:            "chat123",
		MessageID:         "msg456",
		TenantID:          uuid.Nil,
		ToolStatusEnabled: true,
	}

	// First fireActivity call should succeed
	mgr.fireActivity(rc, fakeChannel, ActivityStatusThinking)

	// Wait a short time to ensure timestamp difference is meaningful
	time.Sleep(100 * time.Millisecond)

	// Second fireActivity call within 5s throttle window → should be dropped
	mgr.fireActivity(rc, fakeChannel, ActivityStatusSearching)

	// Give goroutines time to fire
	time.Sleep(100 * time.Millisecond)

	// Should only have 1 OnActivityEvent call due to throttle
	if fakeChannel.GetCallCount() != 1 {
		t.Errorf("expected 1 OnActivityEvent call after throttle, got %d", fakeChannel.GetCallCount())
	}

	// Manually advance lastActivityAt to bypass throttle
	rc.mu.Lock()
	rc.lastActivityAt = time.Now().Add(-time.Duration(activityThrottle) - 1*time.Second)
	rc.mu.Unlock()

	// Now fireActivity should succeed again
	mgr.fireActivity(rc, fakeChannel, ActivityStatusAnalyzing)

	// Give goroutines time to fire
	time.Sleep(100 * time.Millisecond)

	// Should now have 2 calls
	if fakeChannel.GetCallCount() != 2 {
		t.Errorf("expected 2 OnActivityEvent calls after throttle bypass, got %d", fakeChannel.GetCallCount())
	}

	// Verify second call has the new status
	calls := fakeChannel.GetCalls()
	if len(calls) > 1 && calls[1].StatusCode != ActivityStatusAnalyzing {
		t.Errorf("second call status = %q, want %q", calls[1].StatusCode, ActivityStatusAnalyzing)
	}
}

// TestManagerFireActivity_EmptyStatusResends tests that empty status re-sends current status.
func TestManagerFireActivity_EmptyStatusResend(t *testing.T) {
	mgr := NewManager(bus.New())
	fakeChannel := newFakeActivityIndicatorChannel("test")

	rc := &RunContext{
		ChannelName: "test",
		ChatID:      "chat123",
		MessageID:   "msg456",
		TenantID:    uuid.Nil,
	}

	// First call sets status
	mgr.fireActivity(rc, fakeChannel, ActivityStatusThinking)
	time.Sleep(100 * time.Millisecond)

	// Bypass throttle
	rc.mu.Lock()
	rc.lastActivityAt = time.Now().Add(-time.Duration(activityThrottle) - 1*time.Second)
	rc.mu.Unlock()

	// Second call with empty status should re-send current status
	mgr.fireActivity(rc, fakeChannel, "")
	time.Sleep(100 * time.Millisecond)

	calls := fakeChannel.GetCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(calls))
	}

	// Both calls should have the same status (first one)
	if calls[0].StatusCode != ActivityStatusThinking {
		t.Errorf("first call status = %q, want %q", calls[0].StatusCode, ActivityStatusThinking)
	}
	if calls[1].StatusCode != ActivityStatusThinking {
		t.Errorf("second call status = %q, want %q (re-sent)", calls[1].StatusCode, ActivityStatusThinking)
	}
}

// TestManagerStartActivityTicker_StartOnce tests that ticker is started only once.
func TestManagerStartActivityTicker_StartOnce(t *testing.T) {
	mgr := NewManager(bus.New())
	fakeChannel := newFakeActivityIndicatorChannel("test")

	rc := &RunContext{
		ChannelName: "test",
		ChatID:      "chat123",
		MessageID:   "msg456",
		TenantID:    uuid.Nil,
	}

	// First start should succeed
	mgr.startActivityTicker(rc, fakeChannel)

	rc.mu.Lock()
	firstTicker := rc.activityTicker
	firstStarted := rc.activityStarted
	rc.mu.Unlock()

	if !firstStarted {
		t.Errorf("first startActivityTicker should set activityStarted=true")
	}
	if firstTicker == nil {
		t.Errorf("first startActivityTicker should create a ticker")
	}

	// Second start should be idempotent (no-op)
	mgr.startActivityTicker(rc, fakeChannel)

	rc.mu.Lock()
	secondTicker := rc.activityTicker
	secondStarted := rc.activityStarted
	rc.mu.Unlock()

	if !secondStarted {
		t.Errorf("second startActivityTicker should keep activityStarted=true")
	}
	if secondTicker != firstTicker {
		t.Errorf("second startActivityTicker should reuse the same ticker, not create a new one")
	}
}

// TestManagerStopActivityTicker_Idempotent tests that stopActivityTicker can be called safely multiple times.
func TestManagerStopActivityTicker_Idempotent(t *testing.T) {
	mgr := NewManager(bus.New())
	fakeChannel := newFakeActivityIndicatorChannel("test")

	rc := &RunContext{
		ChannelName: "test",
		ChatID:      "chat123",
		MessageID:   "msg456",
		TenantID:    uuid.Nil,
	}

	// Start the ticker
	mgr.startActivityTicker(rc, fakeChannel)

	// First stop should succeed
	mgr.stopActivityTicker(rc)

	rc.mu.Lock()
	afterFirstStop := rc.activityTicker
	afterFirstStopChan := rc.activityStop
	rc.mu.Unlock()

	if afterFirstStop != nil {
		t.Errorf("first stopActivityTicker should clear ticker")
	}
	if afterFirstStopChan != nil {
		t.Errorf("first stopActivityTicker should clear stop channel")
	}

	// Second stop should also succeed (idempotent)
	mgr.stopActivityTicker(rc)

	// No panic = success

	// Third stop should also succeed
	mgr.stopActivityTicker(rc)

	// No panic = success
}

// TestManagerStopActivityTicker_NeverStarted tests that stopActivityTicker handles never-started case.
func TestManagerStopActivityTicker_NeverStarted(t *testing.T) {
	mgr := NewManager(bus.New())

	rc := &RunContext{
		ChannelName: "test",
		ChatID:      "chat123",
		MessageID:   "msg456",
		TenantID:    uuid.Nil,
	}

	// Never called startActivityTicker, just stop
	mgr.stopActivityTicker(rc)

	// Should not panic; no-op
	if rc.activityTicker != nil {
		t.Errorf("stopActivityTicker on never-started should be safe, but ticker is non-nil")
	}
}

// TestContainsAnySubstr_Match tests that containsAnySubstr finds substrings.
func TestContainsAnySubstr_Match(t *testing.T) {
	tests := []struct {
		name string
		s    string
		subs []string
		want bool
	}{
		{"first match", "hello world", []string{"hello"}, true},
		{"middle match", "hello world", []string{"lo wo"}, true},
		{"last match", "hello world", []string{"world"}, true},
		{"multi match first", "hello world", []string{"hello", "xor"}, true},
		{"multi match second", "hello world", []string{"xor", "world"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAnySubstr(tt.s, tt.subs...)
			if got != tt.want {
				t.Errorf("containsAnySubstr(%q, %v) = %v, want %v", tt.s, tt.subs, got, tt.want)
			}
		})
	}
}

// TestContainsAnySubstr_NoMatch tests that containsAnySubstr returns false when no substring matches.
func TestContainsAnySubstr_NoMatch(t *testing.T) {
	tests := []struct {
		name string
		s    string
		subs []string
	}{
		{"no match", "hello world", []string{"xor"}},
		{"empty list", "hello world", []string{}},
		{"empty string", "", []string{"hello"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAnySubstr(tt.s, tt.subs...)
			if got {
				t.Errorf("containsAnySubstr(%q, %v) = true, want false", tt.s, tt.subs)
			}
		})
	}
}

// TestManagerUnregisterRun_StopsActivityTicker is the C1 regression test: UnregisterRun
// (the safety-net cleanup used when a terminal AgentEvent never reaches HandleAgentEvent)
// MUST stop the heartbeat, otherwise the goroutine leaks and keeps firing
// InputAction.notify forever. Before the fix, UnregisterRun cleaned quick-ack/bubbles but
// not the activity ticker.
func TestManagerUnregisterRun_StopsActivityTicker(t *testing.T) {
	mgr := NewManager(bus.New())
	fakeChannel := newFakeActivityIndicatorChannel("test")

	rc := &RunContext{
		ChannelName: "test",
		ChatID:      "chat123",
		MessageID:   "msg456",
		TenantID:    uuid.Nil,
	}
	const runID = "run-c1"
	mgr.runs.Store(runID, rc)

	mgr.startActivityTicker(rc, fakeChannel)
	rc.mu.Lock()
	running := rc.activityTicker != nil
	rc.mu.Unlock()
	if !running {
		t.Fatalf("ticker should be running before UnregisterRun")
	}

	// Simulate the missed-terminal path: no run.completed event reaches
	// HandleAgentEvent, only the consumer's safety-net UnregisterRun fires.
	mgr.UnregisterRun(runID)

	rc.mu.Lock()
	ticker := rc.activityTicker
	stop := rc.activityStop
	rc.mu.Unlock()
	if ticker != nil {
		t.Errorf("UnregisterRun should stop the activity ticker to prevent goroutine/REST leak; got non-nil ticker")
	}
	if stop != nil {
		t.Errorf("UnregisterRun should clear the stop channel; got non-nil")
	}
}
