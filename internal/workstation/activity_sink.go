// Package workstation contains the activity sink that subscribes to domain events
// and persists exec audit rows to WorkstationActivityStore.
package workstation

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// sensitivePatterns is a list of compiled regexes that redact secret-bearing fragments.
// Applied to cmd_preview before storage; raw command is never persisted.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|password|secret|token|auth)[=:]\S+`),
	regexp.MustCompile(`-H\s+"Authorization:[^"]*"`),
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-_\.]+`),
	regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`), // JWT
}

// WireActivitySink subscribes to EventWorkstationExecDone on domainBus and writes
// audit rows to activityStore. The subscription is fire-and-forget (Insert is buffered).
// Also starts a nightly retention goroutine that prunes rows older than 30 days.
// Returns a cleanup function that stops the retention goroutine.
func WireActivitySink(bus eventbus.DomainEventBus, activityStore store.WorkstationActivityStore) func() {
	if bus == nil || activityStore == nil {
		return func() {}
	}

	// Subscribe to exec done events (emitted by WorkstationExecTool.streamAndCollect).
	// The payload is map[string]any (see internal/tools/workstation_exec.go).
	bus.Subscribe(eventbus.EventType(protocol.EventWorkstationExecDone), func(ctx context.Context, ev eventbus.DomainEvent) error {
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			return nil
		}

		wsIDStr, _ := payload["workstation_id"].(string)
		wsID, err := uuid.Parse(wsIDStr)
		if err != nil {
			return nil
		}
		tenantID, _ := uuid.Parse(ev.TenantID)
		agentID := ev.AgentID
		sessionKey, _ := payload["session_key"].(string)

		// I3 fix: use the "command" field from the done event payload for meaningful
		// cmd_hash and cmd_preview. Falls back to sessionKey if command is absent
		// (e.g. events from older tool versions).
		cmdRaw, _ := payload["command"].(string)
		if cmdRaw == "" {
			// Fallback for events without the command field.
			cmdRaw = "session:" + sessionKey
		}
		cmdPreview := redactSensitive(cmdRaw)

		exitCodeF, _ := payload["exit_code"].(int)
		durationF, _ := payload["duration_ms"].(int64)
		// JSON numbers decode as float64 from map[string]any.
		if ef, ok := payload["exit_code"].(float64); ok {
			exitCodeF = int(ef)
		}
		if df, ok := payload["duration_ms"].(float64); ok {
			durationF = int64(df)
		}

		cmdHash := fmt.Sprintf("%x", sha256.Sum256([]byte(cmdRaw)))[:16]

		exitCodeVal := exitCodeF
		durationVal := durationF

		row := &store.WorkstationActivity{
			ID:            uuid.New(),
			TenantID:      tenantID,
			WorkstationID: wsID,
			AgentID:       agentID,
			Action:        "exec",
			CmdHash:       cmdHash,
			CmdPreview:    cmdPreview,
			ExitCode:      &exitCodeVal,
			DurationMS:    &durationVal,
			CreatedAt:     time.Now().UTC(),
		}

		if err := activityStore.Insert(ctx, row); err != nil {
			slog.Warn("workstation.activity.insert_error", "error", err)
		}

		slog.Info("workstation.exec.completed",
			"workstation_id", wsIDStr,
			"tenant_id", ev.TenantID,
			"agent_id", agentID,
			"cmd_hash", cmdHash,
			"exit_code", exitCodeVal,
			"duration_ms", durationVal,
		)
		return nil
	})

	// Start nightly retention goroutine.
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				before := time.Now().Add(-30 * 24 * time.Hour)
				n, err := activityStore.Prune(context.Background(), before)
				if err != nil {
					slog.Warn("workstation.activity.prune_error", "error", err)
				} else if n > 0 {
					slog.Info("workstation.activity.pruned", "rows", n, "before", before.Format(time.RFC3339))
				}
			case <-stopCh:
				return
			}
		}
	}()

	return func() { close(stopCh) }
}

// redactSensitive strips lines or fragments matching known secret patterns from cmd.
// Returns a truncated, redacted string safe for tenant-admin display.
func redactSensitive(cmd string) string {
	result := cmd
	for _, re := range sensitivePatterns {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}
	// Truncate to 200 chars.
	if len(result) > 200 {
		result = result[:200]
	}
	return strings.TrimSpace(result)
}
