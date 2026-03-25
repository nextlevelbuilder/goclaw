package googlechat

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// NewWebhookHandler creates an http.HandlerFunc for Google Chat webhook events.
// projectNumber: Google Cloud project number for OIDC verification (empty = skip verify).
// onMessage: callback for MESSAGE events, invoked in a goroutine.
func NewWebhookHandler(projectNumber string, onMessage func(event *SpaceEvent)) http.HandlerFunc {
	if projectNumber == "" {
		slog.Warn("googlechat: OIDC verification disabled — webhook endpoint is unauthenticated, set project_number to enable")
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// OIDC verification using Google Chat's specific JWK endpoint
		if projectNumber != "" {
			if err := verifyOIDC(r, projectNumber); err != nil {
				slog.Warn("googlechat: OIDC verification failed", "error", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{}`))
				return
			}
		}

		// Read and parse body (limit 1MB to prevent abuse)
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		var event SpaceEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		// Return 200 immediately (Google retries on timeout)
		w.WriteHeader(http.StatusOK)

		// Process asynchronously
		switch event.Type {
		case "MESSAGE":
			if onMessage != nil {
				go onMessage(&event)
			}
		case "ADDED_TO_SPACE":
			slog.Info("googlechat: bot added to space",
				"space", event.Space.GetName(),
				"user", event.User.GetDisplayName())
		case "REMOVED_FROM_SPACE":
			slog.Info("googlechat: bot removed from space",
				"space", event.Space.GetName())
		default:
			slog.Debug("googlechat: unhandled event type", "type", event.Type)
		}
	}
}

// verifyOIDC validates the Google OIDC token from the Authorization header.
// Uses Google Chat's specific JWK endpoint (chat@system.gserviceaccount.com)
// instead of the generic OAuth2 certs endpoint, which doesn't include Chat signing keys.
func verifyOIDC(r *http.Request, projectNumber string) error {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return fmt.Errorf("missing Authorization header")
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == auth {
		return fmt.Errorf("invalid Authorization format")
	}

	// Build list of acceptable audiences: webhook URL + project number
	var audiences []string
	if url := buildRequestURL(r); url != "" {
		audiences = append(audiences, url)
	}
	audiences = append(audiences, projectNumber)

	return verifyChatToken(r.Context(), token, audiences)
}

// buildRequestURL reconstructs the original full URL from request headers.
// Returns empty string if scheme/host cannot be determined.
func buildRequestURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host + r.URL.Path
}

// isBotMentioned checks if the bot was @mentioned in the message annotations.
func isBotMentioned(event *SpaceEvent) bool {
	if event.Message == nil {
		return false
	}
	for _, ann := range event.Message.Annotations {
		if ann.Type == "USER_MENTION" && ann.UserMention != nil {
			if ann.UserMention.User != nil && ann.UserMention.User.Type == "BOT" {
				return true
			}
		}
	}
	return false
}

// extractSenderInfo returns (senderID, displayName) from the event.
// senderID is the stable resource name (e.g. "users/123456789") — display name is excluded
// to prevent session loss when users rename themselves.
func extractSenderInfo(event *SpaceEvent) (string, string) {
	user := event.User
	if event.Message != nil && event.Message.Sender != nil {
		user = event.Message.Sender
	}
	if user == nil {
		return "", ""
	}
	return user.Name, user.DisplayName
}
