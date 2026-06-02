package cmd

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mediaDebounceFloorMs is the minimum debounce window applied when a message
// carries media AND the post-override delay would be 0. Prevents multi-attachment
// bursts from triggering one agent run per attachment when operators disable
// debouncing (issue #63). See plans/260528-1351-multi-attachment-debounce/.
const mediaDebounceFloorMs = 1000

func prepareInboundDebounceMessage(msg *bus.InboundMessage, deps *ConsumerDeps) {
	if msg == nil || deps == nil || deps.Cfg == nil || msg.AgentID != "" {
		return
	}
	msg.AgentID = resolveAgentRoute(deps.Cfg, msg.Channel, msg.ChatID, msg.PeerKind)
}

func resolveInboundDebounceDelay(ctx context.Context, msg bus.InboundMessage, deps *ConsumerDeps) time.Duration {
	debounceMs := 0
	if deps != nil && deps.Cfg != nil {
		debounceMs = deps.Cfg.Gateway.InboundDebounceMs
	}
	if deps == nil || deps.AgentStore == nil || msg.AgentID == "" {
		return inboundDebounceDuration(applyMediaFloor(debounceMs, msg))
	}

	agentCtx := ctx
	if msg.TenantID != uuid.Nil {
		agentCtx = store.WithTenantID(agentCtx, msg.TenantID)
	} else {
		agentCtx = store.WithTenantID(agentCtx, store.MasterTenantID)
	}

	agentData, err := getInboundDebounceAgent(agentCtx, deps.AgentStore, msg.AgentID)
	if err != nil || agentData == nil {
		if err != nil {
			slog.Debug("inbound debounce: agent config unavailable", "agent", msg.AgentID, "error", err)
		}
		return inboundDebounceDuration(applyMediaFloor(debounceMs, msg))
	}
	if overrideMs, ok := agentData.ParseInboundDebounceMs(); ok {
		debounceMs = overrideMs
	}
	return inboundDebounceDuration(applyMediaFloor(debounceMs, msg))
}

// applyMediaFloor enforces the media debounce floor.
//
// Precedence: floor fires ONLY when the post-override delay is exactly 0. A
// non-zero agent override (even below the floor) is honored verbatim — operators
// who set debounce_ms=500 on a media-receiving agent get 500ms, not 1000ms.
//
// Exemption: internal publishers (SenderID prefix "system:" or "subagent:") bypass
// the floor. Their messages are synthesized by tools/subagents and have no burst-
// arrival semantics; a +1s latency on every tool echo would be a regression.
func applyMediaFloor(delayMs int, msg bus.InboundMessage) int {
	if delayMs <= 0 && len(msg.Media) > 0 && !isSystemOrSubagentSender(msg.SenderID) {
		return mediaDebounceFloorMs
	}
	return delayMs
}

// isSystemOrSubagentSender reports whether the SenderID identifies an internal
// publisher (system: or subagent: colon-prefixed). Plain prefix matches like
// "systemic" or "subagentX" without the colon are NOT internal.
func isSystemOrSubagentSender(senderID string) bool {
	return strings.HasPrefix(senderID, "system:") || strings.HasPrefix(senderID, "subagent:")
}

func getInboundDebounceAgent(ctx context.Context, agentStore store.AgentStore, agentID string) (*store.AgentData, error) {
	if parsed, err := uuid.Parse(agentID); err == nil && parsed != uuid.Nil {
		return agentStore.GetByID(ctx, parsed)
	}
	return agentStore.GetByKey(ctx, agentID)
}

func inboundDebounceDuration(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}
