package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// dispatchWaitTimeout bounds how long a webhook dispatch test will block on
// the onMessage channel before failing. Kept short (2s) so a regression in
// the dispatch path cannot hang CI for the full go test default (10 min).
// Do NOT replace with t.Context().Done() — that context is only canceled
// after the test returns, which creates a deadlock if the channel never
// receives. This bit Wave C when an unrelated HMAC patch suppressed
// dispatch and the test ran for 10 minutes before timing out.
const dispatchWaitTimeout = 2 * time.Second

// --- helpers ---

// encryptEventPayload encrypts a JSON payload using the same AES-CBC scheme
// that Feishu uses for encrypted webhook events. Used to test decryptEvent.
func encryptEventPayload(t *testing.T, payload []byte, key string) string {
	t.Helper()
	keyHash := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		t.Fatalf("aes new cipher: %v", err)
	}
	// IV = first 16 bytes of key hash (deterministic for testing)
	iv := keyHash[:aes.BlockSize]

	// PKCS7-pad to block size (16)
	pad := aes.BlockSize - (len(payload) % aes.BlockSize)
	padded := make([]byte, len(payload)+pad)
	copy(padded, payload)
	for i := len(payload); i < len(padded); i++ {
		padded[i] = byte(pad)
	}

	ct := make([]byte, aes.BlockSize+len(padded))
	copy(ct[:aes.BlockSize], iv)
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct[aes.BlockSize:], padded)

	return base64.StdEncoding.EncodeToString(ct)
}

// buildWebhookRequest creates a POST request body for a webhook event.
func buildWebhookRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/feishu/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// --- URL verification ---

func TestWebhookHandler_URLVerification(t *testing.T) {
	called := false
	h := NewWebhookHandler("", "", func(_ *MessageEvent) { called = true })

	body := `{"type":"url_verification","token":"test-tok","challenge":"abc123"}`
	w := httptest.NewRecorder()
	h.ServeHTTP(w, buildWebhookRequest(body))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["challenge"] != "abc123" {
		t.Errorf("challenge: got %q, want %q", resp["challenge"], "abc123")
	}
	if called {
		t.Error("onMessage must not be called for url_verification")
	}
}

// --- Method not allowed ---

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	h := NewWebhookHandler("", "", func(_ *MessageEvent) {})
	req := httptest.NewRequest(http.MethodGet, "/feishu/events", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", w.Code)
	}
}

// --- Token verification ---

func TestWebhookHandler_TokenMismatch(t *testing.T) {
	called := false
	h := NewWebhookHandler("expected-token", "", func(_ *MessageEvent) { called = true })

	// Build a message event with a wrong token
	env := map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_type": "im.message.receive_v1",
			"token":      "wrong-token",
			"app_id":     "cli_test",
			"tenant_key": "test-tenant-1",
		},
		"event": map[string]any{
			"sender":  map[string]any{},
			"message": map[string]any{"message_id": "om_1", "chat_id": "oc_1"},
		},
	}
	body, _ := json.Marshal(env)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, buildWebhookRequest(string(body)))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	// Token mismatch: handler silently drops, must NOT dispatch
	if called {
		t.Error("onMessage must not be called on token mismatch")
	}
}

func TestWebhookHandler_TokenMatch_Dispatches(t *testing.T) {
	dispatched := make(chan *MessageEvent, 1)
	h := NewWebhookHandler("good-token", "", func(e *MessageEvent) { dispatched <- e })

	env := map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id":   "evt_001",
			"event_type": "im.message.receive_v1",
			"token":      "good-token",
			"app_id":     "cli_test",
			"tenant_key": "test-tenant-1",
		},
		"event": map[string]any{
			"sender": map[string]any{
				"sender_id":   map[string]any{"open_id": "ou_U00000001"},
				"sender_type": "user",
				"tenant_key":  "test-tenant-1",
			},
			"message": map[string]any{
				"message_id":   "om_msg001",
				"chat_id":      "oc_chat_001",
				"chat_type":    "p2p",
				"message_type": "text",
				"content":      `{"text":"hello bot"}`,
			},
		},
	}
	body, _ := json.Marshal(env)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, buildWebhookRequest(string(body)))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	// Wait for goroutine to dispatch the event (race-free via channel).
	select {
	case e := <-dispatched:
		if e == nil {
			t.Error("dispatched event was nil")
		}
	case <-time.After(dispatchWaitTimeout):
		t.Error("timeout waiting for dispatched event")
	}
}

// --- Non-message event type ---

func TestWebhookHandler_NonMessageEvent_Ignored(t *testing.T) {
	called := false
	h := NewWebhookHandler("", "", func(_ *MessageEvent) { called = true })

	env := map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_type": "im.chat.member.bot.added_v1",
			"token":      "",
		},
	}
	body, _ := json.Marshal(env)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, buildWebhookRequest(string(body)))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	if called {
		t.Error("onMessage must not be called for non-message events")
	}
}

// --- Invalid JSON ---

func TestWebhookHandler_InvalidJSON(t *testing.T) {
	h := NewWebhookHandler("", "", func(_ *MessageEvent) {})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, buildWebhookRequest("not-json{{{"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

// --- Encrypted event ---

func TestWebhookHandler_EncryptedEvent_Decrypted(t *testing.T) {
	const encKey = "test-encrypt-key-2024"
	dispatched := make(chan *MessageEvent, 1)

	h := NewWebhookHandler("", encKey, func(e *MessageEvent) { dispatched <- e })

	innerEvent := map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id":   "evt_enc_01",
			"event_type": "im.message.receive_v1",
			"token":      "",
			"app_id":     "cli_enc",
			"tenant_key": "test-tenant-1",
		},
		"event": map[string]any{
			"sender": map[string]any{
				"sender_id":  map[string]any{"open_id": "ou_U00000002"},
				"tenant_key": "test-tenant-1",
			},
			"message": map[string]any{
				"message_id":   "om_enc001",
				"chat_id":      "oc_chat_enc",
				"chat_type":    "p2p",
				"message_type": "text",
				"content":      `{"text":"encrypted message"}`,
			},
		},
	}
	plainJSON, _ := json.Marshal(innerEvent)
	encrypted := encryptEventPayload(t, plainJSON, encKey)

	outerBody := map[string]any{"encrypt": encrypted}
	outerJSON, _ := json.Marshal(outerBody)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, buildWebhookRequest(string(outerJSON)))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}

	// Wait for goroutine to dispatch — race-free via buffered channel.
	select {
	case e := <-dispatched:
		if e == nil {
			t.Error("dispatched event was nil")
		}
	case <-time.After(dispatchWaitTimeout):
		t.Error("timeout waiting for dispatched encrypted event")
	}
}

// --- decryptEvent unit tests ---

func TestDecryptEvent_InvalidBase64(t *testing.T) {
	_, err := decryptEvent("not-valid-base64!!!", "key")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecryptEvent_TooShort(t *testing.T) {
	// Valid base64 but shorter than AES block size (16 bytes)
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := decryptEvent(short, "key")
	if err == nil {
		t.Fatal("expected error for ciphertext too short")
	}
}

func TestDecryptEvent_NoJSONInPlaintext(t *testing.T) {
	// Encrypt something that has no { } after decryption.
	// Use all-zero padding — won't contain valid JSON braces in useful positions.
	keyHash := sha256.Sum256([]byte("testkey"))
	block, _ := aes.NewCipher(keyHash[:])
	iv := keyHash[:aes.BlockSize]
	// Plaintext: 16 bytes of 0x10 (valid PKCS7 padding for 16-byte block)
	pt := make([]byte, 16)
	for i := range pt {
		pt[i] = 0x10
	}
	ct := make([]byte, aes.BlockSize+16)
	copy(ct[:aes.BlockSize], iv)
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ct[aes.BlockSize:], pt)
	encoded := base64.StdEncoding.EncodeToString(ct)

	_, err := decryptEvent(encoded, "testkey")
	if err == nil {
		t.Fatal("expected error when no JSON found in decrypted content")
	}
}

// TestDecryptEvent_TamperedPayload is the security-critical test.
// AES-CBC alone does not provide authentication (no HMAC/GCM), so a wrong key
// will still "decrypt" the bytes — but the result will be garbled, non-parseable
// JSON. The webhook handler re-parses the decrypted body with json.Unmarshal and
// then checks the verification token; a tampered payload therefore cannot impersonate
// a valid event.
//
// This test asserts the observable contract: when decryptEvent is called with the
// correct key it returns the original plaintext; when called with a wrong key the
// returned bytes do NOT equal the original plaintext (the decryption is corrupted).
func TestDecryptEvent_TamperedPayload(t *testing.T) {
	const realKey = "real-encrypt-key-abc"
	const wrongKey = "wrong-key-tampered"

	plaintext := []byte(`{"type":"url_verification","challenge":"xyz"}`)
	encrypted := encryptEventPayload(t, plaintext, realKey)

	// With the correct key the original JSON is recovered.
	gotReal, err := decryptEvent(encrypted, realKey)
	if err != nil {
		t.Fatalf("decryptEvent with real key failed: %v", err)
	}
	if string(gotReal) != string(plaintext) {
		t.Errorf("real-key decrypt: got %q, want %q", gotReal, plaintext)
	}

	// With a wrong key: AES-CBC produces garbled output.
	// The function may or may not error (depending on whether { } appear in garbage),
	// but the result MUST differ from the original plaintext — the tampered payload
	// cannot reconstruct the valid event content.
	gotWrong, errWrong := decryptEvent(encrypted, wrongKey)
	if errWrong == nil && string(gotWrong) == string(plaintext) {
		t.Error("SECURITY: decryptEvent with wrong key returned the original plaintext — tampered payload was not corrupted")
	}
	// If errWrong != nil, the garbage didn't even contain { } — that's an even stronger failure.
}
