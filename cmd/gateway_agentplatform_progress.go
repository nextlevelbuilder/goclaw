package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	agentPlatformProgressURLEnv     = "GOCLAW_AGENTPLATFORM_PROGRESS_URL"
	gatewayProgressURLEnv           = "GOCLAW_GATEWAY_PROGRESS_URL"
	gatewayProgressTokenEnv         = "GOCLAW_GATEWAY_PROGRESS_TOKEN"
	agentPlatformProgressTokenEnv   = "GOCLAW_AGENTPLATFORM_PROGRESS_TOKEN" // legacy alias
	agentPlatformProgressTimeoutEnv = "GOCLAW_AGENTPLATFORM_PROGRESS_TIMEOUT_MS"
	agentPlatformProgressKindsEnv   = "GOCLAW_AGENTPLATFORM_PROGRESS_KINDS"
)

type agentPlatformProgressForwarder struct {
	url          string
	token        string
	client       *http.Client
	timeout      time.Duration
	allowedKinds map[string]bool
}

type agentPlatformProgressCallback struct {
	Kind              string         `json:"kind,omitempty"`
	GatewayContextID  string         `json:"gateway_context_id,omitempty"`
	Channel           string         `json:"channel,omitempty"`
	ConversationID    string         `json:"conversation_id,omitempty"`
	MessageID         string         `json:"message_id,omitempty"`
	InternalSessionID string         `json:"internal_session_id,omitempty"`
	OutTrackID        string         `json:"out_track_id,omitempty"`
	ReplyMode         string         `json:"reply_mode,omitempty"`
	Payload           map[string]any `json:"payload"`
	// Compatibility for gateway builds that read renderable fields from the
	// callback root. The canonical protocol object remains Payload.
	Title     string `json:"title,omitempty"`
	Text      string `json:"text,omitempty"`
	Summary   any    `json:"summary,omitempty"`
	Questions any    `json:"questions,omitempty"`
	Fields    any    `json:"fields,omitempty"`
	// Compatibility for gateway builds that still read callback routing context
	// from the original metadata envelope instead of the top-level protocol
	// fields above.
	Metadata       map[string]string `json:"metadata,omitempty"`
	GatewayContext map[string]string `json:"gateway_context,omitempty"`
}

func (d *gatewayDeps) wireAgentPlatformProgressForwarder() {
	url := firstNonEmptyEnv(agentPlatformProgressURLEnv, gatewayProgressURLEnv)
	if url == "" {
		return
	}

	forwarder, err := newAgentPlatformProgressForwarder(
		url,
		firstNonEmptyEnv(gatewayProgressTokenEnv, agentPlatformProgressTokenEnv),
		parseAgentPlatformProgressTimeout(os.Getenv(agentPlatformProgressTimeoutEnv)),
		parseAgentPlatformProgressKinds(os.Getenv(agentPlatformProgressKindsEnv)),
	)
	if err != nil {
		slog.Error("agentplatform.progress_forwarder.disabled", "error", err)
		return
	}

	d.msgBus.Subscribe("agentplatform-progress-forwarder", func(event bus.Event) {
		if event.Name != protocol.EventAgent {
			return
		}
		agentEvent, ok := event.Payload.(agent.AgentEvent)
		if !ok || agentEvent.Type != protocol.AgentEventActivity {
			return
		}

		chatID := agentEvent.ChatID
		if chatID == "" {
			chatID = agentEvent.UserID
		}
		progress, ok := channels.BuildGatewayProgressEvent(agentEvent.RunID, agentEvent.Payload, channels.GatewayProgressRoute{
			AgentID:    agentEvent.AgentID,
			SessionKey: agentEvent.SessionKey,
			Channel:    agentEvent.Channel,
			ChatID:     chatID,
			UserID:     agentEvent.UserID,
			SenderID:   agentEvent.SenderID,
			TenantID:   agentEvent.TenantID.String(),
			Metadata:   agentEvent.Metadata,
		})
		if !ok {
			return
		}
		if !forwarder.kindAllowed(progress.Kind) {
			return
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), forwarder.timeout)
			defer cancel()
			if err := forwarder.postWithRetry(ctx, progress); err != nil {
				slog.Warn("agentplatform.progress_forward_failed",
					"run_id", progress.RunID,
					"child_run_id", progress.ChildRun,
					"kind", progress.Kind,
					"event", progress.Event,
					"error", err,
				)
			}
		}()
	})

	slog.Info("agentplatform.progress_forwarder.enabled", "url", redactProgressURL(url), "kinds", forwarder.allowedKinds)
}

func newAgentPlatformProgressForwarder(rawURL, token string, timeout time.Duration, allowedKinds map[string]bool) (*agentPlatformProgressForwarder, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("%s is empty", agentPlatformProgressURLEnv)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if len(allowedKinds) == 0 {
		allowedKinds = map[string]bool{"progress": true}
	}
	if _, _, err := security.Validate(rawURL); err != nil {
		return nil, fmt.Errorf("validate progress url: %w", err)
	}
	return &agentPlatformProgressForwarder{
		url:          rawURL,
		token:        token,
		timeout:      timeout,
		allowedKinds: allowedKinds,
		client:       security.NewSafeClient(timeout),
	}, nil
}

func (f *agentPlatformProgressForwarder) kindAllowed(kind string) bool {
	return f.allowedKinds["*"] || f.allowedKinds[kind]
}

func (f *agentPlatformProgressForwarder) postWithRetry(ctx context.Context, event channels.GatewayProgressEvent) error {
	attempts := 1
	if channels.IsGatewayProgressKindCritical(event.Kind) {
		attempts = 3
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := f.post(ctx, event); err != nil {
			lastErr = err
			if attempt == attempts {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (f *agentPlatformProgressForwarder) post(ctx context.Context, event channels.GatewayProgressEvent) error {
	_, pinnedIP, err := security.Validate(f.url)
	if err != nil {
		return err
	}

	callback := agentPlatformProgressCallback{
		Kind:              event.Kind,
		GatewayContextID:  event.GatewayContextID,
		Channel:           event.Channel,
		ConversationID:    event.ConversationID,
		MessageID:         event.MessageID,
		InternalSessionID: event.InternalSessionID,
		OutTrackID:        event.OutTrackID,
		ReplyMode:         event.ReplyMode,
		Payload:           event.Payload,
		Metadata:          event.Metadata,
		GatewayContext:    event.GatewayContext,
		Title:             payloadString(event.Payload, "title"),
		Text:              payloadString(event.Payload, "text"),
		Summary:           event.Payload["summary"],
		Questions:         event.Payload["questions"],
		Fields:            event.Payload["fields"],
	}
	body, err := json.Marshal(callback)
	if err != nil {
		return fmt.Errorf("marshal progress event: %w", err)
	}

	reqCtx := security.WithPinnedIP(ctx, pinnedIP)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, f.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GoClaw-Event-Type", event.EventType)
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("post progress: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("progress endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func parseAgentPlatformProgressTimeout(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 5 * time.Second
	}
	var ms int
	if _, err := fmt.Sscanf(raw, "%d", &ms); err != nil || ms <= 0 {
		return 5 * time.Second
	}
	return time.Duration(ms) * time.Millisecond
}

func parseAgentPlatformProgressKinds(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]bool{"progress": true}
	}
	kinds := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		kind := strings.TrimSpace(part)
		if kind == "" {
			continue
		}
		if kind == "all" {
			kind = "*"
		}
		kinds[kind] = true
	}
	if len(kinds) == 0 {
		kinds["progress"] = true
	}
	return kinds
}

func redactProgressURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return value
}
