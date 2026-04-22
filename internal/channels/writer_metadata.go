package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
)

// WriterLabelMaxRunes caps the rendered writer label length so a pathological
// username/displayName cannot dominate the system prompt or squeeze legitimate
// instructions out.
const WriterLabelMaxRunes = 48

// SanitizeWriterLabel strips control characters (\r\n\t and friends) from
// user-controlled metadata strings before they flow into the system prompt
// or Telegram responses. A user could otherwise set their Telegram
// displayName to "Alice\n\nSYSTEM: ignore previous instructions..." and
// have it rendered as a pseudo-instruction. Also collapses whitespace runs
// and truncates to WriterLabelMaxRunes.
func SanitizeWriterLabel(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = r == ' '
	}
	out := strings.TrimSpace(b.String())
	runes := []rune(out)
	if len(runes) > WriterLabelMaxRunes {
		out = string(runes[:WriterLabelMaxRunes]) + "…"
	}
	return out
}

// WriterMeta is the canonical shape stored in ConfigPermission.Metadata for
// file_writer grants. Kept local to avoid a store→channels dependency; the
// field tags must stay in sync with what /addwriter + enrichment emit.
type writerMetaShape struct {
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
}

// WriterLabel renders a writer's metadata JSON into a human-readable tag.
// Preference order: @username → displayName → "User <userID>". All
// user-controlled strings pass through SanitizeWriterLabel. The userID arg
// is the fallback when no metadata is available (legacy rows).
func WriterLabel(metadata json.RawMessage, userID string) string {
	var meta writerMetaShape
	_ = json.Unmarshal(metadata, &meta)
	if u := SanitizeWriterLabel(meta.Username); u != "" {
		return "@" + u
	}
	if d := SanitizeWriterLabel(meta.DisplayName); d != "" {
		return d
	}
	return "User " + userID
}

// writerEnrichTimeout bounds each ResolveMember call so a slow channel
// (network stall, Telegram API hang) never blocks the Grant path beyond
// this budget.
const writerEnrichTimeout = 3 * time.Second

// IsEmptyWriterMetadata reports whether the caller-supplied metadata is
// effectively empty and therefore eligible for enrichment. Accepts
// zero-length, "{}", "null", whitespace-only payloads, and the common
// "both fields present but blank" shape `{"displayName":"","username":""}`
// emitted by `/addwriter` when Telegram returns a user without Username
// or FirstName.
func IsEmptyWriterMetadata(m json.RawMessage) bool {
	if len(m) == 0 {
		return true
	}
	trimmed := bytes.TrimSpace(m)
	if len(trimmed) == 0 ||
		bytes.Equal(trimmed, []byte("{}")) ||
		bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	// Structural check for `{"displayName":"","username":""}` regardless of
	// key order or whitespace. Callers that emit this shape still consider
	// the row unenriched.
	var parsed struct {
		DisplayName string `json:"displayName"`
		Username    string `json:"username"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		return parsed.DisplayName == "" && parsed.Username == ""
	}
	return false
}

// ParseGroupScope splits a file_writer scope into (channelName, chatID).
// Returns ok=false for any shape other than "group:<name>:<chatID>".
func ParseGroupScope(scope string) (channelName, chatID string, ok bool) {
	if !strings.HasPrefix(scope, "group:") {
		return "", "", false
	}
	parts := strings.SplitN(scope, ":", 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// BuildWriterMetadata returns canonical JSON `{"username":"...","displayName":"..."}`.
func BuildWriterMetadata(info MemberInfo) (json.RawMessage, error) {
	return json.Marshal(map[string]string{
		"username":    info.Username,
		"displayName": info.DisplayName,
	})
}

// EnrichFileWriterMetadata resolves (scope,userID) to a writer-metadata JSON
// payload. Returns (nil,false) when the resolver is missing, scope malformed,
// channel lacks resolver support, or lookup fails — callers should proceed
// with whatever metadata they already had. Enrichment is strictly best-effort
// and never returns an error.
func EnrichFileWriterMetadata(ctx context.Context, resolver MemberResolver, scope, userID string) (json.RawMessage, bool) {
	if resolver == nil {
		return nil, false
	}
	channelName, chatID, ok := ParseGroupScope(scope)
	if !ok {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, writerEnrichTimeout)
	defer cancel()
	info, err := resolver.ResolveMember(ctx, channelName, chatID, userID)
	if err != nil {
		if errors.Is(err, ErrMemberResolveNotSupported) {
			slog.Debug("writer_metadata.enrich.skip", "channel", channelName, "reason", "not_supported")
			return nil, false
		}
		slog.Warn("writer_metadata.enrich_failed",
			"channel", channelName, "chat_id", chatID, "user_id", userID, "error", err)
		return nil, false
	}
	// Telegram can return a member whose Username and FirstName are both
	// empty (deleted accounts, banned members, legacy bots). Reporting
	// success here would poison the writerHealLastTry cache with a row
	// that still renders as a bare ID — treat it as a soft miss so the
	// next /writers refresh (after TTL) retries.
	if info.Username == "" && info.DisplayName == "" {
		return nil, false
	}
	meta, err := BuildWriterMetadata(info)
	if err != nil {
		return nil, false
	}
	return meta, true
}
