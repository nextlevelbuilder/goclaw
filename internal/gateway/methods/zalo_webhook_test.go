package methods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// fakeWebhookInstStore stubs ChannelInstanceStore for the webhook URL RPC.
// Only Get is exercised by this RPC.
type fakeWebhookInstStore struct {
	store.ChannelInstanceStore // embed for unimplemented defaults
	byID                       map[uuid.UUID]*store.ChannelInstanceData
	getCalls                   []uuid.UUID
}

func (f *fakeWebhookInstStore) Get(_ context.Context, id uuid.UUID) (*store.ChannelInstanceData, error) {
	f.getCalls = append(f.getCalls, id)
	inst, ok := f.byID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return inst, nil
}

func webhookReqFrame(t *testing.T, params map[string]any) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "req-1",
		Method: protocol.MethodChannelInstancesZaloWebhookURL,
		Params: raw,
	}
}

// readResp drains a single response frame from the capturing client's send
// channel. Fails the test if no frame is available.
func readResp(t *testing.T, ch <-chan []byte) *protocol.ResponseFrame {
	t.Helper()
	select {
	case raw := <-ch:
		var resp protocol.ResponseFrame
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal response: %v\nraw: %s", err, raw)
		}
		return &resp
	default:
		t.Fatal("no response frame written by handler")
		return nil
	}
}

func TestZaloWebhookURL_OAInstance_ReturnsPathAndHint(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	instID := uuid.New()
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{
		instID: {BaseModel: store.BaseModel{ID: instID}, TenantID: tenantID, ChannelType: channels.TypeZaloOA, Name: "My OA"},
	}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": instID.String()}))

	resp := readResp(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, _ := resp.Payload.(map[string]any)
	if payload == nil {
		t.Fatalf("nil result payload; resp=%+v", resp)
	}
	wantPath := "/channels/zalo/webhook/my-oa"
	if got, _ := payload["path"].(string); got != wantPath {
		t.Errorf("path = %q, want %q", got, wantPath)
	}
	if got, _ := payload["slug"].(string); got != "my-oa" {
		t.Errorf("slug = %q, want my-oa", got)
	}
	if got, _ := payload["instance_id"].(string); got != instID.String() {
		t.Errorf("instance_id = %q, want %q", got, instID.String())
	}
	if hint, _ := payload["hint"].(string); hint == "" {
		t.Error("hint should be non-empty (operator guidance)")
	}
}

func TestZaloWebhookURL_RespectsExplicitWebhookPath(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	instID := uuid.New()
	cfg := json.RawMessage(`{"webhook_path":"custom-slug"}`)
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{
		instID: {BaseModel: store.BaseModel{ID: instID}, TenantID: tenantID, ChannelType: channels.TypeZaloOA, Name: "Ignored Name", Config: cfg},
	}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": instID.String()}))

	resp := readResp(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, _ := resp.Payload.(map[string]any)
	if got, _ := payload["path"].(string); got != "/channels/zalo/webhook/custom-slug" {
		t.Errorf("path = %q, want /channels/zalo/webhook/custom-slug", got)
	}
}

func TestZaloWebhookURL_BotInstance_ReturnsPath(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	instID := uuid.New()
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{
		instID: {BaseModel: store.BaseModel{ID: instID}, TenantID: tenantID, ChannelType: channels.TypeZaloBot, Name: "support-bot"},
	}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": instID.String()}))

	resp := readResp(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, _ := resp.Payload.(map[string]any)
	wantPath := "/channels/zalo/webhook/support-bot"
	if got, _ := payload["path"].(string); got != wantPath {
		t.Errorf("path = %q, want %q", got, wantPath)
	}
}

func TestZaloWebhookURL_InvalidUUID_ReturnsInvalidRequest(t *testing.T) {
	t.Parallel()
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, uuid.New(), "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": "not-a-uuid"}))

	resp := readResp(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("error code = %+v, want %s", resp.Error, protocol.ErrInvalidRequest)
	}
	if len(fs.getCalls) != 0 {
		t.Errorf("store.Get called %d times; want 0 (early-return on bad UUID)", len(fs.getCalls))
	}
}

func TestZaloWebhookURL_UnknownInstance_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, uuid.New(), "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": uuid.New().String()}))

	resp := readResp(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("error code = %+v, want %s", resp.Error, protocol.ErrNotFound)
	}
}

func TestZaloWebhookURL_CrossTenant_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	clientTenant := uuid.New()
	otherTenant := uuid.New()
	instID := uuid.New()
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{
		instID: {BaseModel: store.BaseModel{ID: instID}, TenantID: otherTenant, ChannelType: channels.TypeZaloOA},
	}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, clientTenant, "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": instID.String()}))

	resp := readResp(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("error code = %+v, want %s (cross-tenant must not leak)", resp.Error, protocol.ErrNotFound)
	}
	// Defense-in-depth: error message must NOT include the instance UUID
	// (don't help an attacker confirm an instance exists in another tenant).
	if resp.Error != nil && strings.Contains(resp.Error.Message, instID.String()) {
		t.Errorf("error message leaks instance UUID: %q", resp.Error.Message)
	}
}

func TestZaloWebhookURL_WrongChannelType_ReturnsInvalidRequest(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	instID := uuid.New()
	fs := &fakeWebhookInstStore{byID: map[uuid.UUID]*store.ChannelInstanceData{
		instID: {BaseModel: store.BaseModel{ID: instID}, TenantID: tenantID, ChannelType: channels.TypeTelegram},
	}}
	m := NewZaloWebhookMethods(fs)
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "u")

	m.handleWebhookURL(context.Background(), client,
		webhookReqFrame(t, map[string]any{"instance_id": instID.String()}))

	resp := readResp(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("error code = %+v, want %s", resp.Error, protocol.ErrInvalidRequest)
	}
}
