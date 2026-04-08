package nodehost

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// --- SkillBinsCache tests ---

func TestSkillBinsCache_ReturnsCached(t *testing.T) {
	calls := 0
	// Use "sh" which exists in /usr/bin on all unix systems.
	cache := NewSkillBinsCache(func(_ context.Context) ([]string, error) {
		calls++
		return []string{"sh"}, nil
	}, "/usr/bin:/bin")

	bins1, _ := cache.Current(false)
	bins2, _ := cache.Current(false)

	if calls != 1 {
		t.Errorf("expected 1 fetch call, got %d", calls)
	}
	_ = bins1
	_ = bins2
	// Even if resolution fails on some platforms, the fetch should only be called once.
}

func TestSkillBinsCache_ForceRefresh(t *testing.T) {
	calls := 0
	cache := NewSkillBinsCache(func(_ context.Context) ([]string, error) {
		calls++
		return []string{"sh"}, nil
	}, "/usr/bin:/bin")

	cache.Current(false)
	cache.Current(true)

	if calls != 2 {
		t.Errorf("expected 2 fetch calls with force, got %d", calls)
	}
}

func TestSkillBinsCache_ThreadSafe(t *testing.T) {
	cache := NewSkillBinsCache(func(_ context.Context) ([]string, error) {
		time.Sleep(10 * time.Millisecond)
		return []string{"tsx"}, nil
	}, "/usr/bin")

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Current(false)
		}()
	}
	wg.Wait()
}

func TestSkillBinsCache_Deduplication(t *testing.T) {
	entries := resolveSkillBinTrustEntries([]string{"tsx", "tsx", ""}, "/nonexistent")
	// All should be empty since /nonexistent doesn't contain tsx.
	if len(entries) != 0 {
		t.Errorf("expected empty entries for missing bins, got %d", len(entries))
	}
}

// --- CoerceNodeInvokePayload tests ---

func TestCoercePayload_Valid(t *testing.T) {
	raw := json.RawMessage(`{"id":"inv-1","nodeId":"n1","command":"system.run","paramsJSON":"{\"command\":[\"echo\"]}"}`)
	p := CoerceNodeInvokePayload(raw)
	if p == nil {
		t.Fatal("expected non-nil payload")
	}
	if p.ID != "inv-1" || p.NodeID != "n1" || p.Command != "system.run" {
		t.Errorf("unexpected payload: %+v", p)
	}
	if p.ParamsJSON == nil || *p.ParamsJSON != `{"command":["echo"]}` {
		t.Errorf("paramsJSON mismatch: %v", p.ParamsJSON)
	}
}

func TestCoercePayload_MissingFields(t *testing.T) {
	tests := []string{
		`{}`,
		`{"id":"a"}`,
		`{"id":"a","nodeId":"b"}`,
		`not json`,
	}
	for _, raw := range tests {
		p := CoerceNodeInvokePayload(json.RawMessage(raw))
		if p != nil {
			t.Errorf("expected nil for %q, got %+v", raw, p)
		}
	}
}

func TestCoercePayload_ParamsObjectFallback(t *testing.T) {
	raw := json.RawMessage(`{"id":"inv-1","nodeId":"n1","command":"test","params":{"key":"val"}}`)
	p := CoerceNodeInvokePayload(raw)
	if p == nil {
		t.Fatal("expected non-nil")
	}
	if p.ParamsJSON == nil || *p.ParamsJSON != `{"key":"val"}` {
		t.Errorf("expected params object serialized to paramsJSON, got: %v", p.ParamsJSON)
	}
}

// --- Credential resolution tests ---

func TestResolveCredentials_FromEnv(t *testing.T) {
	t.Setenv("GOCLAW_GATEWAY_TOKEN", "my-token")
	t.Setenv("GOCLAW_GATEWAY_PASSWORD", "my-pass")
	creds := resolveGatewayCredentials()
	if creds.Token != "my-token" {
		t.Errorf("token = %q, want my-token", creds.Token)
	}
	if creds.Password != "my-pass" {
		t.Errorf("password = %q, want my-pass", creds.Password)
	}
}

func TestResolveCredentials_EmptyEnv(t *testing.T) {
	t.Setenv("GOCLAW_GATEWAY_TOKEN", "")
	t.Setenv("GOCLAW_GATEWAY_PASSWORD", "")
	creds := resolveGatewayCredentials()
	if creds.Token != "" || creds.Password != "" {
		t.Errorf("expected empty credentials, got: %+v", creds)
	}
}

// --- RPC dispatch routing tests ---

type mockRequester struct {
	mu       sync.Mutex
	requests []mockRequest
}

type mockRequest struct {
	Method string
	Params json.RawMessage
}

func (m *mockRequester) Request(_ context.Context, method string, params any) error {
	data, _ := json.Marshal(params)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, mockRequest{Method: method, Params: data})
	return nil
}

func TestDispatch_SystemWhich(t *testing.T) {
	client := &mockRequester{}
	paramsJSON := `{"bins":["echo","sh"]}`
	frame := NodeInvokeRequestPayload{
		ID: "inv-1", NodeID: "n1", Command: "system.which",
		ParamsJSON: &paramsJSON,
	}
	HandleInvoke(context.Background(), frame, client, nil)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) == 0 {
		t.Fatal("expected at least one request sent")
	}
	if client.requests[0].Method != "node.invoke.result" {
		t.Errorf("method = %q, want node.invoke.result", client.requests[0].Method)
	}
}

func TestDispatch_UnknownCommand(t *testing.T) {
	client := &mockRequester{}
	frame := NodeInvokeRequestPayload{
		ID: "inv-1", NodeID: "n1", Command: "unknown.command",
	}
	HandleInvoke(context.Background(), frame, client, nil)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) == 0 {
		t.Fatal("expected error response")
	}
	var result NodeInvokeResultParams
	json.Unmarshal(client.requests[0].Params, &result)
	if result.Ok {
		t.Error("expected ok=false for unknown command")
	}
}

// --- firstNonEmpty tests ---

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		values []string
		want   string
	}{
		{[]string{"", "", "c"}, "c"},
		{[]string{"a", "b"}, "a"},
		{[]string{" ", "b"}, "b"},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		got := firstNonEmpty(tt.values...)
		if got != tt.want {
			t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
		}
	}
}

// --- ensureNodePathEnv test ---

func TestEnsureNodePathEnv_PreservesExisting(t *testing.T) {
	t.Setenv("PATH", "/custom/bin")
	got := ensureNodePathEnv()
	if got != "/custom/bin" {
		t.Errorf("expected preserved PATH, got %q", got)
	}
}
