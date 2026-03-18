package feishu

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// --- Event types (replacing larkim.P2MessageReceiveV1) ---

// MessageEvent is the parsed structure of a Feishu im.message.receive_v1 event.
type MessageEvent struct {
	Schema string `json:"schema"`
	Header struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		Token     string `json:"token"`
		AppID     string `json:"app_id"`
		TenantKey string `json:"tenant_key"`
	} `json:"header"`
	Event struct {
		Sender  EventSender  `json:"sender"`
		Message EventMessage `json:"message"`
	} `json:"event"`
}

type EventSender struct {
	SenderID struct {
		OpenID  string `json:"open_id"`
		UserID  string `json:"user_id"`
		UnionID string `json:"union_id"`
	} `json:"sender_id"`
	SenderType string `json:"sender_type"`
	TenantKey  string `json:"tenant_key"`
}

type EventMessage struct {
	MessageID   string         `json:"message_id"`
	RootID      string         `json:"root_id"`
	ParentID    string         `json:"parent_id"`
	ChatID      string         `json:"chat_id"`
	ChatType    string         `json:"chat_type"`
	MessageType string         `json:"message_type"`
	Content     string         `json:"content"`
	Mentions    []EventMention `json:"mentions"`
}

type EventMention struct {
	Key       string `json:"key"`
	ID        struct {
		OpenID  string `json:"open_id"`
		UserID  string `json:"user_id"`
		UnionID string `json:"union_id"`
	} `json:"id"`
	Name      string `json:"name"`
	TenantKey string `json:"tenant_key"`
}

// --- Webhook event envelope ---

// webhookEvent is the raw envelope for webhook callbacks.
// Schema v1.0 uses flat structure, v2.0 uses header+event.
type webhookEvent struct {
	// v2.0 fields
	Schema  string          `json:"schema"`
	Header  json.RawMessage `json:"header"`
	Event   json.RawMessage `json:"event"`

	// v1.0 fields (also used for URL verification challenge)
	Type      string `json:"type"`
	Token     string `json:"token"`
	Challenge string `json:"challenge"`
	Encrypt   string `json:"encrypt"`
}

// --- Webhook HTTP handler ---

// NewWebhookHandler creates an http.HandlerFunc that handles Feishu webhook events.
// Supports: URL verification challenge, event decryption, and message dispatch.
func NewWebhookHandler(verificationToken, encryptKey string, onMessage func(event *MessageEvent)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		// Try to decrypt if encrypted
		var envelope webhookEvent
		if err := json.Unmarshal(body, &envelope); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		// Handle encrypted events
		eventBody := body
		if envelope.Encrypt != "" && encryptKey != "" {
			decrypted, err := decryptEvent(envelope.Encrypt, encryptKey)
			if err != nil {
				slog.Warn("feishu webhook decrypt failed", "error", err)
				http.Error(w, "decrypt failed", http.StatusBadRequest)
				return
			}
			eventBody = decrypted
			// Re-parse decrypted content
			if err := json.Unmarshal(decrypted, &envelope); err != nil {
				http.Error(w, "invalid decrypted json", http.StatusBadRequest)
				return
			}
		}

		// URL verification challenge
		if envelope.Type == "url_verification" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"challenge": envelope.Challenge})
			return
		}

		// Parse as message event
		var event MessageEvent

		if err := json.Unmarshal(eventBody, &event); err != nil {
			slog.Debug("feishu webhook parse event failed", "error", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Verify token if configured
		if verificationToken != "" && event.Header.Token != verificationToken {
			slog.Warn("feishu webhook token mismatch")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Only handle message events
		if event.Header.EventType == "im.message.receive_v1" {
			go onMessage(&event)
		}

		w.WriteHeader(http.StatusOK)
	}
}

// --- AES-CBC decryption for encrypted events ---

func decryptEvent(encryptedBase64, key string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	// Key is SHA256 of the encrypt key
	keyHash := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// IV is first 16 bytes
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// Find JSON content (between first { and last })
	plaintext := string(ciphertext)
	start := strings.Index(plaintext, "{")
	end := strings.LastIndex(plaintext, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no json found in decrypted content")
	}

	return []byte(plaintext[start : end+1]), nil
}
