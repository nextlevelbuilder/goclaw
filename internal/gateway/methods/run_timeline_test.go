package methods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type stubRunTimelineStore struct {
	opts  []store.RunTimelineListOpts
	items []store.RunTimelineItem
	err   error
}

func (s *stubRunTimelineStore) AppendRunTimelineItem(context.Context, *store.RunTimelineItem) error {
	return nil
}

func (s *stubRunTimelineStore) ListRunTimelineItems(_ context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	s.opts = append(s.opts, opts)
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

func TestRunTimelineGetScopesViewerByUser(t *testing.T) {
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{
			{RunID: "run-1", UserID: "caller", Seq: 1, Preview: "visible"},
			{RunID: "run-1", UserID: "other", Seq: 2, Preview: "hidden"},
		},
	}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodRunTimelineGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	rawItems, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T", data["items"])
	}
	if len(rawItems) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(rawItems))
	}
	if timeline.opts[0].RunID != "run-1" {
		t.Fatalf("RunID = %q", timeline.opts[0].RunID)
	}
}

func TestRunTimelineGetRejectsNegativeOffset(t *testing.T) {
	timeline := &stubRunTimelineStore{}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodRunTimelineGet, map[string]any{
		"runId":  "run-1",
		"offset": -1,
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
	if len(timeline.opts) != 0 {
		t.Fatalf("store called with opts: %+v", timeline.opts)
	}
}

func TestRunTimelineGetHidesStoreErrorDetail(t *testing.T) {
	timeline := &stubRunTimelineStore{err: errors.New("pq: syntax error at or near \"OFFSET -1\"")}
	m := NewRunTimelineMethods(timeline, &config.Config{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)
	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodRunTimelineGet, map[string]any{"runId": "run-1"}))

	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInternal {
		t.Fatalf("error = %+v, want INTERNAL_ERROR", resp.Error)
	}
	if strings.Contains(resp.Error.Message, "OFFSET -1") || strings.Contains(resp.Error.Message, "pq:") {
		t.Fatalf("error leaked store detail: %q", resp.Error.Message)
	}
}

func readTimelineResponse(t *testing.T, ch <-chan []byte) protocol.ResponseFrame {
	t.Helper()
	raw := <-ch
	var resp protocol.ResponseFrame
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}
