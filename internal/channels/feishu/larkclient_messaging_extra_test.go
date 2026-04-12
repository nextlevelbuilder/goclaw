package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newSimpleMockServer returns a test server that always responds with the given
// JSON payload for any non-token request.
func newSimpleMockServer(t *testing.T, respJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, respJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newBinaryMockServer returns a server that serves binary data for any non-token request.
func newBinaryMockServer(t *testing.T, data []byte, contentType string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// --- DownloadImage ---

func TestDownloadImage_Success(t *testing.T) {
	imgData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic
	srv := newBinaryMockServer(t, imgData, "image/png")

	c := NewLarkClient("app", "secret", srv.URL)
	data, err := c.DownloadImage(context.Background(), "img_key_123")
	if err != nil {
		t.Fatalf("DownloadImage error: %v", err)
	}
	if !bytes.Equal(data, imgData) {
		t.Errorf("data mismatch: got %v, want %v", data, imgData)
	}
}

// --- UploadImage ---

func TestUploadImage_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"image_key":"img_uploaded_abc"}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	key, err := c.UploadImage(context.Background(), bytes.NewReader([]byte{0xFF, 0xD8, 0xFF}))
	if err != nil {
		t.Fatalf("UploadImage error: %v", err)
	}
	if key != "img_uploaded_abc" {
		t.Errorf("image_key: got %q, want %q", key, "img_uploaded_abc")
	}
}

func TestUploadImage_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"quota exceeded","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.UploadImage(context.Background(), bytes.NewReader([]byte{0x01}))
	if err == nil {
		t.Fatal("expected error on non-zero code")
	}
}

// --- UploadFile ---

func TestUploadFile_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"file_key":"file_key_xyz"}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	key, err := c.UploadFile(context.Background(), bytes.NewReader([]byte("PDF data")), "test.pdf", "pdf", 0)
	if err != nil {
		t.Fatalf("UploadFile error: %v", err)
	}
	if key != "file_key_xyz" {
		t.Errorf("file_key: got %q, want %q", key, "file_key_xyz")
	}
}

func TestUploadFile_WithDuration(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"msg":"ok","data":{"file_key":"audio_key"}}`)
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.UploadFile(context.Background(), bytes.NewReader([]byte("audio")), "voice.opus", "opus", 5000)
	if err != nil {
		t.Fatalf("UploadFile error: %v", err)
	}
	if !strings.Contains(gotBody, "5000") {
		t.Errorf("expected duration in body: %s", gotBody)
	}
}

// --- DownloadMessageResource ---

func TestDownloadMessageResource_Success(t *testing.T) {
	fileData := []byte("binary file content")
	srv := newBinaryMockServer(t, fileData, "application/octet-stream")

	c := NewLarkClient("app", "secret", srv.URL)
	data, _, err := c.DownloadMessageResource(context.Background(), "om_msg1", "fk_123", "file")
	if err != nil {
		t.Fatalf("DownloadMessageResource error: %v", err)
	}
	if !bytes.Equal(data, fileData) {
		t.Errorf("data mismatch")
	}
}

// --- CreateCard ---

func TestCreateCard_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"card_id":"card_abc"}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	cardID, err := c.CreateCard(context.Background(), "card", `{"schema":"2.0"}`)
	if err != nil {
		t.Fatalf("CreateCard error: %v", err)
	}
	if cardID != "card_abc" {
		t.Errorf("card_id: got %q, want card_abc", cardID)
	}
}

func TestCreateCard_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":99001,"msg":"invalid card","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.CreateCard(context.Background(), "card", `{}`)
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

// --- UpdateCardSettings ---

func TestUpdateCardSettings_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	err := c.UpdateCardSettings(context.Background(), "card_abc", `{"streaming":false}`, 1, "uuid-001")
	if err != nil {
		t.Fatalf("UpdateCardSettings error: %v", err)
	}
}

func TestUpdateCardSettings_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":500,"msg":"server error","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	err := c.UpdateCardSettings(context.Background(), "card_bad", `{}`, 0, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- UpdateCardElement ---

func TestUpdateCardElement_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"msg":"ok","data":{}}`)
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	err := c.UpdateCardElement(context.Background(), "card_xyz", "elem_1", "new content", 2, "uuid-002")
	if err != nil {
		t.Fatalf("UpdateCardElement error: %v", err)
	}
	if !strings.Contains(gotPath, "card_xyz") {
		t.Errorf("path should contain card_id: %q", gotPath)
	}
	if !strings.Contains(gotPath, "elem_1") {
		t.Errorf("path should contain element_id: %q", gotPath)
	}
}

func TestUpdateCardElement_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":404,"msg":"element not found","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	err := c.UpdateCardElement(context.Background(), "card_x", "elem_x", "text", 0, "")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

// --- AddMessageReaction ---

func TestAddMessageReaction_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"reaction_id":"rxn_abc"}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	reactionID, err := c.AddMessageReaction(context.Background(), "om_msg1", "Typing")
	if err != nil {
		t.Fatalf("AddMessageReaction error: %v", err)
	}
	if reactionID != "rxn_abc" {
		t.Errorf("reaction_id: got %q, want rxn_abc", reactionID)
	}
}

func TestAddMessageReaction_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":1234,"msg":"reaction failed","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.AddMessageReaction(context.Background(), "om_msg1", "Typing")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

// --- DeleteMessageReaction ---

func TestDeleteMessageReaction_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	err := c.DeleteMessageReaction(context.Background(), "om_msg1", "rxn_abc")
	if err != nil {
		t.Fatalf("DeleteMessageReaction error: %v", err)
	}
}

func TestDeleteMessageReaction_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":404,"msg":"not found","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	err := c.DeleteMessageReaction(context.Background(), "om_msg1", "rxn_missing")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

// --- GetBotInfo (returns open_id string) ---

func TestGetBotInfo_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"bot":{"open_id":"ou_bot_123","app_name":"TestBot"}}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	openID, err := c.GetBotInfo(context.Background())
	if err != nil {
		t.Fatalf("GetBotInfo error: %v", err)
	}
	if openID != "ou_bot_123" {
		t.Errorf("open_id: got %q, want ou_bot_123", openID)
	}
}

func TestGetBotInfo_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"forbidden","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.GetBotInfo(context.Background())
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

// --- ListChatMembers ---

func TestListChatMembers_Success(t *testing.T) {
	resp := map[string]any{
		"code": 0,
		"msg":  "ok",
		"data": map[string]any{
			"items": []any{
				map[string]any{"member_id": "ou_A", "name": "Alice"},
				map[string]any{"member_id": "ou_B", "name": "Bob"},
			},
			"has_more": false,
		},
	}
	respJSON, _ := json.Marshal(resp)
	srv := newSimpleMockServer(t, string(respJSON))

	c := NewLarkClient("app", "secret", srv.URL)
	members, err := c.ListChatMembers(context.Background(), "oc_chat_1")
	if err != nil {
		t.Fatalf("ListChatMembers error: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
	if members[0].MemberID != "ou_A" {
		t.Errorf("first member: got %q, want ou_A", members[0].MemberID)
	}
}

func TestListChatMembers_Pagination(t *testing.T) {
	// First page: has_more=true, second page: has_more=false
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok",
				"data": map[string]any{
					"items":      []any{map[string]any{"member_id": "ou_1", "name": "User1"}},
					"page_token": "next_page_token",
					"has_more":   true,
				},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok",
				"data": map[string]any{
					"items":    []any{map[string]any{"member_id": "ou_2", "name": "User2"}},
					"has_more": false,
				},
			})
		}
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	members, err := c.ListChatMembers(context.Background(), "oc_chat_paged")
	if err != nil {
		t.Fatalf("ListChatMembers pagination error: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 total members from 2 pages, got %d", len(members))
	}
}

func TestListChatMembers_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"not found","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.ListChatMembers(context.Background(), "oc_missing")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}
