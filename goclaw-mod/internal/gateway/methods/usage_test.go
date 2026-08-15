package methods

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type usageSessionStore struct {
	store.SessionStore
	sessions []store.SessionInfoRich
	opts     []store.SessionListOpts
}

func (s *usageSessionStore) ListPagedRich(_ context.Context, opts store.SessionListOpts) store.SessionListRichResult {
	s.opts = append(s.opts, opts)
	return store.SessionListRichResult{Sessions: s.sessions, Total: len(s.sessions)}
}

type usageTracingStore struct {
	store.TracingStore
	costs map[string]float64
	keys  []string
}

func (s *usageTracingStore) GetSessionCosts(_ context.Context, sessionKeys []string) (map[string]float64, error) {
	s.keys = append(s.keys[:0], sessionKeys...)
	return s.costs, nil
}

func TestUsageGetIncludesTraceCost(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	sessions := &usageSessionStore{
		sessions: []store.SessionInfoRich{
			{
				SessionInfo:  store.SessionInfo{Key: "agent:cppai-pm:direct:user-a", Updated: now},
				Model:        "qwen3.7-plus",
				Provider:     "bailian",
				InputTokens:  1_200,
				OutputTokens: 300,
			},
			{
				SessionInfo:  store.SessionInfo{Key: "agent:itsddvn:direct:user-b", Updated: now.Add(-time.Minute)},
				Model:        "gpt-5.5",
				Provider:     "openai",
				InputTokens:  500,
				OutputTokens: 100,
			},
		},
	}
	tracing := &usageTracingStore{costs: map[string]float64{
		"agent:cppai-pm:direct:user-a": 0.0123,
		"agent:itsddvn:direct:user-b":  0.0045,
	}}
	methods := NewUsageMethods(sessions, tracing)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	methods.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodUsageGet, map[string]any{"limit": 1}))

	resp := readUsageResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	records, ok := payload["records"].([]any)
	if !ok {
		t.Fatalf("records type = %T", payload["records"])
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	record, ok := records[0].(map[string]any)
	if !ok {
		t.Fatalf("record type = %T", records[0])
	}
	if got, want := record["cost"], 0.0123; got != want {
		t.Fatalf("cost = %#v, want %#v", got, want)
	}
	if want := []string{"agent:cppai-pm:direct:user-a"}; !reflect.DeepEqual(tracing.keys, want) {
		t.Fatalf("cost keys = %#v, want %#v", tracing.keys, want)
	}
}

func readUsageResponse(t *testing.T, ch <-chan []byte) protocol.ResponseFrame {
	t.Helper()
	raw := <-ch
	var resp protocol.ResponseFrame
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}
