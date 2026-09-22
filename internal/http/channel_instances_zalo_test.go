package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestMaskInstanceHTTPIncludesDerivedZaloOAWebhookURL(t *testing.T) {
	h := &ChannelInstancesHandler{publicURLSource: func() string { return "https://gateway.example/" }}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		Name:        "sales-zalo",
		ChannelType: channels.TypeZaloOA,
	})

	want := "https://gateway.example/channels/zalo/webhook/sales-zalo"
	if got, _ := result["webhook_url"].(string); got != want {
		t.Fatalf("webhook_url = %q, want %q", got, want)
	}
	if got, want := result["callback_url"], "https://gateway.example/oauth/zalo/callback"; got != want {
		t.Fatalf("callback_url = %q, want %q", got, want)
	}
}

func TestMaskInstanceHTTPPreservesConfiguredZaloOAWebhookSlug(t *testing.T) {
	cfg, err := json.Marshal(map[string]any{"webhook_path": "registered-slug"})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	h := &ChannelInstancesHandler{publicURLSource: func() string { return "https://gateway.example" }}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		Name:        "new-name",
		ChannelType: channels.TypeZaloOA,
		Config:      cfg,
	})

	want := "https://gateway.example/channels/zalo/webhook/registered-slug"
	if got, _ := result["webhook_url"].(string); got != want {
		t.Fatalf("webhook_url = %q, want %q", got, want)
	}
}

func TestMaskInstanceHTTPPreservesConfiguredZaloOACallbackURL(t *testing.T) {
	creds := json.RawMessage(`{"app_id":"app","secret_key":"secret","redirect_uri":"https://registered.example/zalo"}`)
	h := &ChannelInstancesHandler{publicURLSource: func() string { return "https://gateway.example" }}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		Name:        "sales-zalo",
		ChannelType: channels.TypeZaloOA,
		Credentials: creds,
	})

	want := "https://registered.example/zalo"
	if got, _ := result["callback_url"].(string); got != want {
		t.Fatalf("callback_url = %q, want %q", got, want)
	}
}

func TestMaskInstanceHTTPExposesZaloOAAppIDAndMasksSecrets(t *testing.T) {
	creds := json.RawMessage(`{"app_id":"3274591538521654651","secret_key":"oauth-secret","webhook_secret_key":"webhook-secret"}`)
	h := &ChannelInstancesHandler{}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		ChannelType: channels.TypeZaloOA,
		Credentials: creds,
	})

	masked, ok := result["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("credentials type = %T, want map[string]any", result["credentials"])
	}
	if got, want := masked["app_id"], "3274591538521654651"; got != want {
		t.Fatalf("app_id = %v, want %q", got, want)
	}
	if got, want := masked["secret_key"], "***"; got != want {
		t.Fatalf("secret_key = %v, want %q", got, want)
	}
	if got, want := masked["webhook_secret_key"], "***"; got != want {
		t.Fatalf("webhook_secret_key = %v, want %q", got, want)
	}
}

func TestMaskInstanceHTTPExposesZaloOAConnectionStateWithoutTokens(t *testing.T) {
	h := &ChannelInstancesHandler{}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		ChannelType: channels.TypeZaloOA,
		Credentials: json.RawMessage(`{"app_id":"app","secret_key":"secret"}`),
	})
	if got, ok := result["auth_connected"].(bool); !ok || got {
		t.Fatalf("auth_connected = %#v, want false", result["auth_connected"])
	}
}

func TestMaskInstanceHTTPExposesZaloOAConnectionStateWithTokens(t *testing.T) {
	h := &ChannelInstancesHandler{}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		ChannelType: channels.TypeZaloOA,
		Credentials: json.RawMessage(`{"app_id":"app","secret_key":"secret","access_token":"access","refresh_token":"refresh"}`),
	})
	if got, ok := result["auth_connected"].(bool); !ok || !got {
		t.Fatalf("auth_connected = %#v, want true", result["auth_connected"])
	}
}

func TestMaskInstanceHTTPOmitsWebhookURLWithoutPublicBase(t *testing.T) {
	h := &ChannelInstancesHandler{publicURLSource: func() string { return "" }}
	result := h.maskInstanceHTTP(store.ChannelInstanceData{
		Name:        "sales-zalo",
		ChannelType: channels.TypeZaloOA,
	})
	if _, ok := result["webhook_url"]; ok {
		t.Fatal("webhook_url must be omitted when the public base URL is unavailable")
	}
	if _, ok := result["callback_url"]; ok {
		t.Fatal("callback_url must be omitted when the public base URL is unavailable")
	}
}

func TestHandleGetObservesPublicURLBeforeRenderingZaloOAWebhookURL(t *testing.T) {
	id := uuid.New()
	publicURL := ""
	h := NewChannelInstancesHandler(&stubChannelInstanceStore{inst: &store.ChannelInstanceData{
		BaseModel:   store.BaseModel{ID: id},
		Name:        "sales-zalo",
		ChannelType: channels.TypeZaloOA,
	}}, nil, nil, nil, nil, nil)
	h.SetPublicURLSource(
		func() string { return publicURL },
		func(*http.Request) string {
			publicURL = "https://gateway.example"
			return publicURL
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/instances/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rr := httptest.NewRecorder()
	h.handleGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "https://gateway.example/channels/zalo/webhook/sales-zalo"
	if got, _ := response["webhook_url"].(string); got != want {
		t.Fatalf("webhook_url = %q, want %q", got, want)
	}
	if got, want := response["callback_url"], "https://gateway.example/oauth/zalo/callback"; got != want {
		t.Fatalf("callback_url = %q, want %q", got, want)
	}
}

func TestHandleZaloOASetupPreviewReturnsCreateTimeURLs(t *testing.T) {
	h := &ChannelInstancesHandler{
		publicURLSource: func() string { return "https://gateway.example/" },
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/channels/setup/zalo-oa?name=sales-zalo", nil)
	rr := httptest.NewRecorder()

	h.handleZaloOASetupPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := response["callback_url"], "https://gateway.example/oauth/zalo/callback"; got != want {
		t.Fatalf("callback_url = %q, want %q", got, want)
	}
	if got, want := response["webhook_url"], "https://gateway.example/channels/zalo/webhook/sales-zalo"; got != want {
		t.Fatalf("webhook_url = %q, want %q", got, want)
	}
}

func TestHandleZaloOASetupPreviewPrimesWebhookVerification(t *testing.T) {
	name := "setup-probe-" + uuid.NewString()
	h := &ChannelInstancesHandler{
		publicURLSource: func() string { return "https://gateway.example/" },
	}
	previewReq := httptest.NewRequest(http.MethodGet, "/v1/channels/setup/zalo-oa?name="+name, nil)
	previewRR := httptest.NewRecorder()
	h.handleZaloOASetupPreview(previewRR, previewReq)
	if previewRR.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", previewRR.Code, previewRR.Body.String())
	}

	webhookReq := httptest.NewRequest(http.MethodPost, common.WebhookPathPrefix+name,
		strings.NewReader(`{"event_name":"oa_webhook_verify"}`))
	webhookRR := httptest.NewRecorder()
	common.SharedRouter().ServeHTTP(webhookRR, webhookReq)
	if webhookRR.Code != http.StatusOK {
		t.Fatalf("verification status = %d, want 200; body=%s", webhookRR.Code, webhookRR.Body.String())
	}
}

func TestHandleZaloOASetupPreviewRejectsMissingName(t *testing.T) {
	h := &ChannelInstancesHandler{
		publicURLSource: func() string { return "https://gateway.example" },
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/channels/setup/zalo-oa", nil)
	rr := httptest.NewRecorder()

	h.handleZaloOASetupPreview(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestHandleZaloOAConsentReturnsHTTPFlowPayload(t *testing.T) {
	tenantID := uuid.New()
	instanceID := uuid.New()
	observedPublicURL := false
	h := &ChannelInstancesHandler{
		publicURLUpdate: func(*http.Request) string {
			observedPublicURL = true
			return "https://gateway.example"
		},
		zaloOAConsent: func(_ context.Context, gotTenant uuid.UUID, gotID string) (map[string]any, *ZaloOAFlowError) {
			if gotTenant != tenantID || gotID != instanceID.String() {
				t.Fatalf("flow scope = (%s, %q), want (%s, %q)", gotTenant, gotID, tenantID, instanceID)
			}
			return map[string]any{
				"url":   "https://oauth.zaloapp.com/v4/oa/permission?state=state-token",
				"state": "state-token",
			}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/channels/instances/"+instanceID.String()+"/zalo-oa/consent", nil)
	req.SetPathValue("id", instanceID.String())
	req = req.WithContext(store.WithTenantID(req.Context(), tenantID))
	rr := httptest.NewRecorder()

	h.handleZaloOAConsent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !observedPublicURL {
		t.Fatal("consent handler did not observe the authenticated public URL")
	}
	var response struct {
		URL   string `json:"url"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.URL == "" || response.State == "" {
		t.Fatalf("response = %+v, want non-empty url and state", response)
	}
}

func TestHandleZaloOAExchangeCodeForwardsAuthenticatedTenantPayload(t *testing.T) {
	tenantID := uuid.New()
	instanceID := uuid.New()
	observedPublicURL := false
	h := &ChannelInstancesHandler{
		publicURLUpdate: func(*http.Request) string {
			observedPublicURL = true
			return "https://gateway.example"
		},
		zaloOAExchange: func(_ context.Context, gotTenant uuid.UUID, gotID, code, state, oaID string) (map[string]any, *ZaloOAFlowError) {
			if gotTenant != tenantID || gotID != instanceID.String() {
				t.Fatalf("flow scope = (%s, %q), want (%s, %q)", gotTenant, gotID, tenantID, instanceID)
			}
			if code != "auth-code" || state != "state-token" || oaID != "12345" {
				t.Fatalf("flow payload = (%q, %q, %q)", code, state, oaID)
			}
			return map[string]any{"ok": true, "oa_id": oaID}, nil
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/channels/instances/"+instanceID.String()+"/zalo-oa/exchange-code",
		strings.NewReader(`{"code":"auth-code","state":"state-token","oa_id":"12345"}`),
	)
	req.SetPathValue("id", instanceID.String())
	req = req.WithContext(store.WithTenantID(req.Context(), tenantID))
	rr := httptest.NewRecorder()

	h.handleZaloOAExchangeCode(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["ok"] != true || response["oa_id"] != "12345" {
		t.Fatalf("response = %#v", response)
	}
	if !observedPublicURL {
		t.Fatal("exchange-code handler did not observe the authenticated public URL")
	}
}
